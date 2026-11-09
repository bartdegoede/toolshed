// Package clients registers MCP servers with Claude Code and Codex, and
// reads back what each client currently has configured.
//
// Claude registrations go through `claude mcp add-json`. Codex's are written
// to its config.toml directly, editing only toolshed's keys, because `codex
// mcp add` resets the whole server table (and can't set a headers helper).
// Detection reads the config files directly because it's ~1000x faster than
// `claude mcp list`, which health-checks every server.
package clients

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const (
	Claude = "claude"
	Codex  = "codex"
)

// All lists supported clients in display order.
var All = []string{Claude, Codex}

// Config is a server registration, in Claude Code's add-json shape.
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

// lookPath is swapped in tests.
var lookPath = exec.LookPath

// Available reports whether a client's CLI is on PATH.
func Available(client string) bool {
	_, err := lookPath(client)
	return err == nil
}

// AvailableClients returns the installed clients, in display order.
func AvailableClients() []string {
	var out []string
	for _, c := range All {
		if Available(c) {
			out = append(out, c)
		}
	}
	return out
}

// Register adds (or replaces) a server in one client at a scope: Claude
// takes "user", "local" or "project"; Codex takes "user" or "project".
//
// Claude: remove-then-add through `claude mcp add-json`, carrying over any
// keys toolshed doesn't model (a "timeout", say). If the add fails, the
// previous registration is put back. Codex: its config.toml is edited in
// place (see codextoml.go), which never leaves it half-written.
func Register(client, name string, cfg Config, scope string) error {
	inst := Detect()[name]
	switch client {
	case Claude:
		var prev map[string]json.RawMessage
		if inst != nil && inst.ClaudeScope == scope {
			prev = inst.claudeRaw
		}
		data, err := claudeJSON(cfg, prev)
		if err != nil {
			return err
		}
		_ = run("claude", "mcp", "remove", name, "-s", scope) // replace, not duplicate
		err = run("claude", "mcp", "add-json", name, string(data), "-s", scope)
		if err == nil || prev == nil {
			return err
		}
		old, _ := json.Marshal(prev)
		if rerr := run("claude", "mcp", "add-json", name, string(old), "-s", scope); rerr != nil {
			return fmt.Errorf("%w (restoring the previous registration also failed: %v)", err, rerr)
		}
		return fmt.Errorf("%w (kept the previous registration)", err)
	case Codex:
		path, err := codexConfigPath(scope)
		if err != nil {
			return err
		}
		legacy := ""
		if inst != nil && inst.Codex && inst.CodexScope == codexScope(scope) {
			legacy = inst.CodexName
		}
		return writeCodexServer(path, name, legacy, codexServerFrom(cfg))
	}
	return fmt.Errorf("unknown client %q", client)
}

// codexScope maps a Claude-style scope to Codex's: Codex has no "local".
func codexScope(scope string) string {
	if scope == "project" {
		return "project"
	}
	return "user"
}

// claudeKeys are the keys toolshed sets in a Claude registration; any other
// key in an existing registration is kept.
var claudeKeys = []string{"type", "command", "args", "env", "url", "headers", "headersHelper"}

func claudeJSON(cfg Config, prev map[string]json.RawMessage) ([]byte, error) {
	data, err := json.Marshal(cfg)
	if err != nil || len(prev) == 0 {
		return data, err
	}
	merged := map[string]json.RawMessage{}
	for k, v := range prev {
		if !contains(claudeKeys, k) {
			merged[k] = v
		}
	}
	// A remote server keeps its static headers unless toolshed now provides them.
	if cfg.URL != "" && cfg.HeadersHelper == "" && len(cfg.Headers) == 0 {
		if h, ok := prev["headers"]; ok {
			merged["headers"] = h
		}
	}
	var mine map[string]json.RawMessage
	if err := json.Unmarshal(data, &mine); err != nil {
		return nil, err
	}
	for k, v := range mine {
		merged[k] = v
	}
	return json.Marshal(merged)
}

// Unregister removes a server from one client at a scope.
func Unregister(client, name, scope string) error {
	switch client {
	case Claude:
		return run("claude", "mcp", "remove", name, "-s", scope)
	case Codex:
		path, err := codexConfigPath(scope)
		if err != nil {
			return err
		}
		legacy := ""
		if inst := Detect()[name]; inst != nil && inst.Codex {
			legacy = inst.CodexName
		}
		return writeCodexServer(path, name, legacy, nil)
	}
	return fmt.Errorf("unknown client %q", client)
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
