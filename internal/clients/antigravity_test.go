package clients

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func agSandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(home)
	return filepath.Join(home, ".gemini", "config", "mcp_config.json")
}

func agServers(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	var f struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("not plain JSON: %v\n%s", err, data)
	}
	return f.MCPServers
}

// What `agy mcp add` wrote, plus settings the user added in Antigravity:
// toolshed updates its own keys and keeps the rest.
func TestAntigravityRoundTrip(t *testing.T) {
	path := agSandbox(t)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(`{
  "mcpServers": {
    "notes": {
      "args": [
        "-c",
        "exec cat"
      ],
      "command": "sh",
      "disabled": false,
      "enabledTools": [
        "read"
      ],
      "timeoutSeconds": 30
    },
    "theirs": {
      "serverUrl": "https://theirs.example/mcp"
    }
  },
  "someOtherTopLevel": true
}
`), 0o644)

	inst := Detect()["notes"]
	if inst == nil || inst.Scope(Antigravity) != "user" || inst.Regs[Antigravity].Config.Command != "sh" {
		t.Fatalf("detect: %+v", inst)
	}
	if r := Detect()["theirs"].Regs[Antigravity].Config; r.Type != "http" || r.URL != "https://theirs.example/mcp" {
		t.Fatalf("serverUrl not read: %+v", r)
	}

	if err := Register(Antigravity, "notes", Config{Type: "stdio", Command: "sh", Args: []string{"-c", "exec tac"}, Env: map[string]string{"LEVEL": "info"}}, "user"); err != nil {
		t.Fatal(err)
	}
	got := agServers(t, path)["notes"]
	want := map[string]any{"command": "sh", "args": []any{"-c", "exec tac"}, "env": map[string]any{"LEVEL": "info"},
		"disabled": false, "enabledTools": []any{"read"}, "timeoutSeconds": float64(30)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"someOtherTopLevel": true`) {
		t.Fatalf("other settings lost:\n%s", data)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode changed: %v", fi.Mode().Perm())
	}

	if err := Unregister(Antigravity, "notes", "user"); err != nil {
		t.Fatal(err)
	}
	servers := agServers(t, path)
	if _, ok := servers["notes"]; ok || servers["theirs"] == nil {
		t.Fatalf("unregister: %v", servers)
	}
}

// A remote server keeps the URL key its entry uses; a new one gets
// serverUrl, which every agy version reads.
func TestAntigravityRemoteKeys(t *testing.T) {
	path := agSandbox(t)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(`{"mcpServers": {"a": {"url": "https://old.example/mcp", "headers": {"X-Team": "core"}}}}`), 0o600)
	if err := Register(Antigravity, "a", Config{Type: "http", URL: "https://new.example/mcp"}, "user"); err != nil {
		t.Fatal(err)
	}
	if a := agServers(t, path)["a"]; a["url"] != "https://new.example/mcp" || a["serverUrl"] != nil || a["headers"] == nil {
		t.Fatalf("should update url and keep the user's headers: %v", a)
	}
	if err := Register(Antigravity, "b", Config{Type: "http", URL: "https://b.example/mcp", Headers: map[string]string{"Authorization": "Bearer t"}}, "user"); err != nil {
		t.Fatal(err)
	}
	if b := agServers(t, path)["b"]; b["serverUrl"] != "https://b.example/mcp" || b["url"] != nil {
		t.Fatalf("new remote server: %v", b)
	}
}

// Project scope is the repo's .agents/mcp_config.json; with a name in both
// files the user's wins (checked in agy 1.5.5's /mcp). Claude Code's
// private "local" scope has no counterpart and is refused, not made global.
func TestAntigravityScopes(t *testing.T) {
	_, repo := moveSandbox(t)
	user := AntigravityConfig()
	project := filepath.Join(repo, ".agents", "mcp_config.json")
	if err := Register(Antigravity, "a", Config{Type: "stdio", Command: "p"}, "project"); err != nil {
		t.Fatal(err)
	}
	if agServers(t, project)["a"]["command"] != "p" {
		t.Fatal("not in the project's config")
	}
	if _, err := os.Stat(user); err == nil {
		t.Fatal("the user's config shouldn't be touched")
	}
	_ = os.MkdirAll(filepath.Dir(user), 0o755)
	_ = os.WriteFile(user, []byte(`{"mcpServers": {"a": {"command": "u"}}}`), 0o600)
	inst := Detect()["a"]
	if used, hidden := scopesOf(inst, Antigravity); used != "user" || len(hidden) != 1 || hidden[0] != "project" || inst.Config().Command != "u" {
		t.Fatalf("uses %s, hides %v", used, hidden)
	}
	if err := Register(Antigravity, "b", Config{Type: "stdio", Command: "x"}, "local"); err == nil {
		t.Fatal("local scope should be refused")
	}
	if (antigravityClient{}).Unsupported(Config{Type: "http", URL: "https://x", HeadersHelper: "gh auth token"}) == "" {
		t.Fatal("a headers helper can't be registered in Antigravity")
	}
}

// Moving a server to the other scope takes the user's own keys along, as for
// Claude Code and Codex, and leaves nothing behind.
func TestAntigravityMoveCarriesSettings(t *testing.T) {
	_, repo := moveSandbox(t)
	user := AntigravityConfig()
	_ = os.MkdirAll(filepath.Dir(user), 0o755)
	_ = os.WriteFile(user, []byte(`{"mcpServers": {"a": {"command": "x", "timeoutSeconds": 30, "enabledTools": ["read"]}}}`), 0o600)
	if err := Register(Antigravity, "a", Config{Type: "stdio", Command: "y"}, "project"); err != nil {
		t.Fatal(err)
	}
	got := agServers(t, filepath.Join(repo, ".agents", "mcp_config.json"))["a"]
	want := map[string]any{"command": "y", "timeoutSeconds": float64(30), "enabledTools": []any{"read"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if _, ok := agServers(t, user)["a"]; ok {
		t.Fatal("still at user scope")
	}
}
