// Package clienttest helps tests check where clients have a server
// registered, straight from their config files.
package clienttest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bartdegoede/toolshed/internal/clients"
)

// Placements reads every scope's config file for name, so a second copy
// shows up as "user+project" instead of being hidden like Detect hides it.
// repo is the git repository the project and local scopes are for.
func Placements(t *testing.T, name, repo string) map[string]string {
	t.Helper()
	home := os.Getenv("HOME")
	found := map[string][]string{}
	var claude struct {
		MCPServers map[string]any `json:"mcpServers"`
		Projects   map[string]struct {
			MCPServers map[string]any `json:"mcpServers"`
		} `json:"projects"`
	}
	readJSON(t, filepath.Join(home, ".claude.json"), &claude)
	if _, ok := claude.MCPServers[name]; ok {
		found[clients.Claude] = append(found[clients.Claude], "user")
	}
	for _, p := range claude.Projects {
		if _, ok := p.MCPServers[name]; ok {
			found[clients.Claude] = append(found[clients.Claude], "local")
		}
	}
	var mcp struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	readJSON(t, filepath.Join(repo, ".mcp.json"), &mcp)
	if _, ok := mcp.MCPServers[name]; ok {
		found[clients.Claude] = append(found[clients.Claude], "project")
	}
	for scope, path := range map[string]string{"user": filepath.Join(clients.CodexHome(), "config.toml"), "project": filepath.Join(repo, ".codex", "config.toml")} {
		if data, _ := os.ReadFile(path); strings.Contains(string(data), "[mcp_servers."+name+"]") {
			found[clients.Codex] = append(found[clients.Codex], scope)
		}
	}
	out := map[string]string{}
	for c, scopes := range found {
		slices.Sort(scopes)
		out[c] = strings.Join(scopes, "+")
	}
	return out
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return
	}
	if err != nil || json.Unmarshal(data, v) != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
}
