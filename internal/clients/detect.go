package clients

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Installation is where one server name is registered.
type Installation struct {
	Name        string
	ClaudeScope string // "user", "project", "local", or "" when not in Claude
	Codex       bool
	CodexScope  string // "user" ($CODEX_HOME/config.toml) or "project" (<repo>/.codex/config.toml)
	CodexName   string // the key in Codex's config (older toolshed wrote "my_tool" for "my-tool")
	Config      Config // Claude's config if present, else Codex's
	CodexConfig Config // Codex's own config, when Codex has it

	claudeRaw map[string]json.RawMessage // Claude's config as stored, to keep keys toolshed doesn't model
}

// Clients returns the clients this server is registered in.
func (i Installation) Clients() []string {
	var out []string
	if i.ClaudeScope != "" {
		out = append(out, Claude)
	}
	if i.Codex {
		out = append(out, Codex)
	}
	return out
}

// Scope returns the scope the server is registered at in one client.
func (i Installation) Scope(client string) string {
	if client == Codex {
		return i.CodexScope
	}
	return i.ClaudeScope
}

// Detect returns every server registered in Claude Code or Codex, keyed by
// name. Codex names with underscores are also folded into their hyphen form
// when nothing else has that name, because toolshed used to register
// "my-tool" as "my_tool" in Codex. Unreadable or missing config files are
// treated as empty.
func Detect() map[string]*Installation {
	found := map[string]*Installation{}
	get := func(name string) *Installation {
		if found[name] == nil {
			found[name] = &Installation{Name: name}
		}
		return found[name]
	}

	home, _ := os.UserHomeDir()
	root := GitRoot()

	// Claude: user scope and per-project "local" scope live in ~/.claude.json;
	// project scope lives in .mcp.json at the repo root.
	var claudeJSON struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
		Projects   map[string]struct {
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
		} `json:"projects"`
	}
	readJSON(filepath.Join(home, ".claude.json"), &claudeJSON)
	claude := func(servers map[string]json.RawMessage, scope string) {
		for name, raw := range servers {
			var cfg Config
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &cfg) != nil || json.Unmarshal(raw, &fields) != nil {
				continue
			}
			inst := get(name)
			inst.ClaudeScope, inst.Config, inst.claudeRaw = scope, cfg, fields
		}
	}
	claude(claudeJSON.MCPServers, "user")
	if root != "" {
		claude(claudeJSON.Projects[root].MCPServers, "local")
		var mcpJSON struct {
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
		}
		readJSON(filepath.Join(root, ".mcp.json"), &mcpJSON)
		claude(mcpJSON.MCPServers, "project")
	}

	// Codex: [mcp_servers.<name>] tables in $CODEX_HOME/config.toml, and in
	// <repo>/.codex/config.toml (used once the user trusts the project).
	codex := func(path, scope string) {
		var f struct {
			MCPServers map[string]codexTOMLServer `toml:"mcp_servers"`
		}
		if _, err := toml.DecodeFile(path, &f); err != nil {
			return
		}
		for key, s := range f.MCPServers {
			name := key
			if folded := strings.ReplaceAll(key, "_", "-"); folded != key {
				if _, taken := f.MCPServers[folded]; !taken {
					name = folded
				}
			}
			inst := get(name)
			inst.Codex, inst.CodexScope, inst.CodexName, inst.CodexConfig = true, scope, key, s.config()
			if inst.ClaudeScope == "" {
				inst.Config = inst.CodexConfig
			}
		}
	}
	codex(filepath.Join(CodexHome(), "config.toml"), "user")
	if root != "" {
		codex(filepath.Join(root, ".codex", "config.toml"), "project")
	}
	return found
}

// CodexHome is $CODEX_HOME, or ~/.codex.
func CodexHome() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

// codexConfigPath is the config file for a Codex scope.
func codexConfigPath(scope string) (string, error) {
	if scope != "project" {
		return filepath.Join(CodexHome(), "config.toml"), nil
	}
	root := GitRoot()
	if root == "" {
		return "", fmt.Errorf("project scope needs a git repository")
	}
	return filepath.Join(root, ".codex", "config.toml"), nil
}

// CodexTrusts reports whether Codex trusts the current repository. Codex
// ignores a project's .codex/config.toml until the user trusts it.
func CodexTrusts() bool {
	root := GitRoot()
	if root == "" {
		return false
	}
	var f struct {
		Projects map[string]struct {
			TrustLevel string `toml:"trust_level"`
		} `toml:"projects"`
	}
	if _, err := toml.DecodeFile(filepath.Join(CodexHome(), "config.toml"), &f); err != nil {
		return false
	}
	want, _ := filepath.EvalSymlinks(root)
	for p, v := range f.Projects {
		if q, err := filepath.EvalSymlinks(p); err == nil && q == want && v.TrustLevel == "trusted" {
			return true
		}
	}
	return false
}

func readJSON(path string, v any) {
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, v)
	}
}

// GitRoot returns the current git repository root, or "".
func GitRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
