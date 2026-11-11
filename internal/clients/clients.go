// Package clients registers MCP servers with the clients toolshed supports
// (Claude Code and Codex), and reads back what each one currently has
// configured.
//
// Each client is an adapter (claude.go, codex.go) behind the Client
// interface. Everything outside this package works with client names and the
// neutral Config, so supporting another client means adding an adapter here.
//
// Detection reads the config files directly because it's ~1000x faster than
// `claude mcp list`, which health-checks every server.
package clients

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Client is one MCP client toolshed can register servers with.
type Client interface {
	Name() string  // "claude": what --client, the registry and the JSON output use
	Label() string // "Claude Code": what people read
	// Available reports whether the client is installed on this machine.
	Available() bool
	// Scope maps a requested scope ("user", "local" or "project") to the
	// nearest one this client has.
	Scope(requested string) string
	// Notes are things to know after registering a server at scope.
	Notes(scope string) []string

	// detect returns the servers in the client's config files, by name.
	// root is the git repository root, or "".
	detect(root string) map[string]Registration
	// register adds or replaces a server; prev is its current registration
	// in this client, if any.
	register(name string, cfg Config, scope string, prev *Registration) error
	// unregister removes a server; prev is as for register.
	unregister(name, scope string, prev *Registration) error
}

const (
	Claude = "claude"
	Codex  = "codex"
)

// adapters lists supported clients in display order.
var adapters = []Client{claudeClient{}, codexClient{}}

// All lists supported client names in display order.
var All = func() []string {
	var names []string
	for _, c := range adapters {
		names = append(names, c.Name())
	}
	return names
}()

// Get returns the client called name, or nil.
func Get(name string) Client {
	for _, c := range adapters {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// Label returns a client's display name ("Claude Code"), or name itself for
// an unknown client.
func Label(name string) string {
	if c := Get(name); c != nil {
		return c.Label()
	}
	return name
}

// Labels lists every client's display name for a sentence, joining the
// last two with conj: "Claude Code or Codex".
func Labels(conj string) string {
	var labels []string
	for _, c := range adapters {
		labels = append(labels, c.Label())
	}
	return joinList(labels, conj)
}

// Names is Labels with client names: "claude or codex".
func Names(conj string) string {
	return joinList(All, conj)
}

func joinList(items []string, conj string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " " + conj + " " + items[len(items)-1]
}

// Config is a server registration, in Claude Code's add-json shape. Each
// adapter translates it to and from its client's own format.
type Config struct {
	Type          string            `json:"type"` // "stdio" or "http"
	Command       string            `json:"command,omitempty"`
	Args          []string          `json:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	URL           string            `json:"url,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"` // static headers (Codex: http_headers)
	HeadersHelper string            `json:"headersHelper,omitempty"`

	// Codex only, read for import and doctor; toolshed never writes them.
	BearerTokenEnvVar string            `json:"-"`
	EnvHeaders        map[string]string `json:"-"`
}

// Registration is a server as one client has it.
type Registration struct {
	Scope  string // "user", "project", or Claude's "local"
	Key    string // the server's key in the client's config (older toolshed wrote Codex's "my-tool" as "my_tool")
	Config Config

	raw map[string]json.RawMessage // the client's own copy, for keys toolshed doesn't model (Claude)
}

// Installation is where one server name is registered.
type Installation struct {
	Name string
	Regs map[string]Registration // by client name
}

// Clients returns the clients this server is registered in, in display order.
func (i Installation) Clients() []string {
	var out []string
	for _, c := range All {
		if _, ok := i.Regs[c]; ok {
			out = append(out, c)
		}
	}
	return out
}

// Has reports whether the server is registered in client.
func (i Installation) Has(client string) bool {
	_, ok := i.Regs[client]
	return ok
}

// Scope returns the scope the server is registered at in one client, or "".
func (i Installation) Scope(client string) string {
	return i.Regs[client].Scope
}

// Config returns the first client's config, in display order: the one to
// show, and to compare the others with.
func (i Installation) Config() Config {
	for _, c := range i.Clients() {
		return i.Regs[c].Config
	}
	return Config{}
}

// Configs returns every client's config, in display order.
func (i Installation) Configs() []Config {
	var out []Config
	for _, c := range i.Clients() {
		out = append(out, i.Regs[c].Config)
	}
	return out
}

// lookPath is swapped in tests.
var lookPath = exec.LookPath

// onPath reports whether a command is on PATH.
func onPath(name string) bool {
	_, err := lookPath(name)
	return err == nil
}

// Available reports whether a client is installed.
func Available(client string) bool {
	c := Get(client)
	return c != nil && c.Available()
}

// AvailableClients returns the installed clients, in display order.
func AvailableClients() []string {
	var out []string
	for _, c := range adapters {
		if c.Available() {
			out = append(out, c.Name())
		}
	}
	return out
}

// Detect returns every server registered in any client, keyed by name.
// Unreadable or missing config files are treated as empty.
func Detect() map[string]*Installation {
	found := map[string]*Installation{}
	root := GitRoot()
	for _, c := range adapters {
		for name, r := range c.detect(root) {
			inst := found[name]
			if inst == nil {
				inst = &Installation{Name: name, Regs: map[string]Registration{}}
				found[name] = inst
			}
			inst.Regs[c.Name()] = r
		}
	}
	return found
}

// Register adds (or replaces) a server in one client at a scope, keeping the
// settings toolshed doesn't manage. scope is one the client has (see
// Client.Scope).
//
// A server lives at one scope per client: registering it at another scope
// moves it, settings and all, and removes the old registration once the new
// one is in place. Otherwise the old one would linger where Detect, which
// reports one scope per client, doesn't show it and uninstall doesn't reach.
func Register(client, name string, cfg Config, scope string) error {
	c := Get(client)
	if c == nil {
		return fmt.Errorf("unknown client %q", client)
	}
	prev := current(client, name)
	if err := c.register(name, cfg, scope, prev); err != nil {
		return err
	}
	if prev != nil && prev.Scope != c.Scope(scope) {
		if err := c.unregister(name, prev.Scope, prev); err != nil {
			return fmt.Errorf("registered at %s scope, but removing the %s one failed: %w", c.Scope(scope), prev.Scope, err)
		}
	}
	return nil
}

// Unregister removes a server from one client at a scope.
func Unregister(client, name, scope string) error {
	c := Get(client)
	if c == nil {
		return fmt.Errorf("unknown client %q", client)
	}
	return c.unregister(name, scope, current(client, name))
}

// current is the server's registration in client, or nil.
func current(client, name string) *Registration {
	if inst := Detect()[name]; inst != nil {
		if r, ok := inst.Regs[client]; ok {
			return &r
		}
	}
	return nil
}

// GitRoot returns the current git repository root, or "".
func GitRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// homeDir is the user's home directory, or "" (then nothing is found there).
func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("%s %s: %w", name, args[0], err)
		}
		return errors.New(msg)
	}
	return nil
}
