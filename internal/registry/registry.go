// Package registry loads, validates and edits toolshed's catalog of tools.
//
// The catalog is the built-in examples (registry.yaml, compiled in) with the
// user's own registry layered on top (~/.toolshed/registry.yaml). User
// entries win on ID collisions, and `examples: false` in the user file hides
// the built-ins entirely.
package registry

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	toolshed "github.com/bartdegoede/toolshed"
	"github.com/bartdegoede/toolshed/internal/clients"
)

// Entry is one tool: an MCP server toolshed can install into Claude Code and
// Codex. The yaml tags are the public file schema (docs/registry.md).
type Entry struct {
	ID          string            `yaml:"id"`
	Name        string            `yaml:"name,omitempty"`
	Description string            `yaml:"description,omitempty"`
	Command     string            `yaml:"command,omitempty"`        // stdio server: executable (on PATH or absolute)
	Args        []string          `yaml:"args,omitempty"`           // stdio server: arguments
	URL         string            `yaml:"url,omitempty"`            // remote server (streamable HTTP)
	HeaderCmd   string            `yaml:"headers_helper,omitempty"` // remote: command printing JSON headers
	Headers     map[string]string `yaml:"headers,omitempty"`        // remote: HTTP headers; ${KEY} is replaced by input KEY
	Env         map[string]string `yaml:"env,omitempty"`            // fixed environment variables
	Inputs      []Input           `yaml:"inputs,omitempty"`         // values prompted for at install
	Clients     []string          `yaml:"clients,omitempty"`        // restrict to these clients (clients.All)

	// Pending marks a tool an agent added through `toolshed mcp`. toolshed
	// won't start or install it until the user approves it (`toolshed
	// approve`, or the TUI), so an agent can't run a command of its choosing.
	Pending bool `yaml:"pending,omitempty"`

	Source string `yaml:"-"` // "builtin" or "user"
}

// Input is a value the user provides at install time, passed to the server
// as an environment variable. Secret inputs live in the OS keyring.
type Input struct {
	Key      string `yaml:"key"`
	Label    string `yaml:"label,omitempty"`
	Help     string `yaml:"help,omitempty"`
	Secret   bool   `yaml:"secret,omitempty"`
	Optional bool   `yaml:"optional,omitempty"`
	Default  string `yaml:"default,omitempty"`
	Share    string `yaml:"share,omitempty"` // keyring group shared across tools; defaults to the entry ID
}

// File is the on-disk shape of a registry.
type File struct {
	Examples *bool   `yaml:"examples,omitempty"` // user file only: false hides the built-in examples
	Tools    []Entry `yaml:"tools"`
}

// Registry is the merged, validated catalog.
type Registry struct {
	Entries  []*Entry // display order: user entries first, then built-ins
	UserPath string
	byID     map[string]*Entry
}

const (
	SourceBuiltin = "builtin"
	SourceUser    = "user"
)

// Home is toolshed's state directory: $TOOLSHED_HOME or ~/.toolshed.
func Home() string {
	if p := os.Getenv("TOOLSHED_HOME"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".toolshed")
}

// UserPath is the user registry: $TOOLSHED_REGISTRY or ~/.toolshed/registry.yaml.
func UserPath() string {
	if p := os.Getenv("TOOLSHED_REGISTRY"); p != "" {
		return p
	}
	return filepath.Join(Home(), "registry.yaml")
}

// IsRemote reports whether the entry is a remote (HTTP) server.
func (e *Entry) IsRemote() bool { return e.URL != "" }

// DisplayName falls back to the ID when no name is set.
func (e *Entry) DisplayName() string {
	if e.Name != "" {
		return e.Name
	}
	return e.ID
}

// CommandLine is what toolshed would run (or connect to) for this entry, for
// showing to the user before they approve it.
func (e *Entry) CommandLine() string {
	if e.IsRemote() {
		switch {
		case e.HeaderCmd != "":
			return e.URL + " (headers from: " + e.HeaderCmd + ")"
		case len(e.Headers) > 0:
			names := make([]string, 0, len(e.Headers))
			for k, v := range e.Headers {
				names = append(names, k+": "+v)
			}
			sort.Strings(names)
			return e.URL + " (" + strings.Join(names, ", ") + ")"
		}
		return e.URL
	}
	parts := []string{quoteArg(e.Command)}
	for _, a := range e.Args {
		parts = append(parts, quoteArg(a))
	}
	return strings.Join(parts, " ")
}

