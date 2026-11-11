package clients

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// claudeClient is Claude Code. Registrations go through `claude mcp
// add-json`, carrying over keys toolshed doesn't model.
//
// Scopes: "user" and per-project "local" live in ~/.claude.json, "project"
// in .mcp.json at the repo root.
type claudeClient struct{}

func (claudeClient) Name() string                  { return Claude }
func (claudeClient) Label() string                 { return "Claude Code" }
func (claudeClient) Available() bool               { return onPath("claude") }
func (claudeClient) Scope(requested string) string { return requested }
func (claudeClient) Notes(string) []string         { return nil }

// detect returns user, local and project registrations in the order Claude
// Code resolves a name defined at several scopes, checked against the real
// CLI: local wins over user, and user over project.
func (claudeClient) detect(root string) []found {
	var out []found
	add := func(servers map[string]json.RawMessage, scope string) {
		for name, raw := range servers {
			var cfg Config
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &cfg) != nil || json.Unmarshal(raw, &fields) != nil {
				continue
			}
			out = append(out, found{name, Registration{Scope: scope, Key: name, Config: cfg, raw: fields}})
		}
	}
	var claudeJSON struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
		Projects   map[string]struct {
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
		} `json:"projects"`
	}
	readJSON(filepath.Join(homeDir(), ".claude.json"), &claudeJSON)
	if root != "" {
		var mcpJSON struct {
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
		}
		readJSON(filepath.Join(root, ".mcp.json"), &mcpJSON)
		add(mcpJSON.MCPServers, "project")
	}
	add(claudeJSON.MCPServers, "user")
	if root != "" {
		add(claudeJSON.Projects[root].MCPServers, "local")
	}
	return out
}

// register is remove-then-add, carrying over any keys toolshed doesn't
// model (a "timeout", say), also when it moves to another scope. If the add
// fails, the previous registration at that scope is put back.
func (claudeClient) register(name string, cfg Config, scope string, prev *Registration) error {
	var carry, old map[string]json.RawMessage
	if prev != nil {
		carry = prev.raw
		if prev.Scope == scope {
			old = prev.raw
		}
	}
	data, err := claudeJSON(cfg, carry)
	if err != nil {
		return err
	}
	_ = run("claude", "mcp", "remove", name, "-s", scope) // replace, not duplicate
	err = run("claude", "mcp", "add-json", name, string(data), "-s", scope)
	if err == nil || old == nil {
		return err
	}
	restore, _ := json.Marshal(old)
	if rerr := run("claude", "mcp", "add-json", name, string(restore), "-s", scope); rerr != nil {
		return fmt.Errorf("%w (restoring the previous registration also failed: %v)", err, rerr)
	}
	return fmt.Errorf("%w (kept the previous registration)", err)
}

func (claudeClient) unregister(name, scope string, _ *Registration) error {
	return run("claude", "mcp", "remove", name, "-s", scope)
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

func readJSON(path string, v any) {
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, v)
	}
}
