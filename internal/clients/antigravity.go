package clients

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
)

// antigravityClient is Antigravity CLI (`agy`), Gemini CLI's successor.
// Checked against agy 1.5.5:
//
//   - servers under "mcpServers" in ~/.gemini/config/mcp_config.json (user
//     scope) and <repo>/.agents/mcp_config.json (project scope, also read
//     from the repo's subdirectories); with a name in both, the user's
//     wins. `agy mcp add/list` only see the user file, the agent both;
//   - a local server is command/args/env; a remote one is "url" or the
//     older "serverUrl" (what `agy mcp add` writes) with static "headers";
//   - there's no headers helper, so a remote server whose headers come from
//     a command can't be registered there;
//   - `agy mcp add` replaces a server's whole entry, dropping keys it doesn't
//     set (timeoutSeconds, enabledTools), so toolshed edits the file itself,
//     only its own keys (see jsonedit.go).
type antigravityClient struct{}

const Antigravity = "antigravity"

func (antigravityClient) Name() string          { return Antigravity }
func (antigravityClient) Label() string         { return "Antigravity" }
func (antigravityClient) Available() bool       { return onPath("agy") }
func (antigravityClient) Notes(string) []string { return nil }

// Scope: user and project. Claude's private per-project "local" has no
// counterpart, and isn't made global or shared instead.
func (antigravityClient) Scope(requested string) string {
	if requested == "user" || requested == "project" {
		return requested
	}
	return ""
}

func (antigravityClient) Unsupported(cfg Config) string {
	if cfg.HeadersHelper != "" {
		return "it can't get a server's headers from a command"
	}
	return ""
}

// AntigravityConfig is Antigravity's MCP config file for your user.
func AntigravityConfig() string {
	return filepath.Join(homeDir(), ".gemini", "config", "mcp_config.json")
}

// antigravityConfigPath is the config file for a scope.
func antigravityConfigPath(scope string) (string, error) {
	if scope != "project" {
		return AntigravityConfig(), nil
	}
	root := GitRoot()
	if root == "" {
		return "", fmt.Errorf("project scope needs a git repository")
	}
	return filepath.Join(root, ".agents", "mcp_config.json"), nil
}

const antigravitySection = "mcpServers"

// antigravityServer is how Antigravity stores one server.
type antigravityServer struct {
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	URL       string            `json:"url"`
	ServerURL string            `json:"serverUrl"`
	Headers   map[string]string `json:"headers"`
}

func (s antigravityServer) config() Config {
	if url := s.URL + s.ServerURL; url != "" {
		if s.URL != "" {
			url = s.URL // the current key wins over the legacy one
		}
		return Config{Type: "http", URL: url, Headers: s.Headers}
	}
	return Config{Type: "stdio", Command: s.Command, Args: s.Args, Env: s.Env}
}

// detect reads the project's file first: the user's wins (see above).
func (antigravityClient) detect(root string) []found {
	var out []found
	read := func(path, scope string) {
		var f struct {
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
		}
		if readJSONC(path, &f) != nil {
			return
		}
		for name, raw := range f.MCPServers {
			var s antigravityServer
			if json.Unmarshal(raw, &s) != nil {
				continue // one odd entry doesn't hide the others
			}
			out = append(out, found{name, Registration{Scope: scope, Key: name, Config: s.config()}})
		}
	}
	if root != "" {
		read(filepath.Join(root, ".agents", "mcp_config.json"), "project")
	}
	read(AntigravityConfig(), "user")
	return out
}

// antigravityKeys are the keys toolshed writes; any other key in an entry is
// the user's, and is kept (and carried along when the server moves scope).
var antigravityKeys = []string{"command", "args", "env", "url", "serverUrl", "headers"}

func (antigravityClient) register(name string, cfg Config, scope string, prev *Registration) error {
	path, err := antigravityConfigPath(scope)
	if err != nil {
		return err
	}
	carried, err := antigravityCarry(name, path, prev)
	if err != nil {
		return err
	}
	var set []jsonKV
	var drop []string
	if cfg.URL != "" {
		key, other := "serverUrl", "url"
		var f struct {
			MCPServers map[string]map[string]any `json:"mcpServers"`
		}
		if readJSONC(path, &f) == nil {
			if _, ok := f.MCPServers[name]["url"]; ok {
				key, other = "url", "serverUrl" // keep the key the entry already uses
			}
		}
		set = []jsonKV{{key, cfg.URL}}
		drop = []string{other, "command", "args", "env"}
		if len(cfg.Headers) > 0 {
			set = append(set, jsonKV{"headers", cfg.Headers})
		} // else: keep the user's own static headers
	} else {
		set = []jsonKV{{"command", cfg.Command}}
		drop = []string{"url", "serverUrl", "headers"}
		if len(cfg.Args) > 0 {
			set = append(set, jsonKV{"args", cfg.Args})
		} else {
			drop = append(drop, "args")
		}
		if len(cfg.Env) > 0 {
			set = append(set, jsonKV{"env", cfg.Env})
		} else {
			drop = append(drop, "env")
		}
	}
	return writeJSONServer(path, antigravitySection, name, append(set, carried...), drop)
}

// antigravityCarry returns the user's own keys of a server that's moving here
// from another scope (prev), unless this file has an entry for it already.
func antigravityCarry(name, path string, prev *Registration) ([]jsonKV, error) {
	if prev == nil {
		return nil, nil
	}
	from, err := antigravityConfigPath(prev.Scope)
	if err != nil || from == path {
		return nil, err
	}
	var here, there struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := readJSONC(path, &here); err != nil {
		return nil, err
	}
	if _, ok := here.MCPServers[name]; ok {
		return nil, nil
	}
	if err := readJSONC(from, &there); err != nil {
		return nil, err
	}
	old := there.MCPServers[prev.Key]
	keys := make([]string, 0, len(old))
	for k := range old {
		if !contains(antigravityKeys, k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []jsonKV
	for _, k := range keys {
		out = append(out, jsonKV{k, old[k]})
	}
	return out, nil
}

func (antigravityClient) unregister(name, scope string, _ *Registration) error {
	path, err := antigravityConfigPath(scope)
	if err != nil {
		return err
	}
	return writeJSONServer(path, antigravitySection, name, nil, nil)
}
