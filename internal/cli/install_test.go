package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
)

// sandbox gives a test an isolated HOME, toolshed home and the mock
// claude/codex CLIs, like session() does for the MCP tests.
func sandbox(t *testing.T) string {
	t.Helper()
	session(t)
	return os.Getenv("HOME")
}

// A Claude "local" install (claude mcp add -s local) must stay local on
// update, not gain a second, global registration.
func TestUpdateKeepsLocalScope(t *testing.T) {
	home := sandbox(t)
	if out, err := exec.Command("git", "init", "-q", home).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := registry.AddUserEntry(registry.Entry{ID: "hello", Command: "sh"}); err != nil {
		t.Fatal(err)
	}
	if err := clients.Register(clients.Claude, "hello", clients.Config{Type: "stdio", Command: "sh"}, "local"); err != nil {
		t.Fatal(err)
	}
	if err := runUpdate([]string{"hello"}); err != nil {
		t.Fatal(err)
	}
	inst := clients.Detect()["hello"]
	if inst == nil || inst.ClaudeScope != "local" {
		t.Fatalf("expected hello to stay local, got %+v", inst)
	}
	data, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	if strings.Contains(string(data), `"mcpServers":{"hello"`) || strings.Count(string(data), `"hello"`) != 1 {
		t.Fatalf("update added a user-scope copy:\n%s", data)
	}
}

func TestAddRefusesToReplaceWithoutForce(t *testing.T) {
	sandbox(t)
	if err := runAdd([]string{"mine", "--no-check", "--", "sh"}); err != nil {
		t.Fatal(err)
	}
	err := runAdd([]string{"mine", "--no-check", "--", "true"})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected a refusal mentioning --force, got %v", err)
	}
	if err := runAdd([]string{"mine", "--no-check", "--force", "--", "true"}); err != nil {
		t.Fatal(err)
	}
	reg, _ := registry.Load()
	if got := reg.Get("mine").Command; got != "true" {
		t.Fatalf("--force should replace, command = %q", got)
	}
}

func TestApproveCommand(t *testing.T) {
	sandbox(t)
	_ = registry.AddUserEntry(registry.Entry{ID: "agentic", Command: "sh", Pending: true})
	if err := runApprove([]string{"agentic"}); err == nil {
		t.Fatal("approve without a terminal and without -y should refuse")
	}
	if err := runApprove([]string{"agentic", "-y"}); err != nil {
		t.Fatal(err)
	}
	reg, _ := registry.Load()
	if reg.Get("agentic").Pending {
		t.Fatal("still pending after approve -y")
	}
}
