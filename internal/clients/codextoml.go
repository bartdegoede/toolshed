package clients

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/pelletier/go-toml/v2/unstable"
)

// Codex registrations are written to config.toml directly rather than with
// `codex mcp add`: that command replaces the whole server table with
// defaults, so every update would wipe the user's settings for it
// (startup_timeout_sec, enabled/disabled tools, per-tool approvals, comments),
// and it can't set http_headers_helper at all.
//
// editCodexServer edits only the keys toolshed owns in [mcp_servers.<name>]
// and leaves every other byte of the file as it was.

// Keys toolshed writes, per transport. Writing one transport also removes the
// other transport's keys, which Codex rejects in the same table.
var (
	codexStdioKeys = []string{"command", "args", "env"}
	codexHTTPKeys  = []string{"url", "http_headers_helper", "http_headers"}
	// User settings that only make sense for one transport: kept while the
	// transport stays the same, removed when it changes.
	codexStdioOnly = []string{"env_vars", "cwd"}
	codexHTTPOnly  = []string{"bearer_token_env_var", "env_http_headers"}
)

type tomlTable struct {
	path                 []string
	headerStart, bodyEnd int // header line start; end of its last key/value line
	headerEnd            int // end of the header line (after '\n')
}

type tomlKeyVal struct {
	table      []string // path of the table it's in
	key        []string // dotted key, relative to that table
	start, end int      // whole line(s), including the trailing newline
}

func parseTOMLLayout(doc []byte) ([]*tomlTable, []tomlKeyVal, error) {
	var tables []*tomlTable
	var kvs []tomlKeyVal
	var cur []string
	var curTable *tomlTable
	p := unstable.Parser{}
	p.Reset(doc)
	for p.NextExpression() {
		e := p.Expression()
		switch e.Kind {
		case unstable.Table, unstable.ArrayTable:
			var path []string
			first := -1
			for it := e.Key(); it.Next(); {
				k := it.Node()
				if first < 0 {
					first = int(k.Raw.Offset)
				}
				path = append(path, string(k.Data))
			}
			start := lineStart(doc, first)
			end := lineEnd(doc, first)
			curTable = &tomlTable{path: path, headerStart: start, headerEnd: end, bodyEnd: end}
			tables = append(tables, curTable)
			cur = path
		case unstable.KeyValue:
			var key []string
			first := -1
			for it := e.Key(); it.Next(); {
				k := it.Node()
				if first < 0 {
					first = int(k.Raw.Offset)
				}
				key = append(key, string(k.Data))
			}
			end := lineEnd(doc, int(e.Raw.Offset+e.Raw.Length)-1)
			kvs = append(kvs, tomlKeyVal{table: cur, key: key, start: lineStart(doc, first), end: end})
			if curTable != nil {
				curTable.bodyEnd = end
			}
		}
	}
	return tables, kvs, p.Error()
}

// withBlankBefore extends a removal to one blank line in front of it, so
// removing a table doesn't leave a double gap.
func withBlankBefore(doc []byte, start int) int {
	if start >= 2 && doc[start-1] == '\n' && doc[start-2] == '\n' {
		return start - 1
	}
	return start
}

func lineStart(doc []byte, off int) int {
	if i := bytes.LastIndexByte(doc[:off], '\n'); i >= 0 {
		return i + 1
	}
	return 0
}

func lineEnd(doc []byte, off int) int {
	if i := bytes.IndexByte(doc[off:], '\n'); i >= 0 {
		return off + i + 1
	}
	return len(doc)
}

func hasPrefix(path, prefix []string) bool {
	if len(path) < len(prefix) {
		return false
	}
	for i := range prefix {
		if path[i] != prefix[i] {
			return false
		}
	}
	return true
}

// errCodexLayout means the server is written in a shape toolshed won't edit
// (an inline table, or dotted keys outside its own table).
var errCodexLayout = errors.New("unsupported layout")

type codexServer struct {
	Command       string
	Args          []string
	Env           map[string]string
	URL           string
	HeadersHelper string
	Headers       map[string]string
}

func codexServerFrom(cfg Config) *codexServer {
	return &codexServer{Command: cfg.Command, Args: cfg.Args, Env: cfg.Env, URL: cfg.URL,
		HeadersHelper: cfg.HeadersHelper, Headers: cfg.Headers}
}

type edit struct {
	start, end int
	text       string
}