// SecretKeys lists the entry's secret inputs (what it gets from the keyring).
func (e *Entry) SecretKeys() []string {
	var keys []string
	for _, in := range e.Inputs {
		if in.Secret {
			keys = append(keys, in.Key)
		}
	}
	return keys
}

// ShellQuote quotes s for a POSIX shell, leaving plain words as they are.
func ShellQuote(s string) string { return quoteArg(s) }

// placeholder matches ${KEY} in a header value.
var placeholder = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// HeaderKeys lists the inputs a header value refers to.
func HeaderKeys(value string) []string {
	var keys []string
	for _, m := range placeholder.FindAllStringSubmatch(value, -1) {
		keys = append(keys, m[1])
	}
	return keys
}

// ExpandHeaders fills ${KEY} in each header from vals. missing lists keys
// with no value; their placeholders are left empty.
func ExpandHeaders(headers, vals map[string]string) (out map[string]string, missing []string) {
	out = map[string]string{}
	seen := map[string]bool{}
	for name, v := range headers {
		out[name] = placeholder.ReplaceAllStringFunc(v, func(m string) string {
			k := placeholder.FindStringSubmatch(m)[1]
			if vals[k] == "" && !seen[k] {
				seen[k] = true
				missing = append(missing, k)
			}
			return vals[k]
		})
	}
	sort.Strings(missing)
	return out, missing
}

func quoteArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`;&|<>(){}*?!#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Summary is the first line of the description.
func (e *Entry) Summary() string {
	d := strings.TrimSpace(e.Description)
	if i := strings.IndexByte(d, '\n'); i >= 0 {
		return strings.TrimSpace(d[:i])
	}
	return d
}

// SupportsClient reports whether the entry may be installed into client.
func (e *Entry) SupportsClient(client string) bool {
	if len(e.Clients) == 0 {
		return true
	}
	for _, c := range e.Clients {
		if c == client {
			return true
		}
	}
	return false
}

// SecretGroup returns the keyring group an input's value is stored under.
func (e *Entry) SecretGroup(in Input) string {
	if in.Share != "" {
		return in.Share
	}
	return e.ID
}

// Get returns the entry with the given ID, or nil. It also works on a
// Registry built by hand (as tests do), without Load's index.
func (r *Registry) Get(id string) *Entry {
	if r.byID != nil {
		return r.byID[id]
	}
	for _, e := range r.Entries {
		if e.ID == id {
			return e
		}
	}
	return nil
}

// Load reads the built-in and user registries and merges them.
func Load() (*Registry, error) {
	builtin, err := Parse(toolshed.DefaultRegistry)
	if err != nil {
		return nil, fmt.Errorf("built-in registry: %w", err)
	}
	userPath := UserPath()
	var user *File
	data, err := os.ReadFile(userPath)
	switch {
	case err == nil:
		if user, err = Parse(data); err != nil {
			return nil, fmt.Errorf("%s: %w", userPath, err)
		}
	case errors.Is(err, os.ErrNotExist) && os.Getenv("TOOLSHED_REGISTRY") == "":
		user = &File{}
	default:
		return nil, fmt.Errorf("read %s: %w", userPath, err)
	}
	return merge(builtin, user, userPath), nil
}

func merge(builtin, user *File, userPath string) *Registry {
	r := &Registry{UserPath: userPath, byID: map[string]*Entry{}}
	for i := range user.Tools {
		e := user.Tools[i]
		e.Source = SourceUser
		r.Entries = append(r.Entries, &e)
		r.byID[e.ID] = &e
	}
	if user.Examples == nil || *user.Examples {
		for i := range builtin.Tools {
			e := builtin.Tools[i]
			if _, overridden := r.byID[e.ID]; overridden {
				continue
			}
			e.Source = SourceBuiltin
			r.Entries = append(r.Entries, &e)
			r.byID[e.ID] = &e
		}
	}
	return r
}

// Parse decodes and validates a registry document.
func Parse(data []byte) (*File, error) {
	var f File
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // typos in field names are errors, not silent no-ops
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse: %w", err)
	}
	seen := map[string]bool{}
	var errs []error
	for i := range f.Tools {
		e := &f.Tools[i]
		if err := ValidateEntry(e); err != nil {
			errs = append(errs, fmt.Errorf("tools[%d]: %w", i, err))
		}
		if seen[e.ID] {
			errs = append(errs, fmt.Errorf("tools[%d]: duplicate id %q", i, e.ID))
		}
		seen[e.ID] = true
	}
	return &f, errors.Join(errs...)
}

// ValidateEntry checks one entry for mistakes that would otherwise surface as
// confusing failures inside Claude Code or Codex.
func ValidateEntry(e *Entry) error {
	where := e.ID
	if where == "" {
		return errors.New("id is required")
	}
	var errs []error
	if !IsValidID(e.ID) {
		errs = append(errs, fmt.Errorf("%s: id must be lowercase letters, digits and dashes", where))
	}
	switch {
	case e.Command == "" && e.URL == "":
		errs = append(errs, fmt.Errorf("%s: set either command (local server) or url (remote server)", where))
	case e.Command != "" && e.URL != "":
		errs = append(errs, fmt.Errorf("%s: set command or url, not both", where))
	case e.URL != "" && !strings.HasPrefix(e.URL, "http://") && !strings.HasPrefix(e.URL, "https://"):
		errs = append(errs, fmt.Errorf("%s: url must start with http:// or https://", where))
	}
	if len(e.Args) > 0 && e.Command == "" {
		errs = append(errs, fmt.Errorf("%s: args requires command", where))
	}
	if e.URL != "" && len(e.Env) > 0 {
		errs = append(errs, fmt.Errorf("%s: env only applies to command servers (remote servers get no environment; use headers)", where))
	}
	if e.HeaderCmd != "" && e.URL == "" {
		errs = append(errs, fmt.Errorf("%s: headers_helper only applies to url servers", where))
	}
	if len(e.Headers) > 0 && e.URL == "" {
		errs = append(errs, fmt.Errorf("%s: headers only apply to url servers (command servers get env)", where))
	}
	if len(e.Headers) > 0 && e.HeaderCmd != "" {
		errs = append(errs, fmt.Errorf("%s: set headers or headers_helper, not both", where))
	}
	if e.URL != "" {
		used := map[string]bool{}
		names := make([]string, 0, len(e.Headers))
		for name := range e.Headers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			for _, k := range HeaderKeys(e.Headers[name]) {
				used[k] = true
				if !hasInputKey(e, k) {
					errs = append(errs, fmt.Errorf("%s: header %s uses ${%s}, which isn't one of its inputs", where, name, k))
				}
			}
		}
		for _, in := range e.Inputs {
			if !used[in.Key] {
				errs = append(errs, fmt.Errorf("%s: input %s isn't used: remote servers get inputs only through ${%s} in headers", where, in.Key, in.Key))
			}
		}
	}
	for _, c := range e.Clients {
		if clients.Get(c) == nil {
			errs = append(errs, fmt.Errorf("%s: unknown client %q (want %s)", where, c, clients.Names("or")))
		}
	}
	keys := map[string]bool{}
	for _, in := range e.Inputs {
		if !isEnvKey(in.Key) {
			errs = append(errs, fmt.Errorf("%s: input key %q must look like an env var (A-Z, 0-9, _)", where, in.Key))
		}
		if keys[in.Key] {
			errs = append(errs, fmt.Errorf("%s: input %q declared twice", where, in.Key))
		}
		keys[in.Key] = true
		if in.Share != "" && !IsValidID(in.Share) {
			errs = append(errs, fmt.Errorf("%s: share %q must be lowercase letters, digits and dashes", where, in.Share))
		}
		if in.Secret && in.Default != "" {
			errs = append(errs, fmt.Errorf("%s: secret input %q cannot have a default", where, in.Key))
		}
	}
	return errors.Join(errs...)
}

// IsValidID reports whether s is a usable tool ID (kebab-case).
func IsValidID(s string) bool {
	if s == "" || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func hasInputKey(e *Entry, key string) bool {
	for _, in := range e.Inputs {
		if in.Key == key {
			return true
		}
	}
	return false
}

func isEnvKey(s string) bool {
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		return false
	}
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}
