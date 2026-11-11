package clients

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// claudeAt writes name into Claude's config at scope directly, the way a
// user (or an older toolshed) could have left it, with extra keys.
func claudeAt(t *testing.T, home, repo, scope, name, cmd string, extra map[string]any) {
	t.Helper()
	srv := map[string]any{"type": "stdio", "command": cmd}
	for k, v := range extra {
		srv[k] = v
	}
	path, sel := filepath.Join(home, ".claude.json"), []string{"mcpServers"}
	switch scope {
	case "project":
		path = filepath.Join(repo, ".mcp.json")
	case "local":
		root := GitRoot()
		sel = []string{"projects", root, "mcpServers"}
	}
	doc := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &doc)
	}
	m := doc
	for _, k := range sel {
		next, _ := m[k].(map[string]any)
		if next == nil {
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	m[name] = srv
	data, _ := json.Marshal(doc)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func scopesOf(inst *Installation, client string) (used string, hidden []string) {
	for _, r := range inst.Hidden[client] {
		hidden = append(hidden, r.Scope)
	}
	return inst.Scope(client), hidden
}

// Detect reports the copy each client actually uses when a name is at
// several scopes (checked against the real CLIs), and keeps the others.
func TestDetectPrecedence(t *testing.T) {
	t.Run("claude: local, then user, then project", func(t *testing.T) {
		home, repo := moveSandbox(t)
		claudeAt(t, home, repo, "project", "dup", "p", nil)
		claudeAt(t, home, repo, "user", "dup", "u", nil)
		if used, hidden := scopesOf(Detect()["dup"], Claude); used != "user" || fmt.Sprint(hidden) != "[project]" {
			t.Fatalf("user + project: uses %s, hides %v", used, hidden)
		}
		claudeAt(t, home, repo, "local", "dup", "l", nil)
		inst := Detect()["dup"]
		if used, hidden := scopesOf(inst, Claude); used != "local" || fmt.Sprint(hidden) != "[project user]" {
			t.Fatalf("all three: uses %s, hides %v", used, hidden)
		}
		if inst.Config().Command != "l" {
			t.Fatalf("config should be the local one: %+v", inst.Config())
		}
	})
	t.Run("codex: project once trusted, else user", func(t *testing.T) {
		home, repo := moveSandbox(t)
		user := filepath.Join(home, ".codex", "config.toml")
		_ = os.MkdirAll(filepath.Dir(user), 0o755)
		_ = os.MkdirAll(filepath.Join(repo, ".codex"), 0o755)
		_ = os.WriteFile(filepath.Join(repo, ".codex", "config.toml"), []byte("[mcp_servers.dup]\ncommand = \"p\"\n"), 0o600)
		_ = os.WriteFile(user, []byte("[mcp_servers.dup]\ncommand = \"u\"\n"), 0o600)
		if used, hidden := scopesOf(Detect()["dup"], Codex); used != "user" || fmt.Sprint(hidden) != "[project]" {
			t.Fatalf("untrusted: uses %s, hides %v", used, hidden)
		}
		real, _ := filepath.EvalSymlinks(repo)
		f, _ := os.OpenFile(user, os.O_APPEND|os.O_WRONLY, 0)
		fmt.Fprintf(f, "\n[projects.%q]\ntrust_level = \"trusted\"\n", real)
		_ = f.Close()
		if used, hidden := scopesOf(Detect()["dup"], Codex); used != "project" || fmt.Sprint(hidden) != "[user]" {
			t.Fatalf("trusted: uses %s, hides %v", used, hidden)
		}
	})
}

// Installing at the scope of a hidden copy updates that copy (keeping its own
// settings) and moves the tool there: the copy the client used is removed.
func TestRegisterAtHiddenScope(t *testing.T) {
	home, repo := moveSandbox(t)
	claudeAt(t, home, repo, "project", "dup", "p", map[string]any{"timeout": 7})
	claudeAt(t, home, repo, "user", "dup", "u", nil)
	if err := Register(Claude, "dup", Config{Type: "stdio", Command: "new"}, "project"); err != nil {
		t.Fatal(err)
	}
	inst := Detect()["dup"]
	if used, hidden := scopesOf(inst, Claude); used != "project" || len(hidden) != 0 {
		t.Fatalf("uses %s, hides %v", used, hidden)
	}
	data, _ := os.ReadFile(filepath.Join(repo, ".mcp.json"))
	var mcp struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	_ = json.Unmarshal(data, &mcp)
	if d := mcp.MCPServers["dup"]; d["command"] != "new" || d["timeout"] != float64(7) {
		t.Fatalf("the project copy should be updated in place: %s", data)
	}
}

// Unregister at a scope removes the copy there, even one the client hides,
// and leaves the one it uses.
func TestUnregisterHiddenCopy(t *testing.T) {
	home, repo := moveSandbox(t)
	claudeAt(t, home, repo, "project", "dup", "p", nil)
	claudeAt(t, home, repo, "user", "dup", "u", nil)
	if err := Unregister(Claude, "dup", "project"); err != nil {
		t.Fatal(err)
	}
	inst := Detect()["dup"]
	if used, hidden := scopesOf(inst, Claude); used != "user" || len(hidden) != 0 || inst.Config().Command != "u" {
		t.Fatalf("uses %s, hides %v", used, hidden)
	}
}