// editCodexServer sets (s != nil) or removes (s == nil) the server name in a
// Codex config. legacy, when set, is an older name for the same server
// (toolshed used to register "my-tool" as "my_tool"): its table is renamed.
func editCodexServer(doc []byte, name, legacy string, s *codexServer) ([]byte, error) {
	tables, kvs, err := parseTOMLLayout(doc)
	if err != nil {
		return nil, err
	}
	main := []string{"mcp_servers", name}
	find := func(path []string) *tomlTable {
		for _, t := range tables {
			if reflect.DeepEqual(t.path, path) {
				return t
			}
		}
		return nil
	}
	// The server defined anywhere but in its own tables: leave it to the user.
	for _, kv := range kvs {
		full := append(append([]string{}, kv.table...), kv.key...)
		if len(kv.key) > 0 && !hasPrefix(kv.table, main) && hasPrefix(full, main) {
			return nil, fmt.Errorf("%w: mcp_servers.%s isn't a [mcp_servers.%s] table", errCodexLayout, name, name)
		}
	}

	var edits []edit
	if s == nil { // remove every table of the server, under either name
		for _, t := range tables {
			if hasPrefix(t.path, main) || legacy != "" && hasPrefix(t.path, []string{"mcp_servers", legacy}) {
				edits = append(edits, edit{withBlankBefore(doc, t.headerStart), t.bodyEnd, ""})
			}
		}
		return applyEdits(doc, edits), nil
	}
	if legacy != "" && legacy != name && find(main) == nil && find([]string{"mcp_servers", legacy}) != nil {
		old := []string{"mcp_servers", legacy}
		for _, t := range tables {
			if hasPrefix(t.path, old) {
				header := "[" + tomlPath(append(append([]string{}, main...), t.path[2:]...)) + "]\n"
				if bytes.HasPrefix(bytes.TrimSpace(doc[t.headerStart:t.headerEnd]), []byte("[[")) {
					header = "[" + header[:len(header)-1] + "]\n"
				}
				edits = append(edits, edit{t.headerStart, t.headerEnd, header})
				t.path = append(append([]string{}, main...), t.path[2:]...)
			}
		}
		for i := range kvs {
			if hasPrefix(kvs[i].table, old) {
				kvs[i].table = append(append([]string{}, main...), kvs[i].table[2:]...)
			}
		}
	}

	write, drop := codexStdioKeys, append(append([]string{}, codexHTTPKeys...), codexHTTPOnly...)
	if s.URL != "" {
		write, drop = codexHTTPKeys, append(append([]string{}, codexStdioKeys...), codexStdioOnly...)
		if s.HeadersHelper == "" && len(s.Headers) == 0 {
			write = []string{"url", "http_headers_helper"} // keep the user's own http_headers
		}
	}
	owned := append(append([]string{}, write...), drop...)
	for _, kv := range kvs {
		if reflect.DeepEqual(kv.table, main) && contains(owned, kv.key[0]) {
			edits = append(edits, edit{kv.start, kv.end, ""})
		}
	}
	for _, t := range tables { // [mcp_servers.<name>.env] and friends
		if len(t.path) == 3 && hasPrefix(t.path, main) && contains(owned, t.path[2]) {
			edits = append(edits, edit{withBlankBefore(doc, t.headerStart), t.bodyEnd, ""})
		}
	}
	lines := codexLines(s)
	if t := find(main); t != nil {
		edits = append(edits, edit{t.headerEnd, t.headerEnd, lines})
	} else {
		prefix := ""
		if len(doc) > 0 && !bytes.HasSuffix(doc, []byte("\n")) {
			prefix = "\n"
		}
		if len(bytes.TrimSpace(doc)) > 0 {
			prefix += "\n"
		}
		edits = append(edits, edit{len(doc), len(doc), prefix + "[" + tomlPath(main) + "]\n" + lines})
	}
	return applyEdits(doc, edits), nil
}

func applyEdits(doc []byte, edits []edit) []byte {
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var out bytes.Buffer
	pos := 0
	for _, e := range edits {
		if e.start < pos { // overlapping (a key inside a removed table)
			if e.end > pos {
				pos = e.end
			}
			out.WriteString(e.text)
			continue
		}
		out.Write(doc[pos:e.start])
		out.WriteString(e.text)
		pos = e.end
	}
	out.Write(doc[pos:])
	return out.Bytes()
}

func codexLines(s *codexServer) string {
	var b strings.Builder
	if s.URL != "" {
		fmt.Fprintf(&b, "url = %s\n", tomlString(s.URL))
		if s.HeadersHelper != "" {
			fmt.Fprintf(&b, "http_headers_helper = %s\n", tomlString(s.HeadersHelper))
		}
		if len(s.Headers) > 0 {
			fmt.Fprintf(&b, "http_headers = %s\n", tomlInlineTable(s.Headers))
		}
		return b.String()
	}
	fmt.Fprintf(&b, "command = %s\n", tomlString(s.Command))
	if len(s.Args) > 0 {
		parts := make([]string, len(s.Args))
		for i, a := range s.Args {
			parts[i] = tomlString(a)
		}
		fmt.Fprintf(&b, "args = [%s]\n", strings.Join(parts, ", "))
	}
	if len(s.Env) > 0 {
		fmt.Fprintf(&b, "env = %s\n", tomlInlineTable(s.Env))
	}
	return b.String()
}

func tomlInlineTable(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = tomlKey(k) + " = " + tomlString(m[k])
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func tomlPath(path []string) string {
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = tomlKey(p)
	}
	return strings.Join(parts, ".")
}

