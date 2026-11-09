package registry

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

// Kinds of tool a Spec can describe.
const (
	KindCommand = "command" // any executable that speaks MCP over stdio
	KindNPX     = "npx"     // an npm package run with `npx -y`
	KindUVX     = "uvx"     // a Python package run with `uvx`
	KindURL     = "url"     // a remote MCP server
)

// Spec is the short form of a new tool, as typed by a person (or an agent):
// a kind plus "what to run". The TUI's add form, `toolshed add` and
// `toolshed mcp` all build entries through it.
type Spec struct {
	Kind        string
	Source      string // command line, package (+ args), or URL
	ID          string // optional; derived from Source when empty
	Name        string
	Description string
	Secrets     []string          // env vars to prompt for and keep in the keyring
	Headers     map[string]string // url only: HTTP headers, ${KEY} for a secret
}

// Entry converts the spec into a validated registry entry.
func (s Spec) Entry() (Entry, error) {
	fields := strings.Fields(s.Source)
	if len(fields) == 0 {
		return Entry{}, errors.New("nothing to run: give a command, package or URL")
	}
	e := Entry{
		ID:          strings.TrimSpace(s.ID),
		Name:        strings.TrimSpace(s.Name),
		Description: strings.TrimSpace(s.Description),
	}
	if e.ID == "" {
		e.ID = SuggestID(s.Kind, s.Source)
	}
	switch s.Kind {
	case KindURL:
		e.URL = fields[0]
	case KindNPX:
		e.Command, e.Args = "npx", append([]string{"-y"}, fields...)
	case KindUVX:
		e.Command, e.Args = "uvx", fields
	case KindCommand, "":
		e.Command, e.Args = fields[0], fields[1:]
	default:
		return Entry{}, errors.New("kind must be command, npx, uvx or url")
	}
	for _, k := range s.Secrets {
		if k = strings.ToUpper(strings.TrimSpace(k)); k != "" {
			e.Inputs = append(e.Inputs, Input{Key: k, Secret: true})
		}
	}
	if len(s.Headers) > 0 {
		e.Headers = s.Headers
	}
	return e, ValidateEntry(&e)
}

// SuggestID derives a tool ID from what the user typed:
// "@acme/mcp-server-notes" → "notes", "https://mcp.deepwiki.com/mcp" → "deepwiki".
func SuggestID(kind, source string) string {
	fields := strings.Fields(source)
	if len(fields) == 0 {
		return ""
	}
	base := fields[0]
	switch kind {
	case KindURL:
		u, err := url.Parse(base)
		if err != nil || u.Hostname() == "" {
			return ""
		}
		labels := strings.Split(u.Hostname(), ".")
		if len(labels) > 1 {
			labels = labels[:len(labels)-1] // drop TLD
		}
		for _, l := range labels {
			if l != "mcp" && l != "www" && l != "api" {
				return sanitizeID(l)
			}
		}
		return sanitizeID(labels[len(labels)-1])
	case KindNPX, KindUVX:
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		if base == "" { // "@scope/" while the user is still typing
			return ""
		}
		if i := strings.Index(base[1:], "@"); i >= 0 { // strip @version
			base = base[:i+1]
		}
	default:
		base = filepath.Base(base)
	}
	id := strings.ToLower(base)
	for _, p := range []string{"mcp-server-", "server-", "mcp-"} {
		id = strings.TrimPrefix(id, p)
	}
	for _, s := range []string{"-mcp-server", "-mcp", "-server"} {
		id = strings.TrimSuffix(id, s)
	}
	return sanitizeID(id)
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
