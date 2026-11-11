package clients

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// codexClient is Codex. Its config.toml is edited directly, only toolshed's
// keys (see codextoml.go), because `codex mcp add` resets the whole server
// table (and can't set a headers helper).
//
// Scopes: "user" is $CODEX_HOME/config.toml, "project" is
// <repo>/.codex/config.toml, which Codex reads once the project is trusted.
type codexClient struct{}

func (codexClient) Name() string    { return Codex }
func (codexClient) Label() string   { return "Codex" }
func (codexClient) Available() bool { return onPath("codex") }

// Scope: Codex has no "local".
func (codexClient) Scope(requested string) string {
	if requested == "project" {
		return "project"
	}
	return "user"
}

func (codexClient) Notes(scope string) []string {
	if scope == "project" && !codexTrusts() {
		return []string{"Codex only loads this repository's .codex/config.toml once you trust the project (start codex here and accept)"}
	}
	return nil
}

// detect reads the [mcp_servers.<name>] tables. Names with underscores are
// folded into their hyphen form when nothing else in the file has that name,
// because toolshed used to register "my-tool" as "my_tool" in Codex.
func (codexClient) detect(root string) map[string]Registration {
	found := map[string]Registration{}
	read := func(path, scope string) {
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
			found[name] = Registration{Scope: scope, Key: key, Config: s.config()}
		}
	}
	read(filepath.Join(CodexHome(), "config.toml"), "user")
	if root != "" {
		read(filepath.Join(root, ".codex", "config.toml"), "project")
	}
	return found
}

func (c codexClient) register(name string, cfg Config, scope string, prev *Registration) error {
	path, err := codexConfigPath(scope)
	if err != nil {
		return err
	}
	legacy := ""
	if prev != nil && prev.Scope == c.Scope(scope) {
		legacy = prev.Key
	}
	return writeCodexServer(path, name, legacy, codexServerFrom(cfg))
}

func (codexClient) unregister(name, scope string, prev *Registration) error {
	path, err := codexConfigPath(scope)
	if err != nil {
		return err
	}
	legacy := ""
	if prev != nil {
		legacy = prev.Key
	}
	return writeCodexServer(path, name, legacy, nil)
}

// CodexHome is $CODEX_HOME, or ~/.codex.
func CodexHome() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h
	}
	return filepath.Join(homeDir(), ".codex")
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

// codexTrusts reports whether Codex trusts the current repository. Codex
// ignores a project's .codex/config.toml until the user trusts it.
func codexTrusts() bool {
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