func tomlKey(k string) string {
	if k != "" && strings.Trim(k, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") == "" {
		return k
	}
	return tomlString(k)
}

// tomlString is a TOML basic string (only the escapes TOML allows).
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// extractCodexServer returns the text of a server's tables (its own and its
// subtables, such as [mcp_servers.<key>.tools.search]), to carry its
// settings to another config file when it moves scope.
func extractCodexServer(doc []byte, key string) ([]byte, error) {
	tables, kvs, err := parseTOMLLayout(doc)
	if err != nil {
		return nil, err
	}
	main := []string{"mcp_servers", key}
	for _, kv := range kvs {
		full := append(append([]string{}, kv.table...), kv.key...)
		if len(kv.key) > 0 && !hasPrefix(kv.table, main) && hasPrefix(full, main) {
			return nil, fmt.Errorf("%w: mcp_servers.%s isn't a [mcp_servers.%s] table", errCodexLayout, key, key)
		}
	}
	var parts [][]byte
	for _, t := range tables {
		if hasPrefix(t.path, main) {
			parts = append(parts, bytes.TrimRight(doc[t.headerStart:t.bodyEnd], "\n"))
		}
	}
	if len(parts) == 0 {
		return nil, nil
	}
	return append(bytes.Join(parts, []byte("\n\n")), '\n'), nil
}

// hasCodexServer reports whether doc has a table for the server under name
// or legacy.
func hasCodexServer(doc []byte, name, legacy string) bool {
	tables, _, err := parseTOMLLayout(doc)
	if err != nil {
		return true // let editCodexServer report it
	}
	for _, t := range tables {
		if hasPrefix(t.path, []string{"mcp_servers", name}) || legacy != "" && hasPrefix(t.path, []string{"mcp_servers", legacy}) {
			return true
		}
	}
	return false
}

// writeCodexServer applies editCodexServer to a config file, checks that the
// result parses and says what was intended, and replaces the file atomically.
// seed, when the file doesn't have the server yet, is its tables from another
// config file (see extractCodexServer): they're added first, so the settings
// toolshed doesn't manage move with it.
func writeCodexServer(path, name, legacy string, seed []byte, s *codexServer) error {
	doc, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(seed) > 0 && !hasCodexServer(doc, name, legacy) {
		if len(doc) > 0 && !bytes.HasSuffix(doc, []byte("\n")) {
			doc = append(doc, '\n')
		}
		if len(bytes.TrimSpace(doc)) > 0 {
			doc = append(doc, '\n')
		}
		doc = append(doc, seed...)
	}
	out, err := editCodexServer(doc, name, legacy, s)
	if err != nil {
		return fmt.Errorf("editing %s: %w", path, err)
	}
	var check struct {
		MCPServers map[string]codexTOMLServer `toml:"mcp_servers"`
	}
	if _, err := toml.Decode(string(out), &check); err != nil {
		return fmt.Errorf("editing %s produced invalid TOML (nothing was written): %w", path, err)
	}
	got, ok := check.MCPServers[name]
	if s == nil {
		if ok {
			return fmt.Errorf("editing %s: %s is still there (nothing was written)", path, name)
		}
	} else if !ok || !got.matches(s) {
		return fmt.Errorf("editing %s didn't give the intended [mcp_servers.%s] (nothing was written)", path, name)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// codexTOMLServer is how Codex stores one server.
type codexTOMLServer struct {
	Command           string            `toml:"command"`
	Args              []string          `toml:"args"`
	Env               map[string]string `toml:"env"`
	URL               string            `toml:"url"`
	HeadersHelper     string            `toml:"http_headers_helper"`
	Headers           map[string]string `toml:"http_headers"`
	BearerTokenEnvVar string            `toml:"bearer_token_env_var"`
	EnvHTTPHeaders    map[string]string `toml:"env_http_headers"`
}

func (c codexTOMLServer) matches(s *codexServer) bool {
	eq := func(a, b map[string]string) bool { return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b) }
	if s.URL != "" {
		return c.URL == s.URL && c.HeadersHelper == s.HeadersHelper && (len(s.Headers) == 0 || eq(c.Headers, s.Headers)) && c.Command == ""
	}
	return c.Command == s.Command && (len(c.Args) == 0 && len(s.Args) == 0 || reflect.DeepEqual(c.Args, s.Args)) && eq(c.Env, s.Env) && c.URL == ""
}

func (c codexTOMLServer) config() Config {
	cfg := Config{Type: "stdio", Command: c.Command, Args: c.Args, Env: c.Env}
	if c.URL != "" {
		cfg = Config{Type: "http", URL: c.URL, HeadersHelper: c.HeadersHelper, Headers: c.Headers,
			BearerTokenEnvVar: c.BearerTokenEnvVar, EnvHeaders: c.EnvHTTPHeaders}
	}
	return cfg
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
