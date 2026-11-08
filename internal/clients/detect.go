package clients

import (
	"encoding/json"
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
	Config      Config // Claude's config if present, else Codex's
	CodexConfig Config // Codex's own config, when Codex has it
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

// Detect returns every server registered in Claude Code or Codex, keyed by
// name (Codex's underscore names are folded into their hyphen form).
// Unreadable or missing config files are treated as empty.
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
		MCPServers map[string]Config `json:"mcpServers"`
		Projects   map[string]struct {
			MCPServers map[string]Config `json:"mcpServers"`
		} `json:"projects"`
	}
	readJSON(filepath.Join(home, ".claude.json"), &claudeJSON)
	for name, cfg := range claudeJSON.MCPServers {
		inst := get(name)
		inst.ClaudeScope, inst.Config = "user", cfg
	}
	if root != "" {
		for name, cfg := range claudeJSON.Projects[root].MCPServers {
			inst := get(name)
			inst.ClaudeScope, inst.Config = "local", cfg
		}
		var mcpJSON struct {
			MCPServers map[string]Config `json:"mcpServers"`
		}
		readJSON(filepath.Join(root, ".mcp.json"), &mcpJSON)
		for name, cfg := range mcpJSON.MCPServers {
			inst := get(name)
			inst.ClaudeScope, inst.Config = "project", cfg
		}
	}

	// Codex: [mcp_servers.<name>] tables in $CODEX_HOME/config.toml.
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	var codexTOML struct {
		MCPServers map[string]struct {
			Command string            `toml:"command"`
			Args    []string          `toml:"args"`
			Env     map[string]string `toml:"env"`
			URL     string            `toml:"url"`
		} `toml:"mcp_servers"`
	}
	if _, err := toml.DecodeFile(filepath.Join(codexHome, "config.toml"), &codexTOML); err == nil {
		for name, s := range codexTOML.MCPServers {
			inst := get(strings.ReplaceAll(name, "_", "-"))
			inst.Codex = true
			typ := "stdio"
			if s.URL != "" {
				typ = "http"
			}
			inst.CodexConfig = Config{Type: typ, Command: s.Command, Args: s.Args, Env: s.Env, URL: s.URL}
			if inst.ClaudeScope == "" {
				inst.Config = inst.CodexConfig
			}
		}
	}
	return found
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
