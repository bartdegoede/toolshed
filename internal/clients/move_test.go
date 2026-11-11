package clients

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// moveSandbox is a HOME with the mock claude CLI and a git repo below it
// (not HOME itself: then ~/.codex/config.toml would be the repo's
// .codex/config.toml), with the repo as the working directory.
func moveSandbox(t *testing.T) (home, repo string) {
	t.Helper()
	home = useMockClaude(t)
	repo = filepath.Join(home, "repo")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	t.Chdir(repo)
	return home, repo
}

func readTOML(t *testing.T, path string) map[string]any {
	t.Helper()
	var v map[string]any
	if _, err := toml.DecodeFile(path, &v); err != nil && !os.IsNotExist(err) {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

func serverIn(doc map[string]any, name string) map[string]any {
	servers, _ := doc["mcp_servers"].(map[string]any)
	s, _ := servers[name].(map[string]any)
	return s
}

// Moving a Codex server to another scope takes its settings along (keys
// toolshed doesn't manage, subtables, an old underscore name) and leaves
// nothing behind in the old file but what belongs to other servers.
func TestCodexMoveCarriesSettings(t *testing.T) {
	home, repo := moveSandbox(t)
	user := filepath.Join(home, ".codex", "config.toml")
	_ = os.MkdirAll(filepath.Dir(user), 0o755)
	_ = os.WriteFile(user, []byte(`model = "o3"

[mcp_servers.my_tool]
command = "old"
startup_timeout_sec = 30 # mine

[mcp_servers.my_tool.tools.search]
approval = "never"

[mcp_servers.other]
command = "x"
`), 0o600)

	if err := Register(Codex, "my-tool", Config{Type: "stdio", Command: "new"}, "project"); err != nil {
		t.Fatal(err)
	}
	moved := serverIn(readTOML(t, filepath.Join(repo, ".codex", "config.toml")), "my-tool")
	tools, _ := moved["tools"].(map[string]any)
	search, _ := tools["search"].(map[string]any)
	if moved["command"] != "new" || moved["startup_timeout_sec"] != int64(30) || search["approval"] != "never" {
		t.Fatalf("settings didn't move with it: %v", moved)
	}
	left := readTOML(t, user)
	if serverIn(left, "my_tool") != nil || serverIn(left, "my-tool") != nil {
		t.Fatalf("old registration still there: %v", left)
	}
	if left["model"] != "o3" || serverIn(left, "other")["command"] != "x" {
		t.Fatalf("the rest of the old file changed: %v", left)
	}
	if inst := Detect()["my-tool"]; inst == nil || inst.Scope(Codex) != "project" {
		t.Fatalf("detect: %+v", inst)
	}

	// And back.
	if err := Register(Codex, "my-tool", Config{Type: "stdio", Command: "new"}, "user"); err != nil {
		t.Fatal(err)
	}
	if back := serverIn(readTOML(t, user), "my-tool"); back["startup_timeout_sec"] != int64(30) {
		t.Fatalf("settings lost moving back: %v", back)
	}
	if s := serverIn(readTOML(t, filepath.Join(repo, ".codex", "config.toml")), "my-tool"); s != nil {
		t.Fatalf("still in the project file: %v", s)
	}
}

// A layout toolshed won't edit can't be moved either: nothing changes.
func TestCodexMoveRefusesUnsupportedLayout(t *testing.T) {
	home, repo := moveSandbox(t)
	user := filepath.Join(home, ".codex", "config.toml")
	_ = os.MkdirAll(filepath.Dir(user), 0o755)
	before := "mcp_servers.inline = { command = \"a\" }\n"
	_ = os.WriteFile(user, []byte(before), 0o600)
	if err := Register(Codex, "inline", Config{Type: "stdio", Command: "a"}, "project"); err == nil {
		t.Fatal("expected the move to be refused")
	}
	if data, _ := os.ReadFile(user); string(data) != before {
		t.Fatalf("old file changed:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(repo, ".codex", "config.toml")); err == nil {
		t.Fatal("project file written")
	}
}

// Claude: a key toolshed doesn't model moves with the server, and the old
// scope's registration is removed.
func TestClaudeMoveCarriesSettings(t *testing.T) {
	home, repo := moveSandbox(t)
	_ = os.WriteFile(filepath.Join(home, ".claude.json"),
		[]byte(`{"mcpServers":{"demo":{"type":"stdio","command":"old","timeout":5000}}}`), 0o600)
	if err := Register(Claude, "demo", Config{Type: "stdio", Command: "new"}, "project"); err != nil {
		t.Fatal(err)
	}
	var mcp struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	data, _ := os.ReadFile(filepath.Join(repo, ".mcp.json"))
	_ = json.Unmarshal(data, &mcp)
	if d := mcp.MCPServers["demo"]; d["command"] != "new" || d["timeout"] != float64(5000) {
		t.Fatalf(".mcp.json: %s", data)
	}
	if user, _ := os.ReadFile(filepath.Join(home, ".claude.json")); strings.Contains(string(user), `"demo"`) {
		t.Fatalf("still at user scope: %s", user)
	}
}
