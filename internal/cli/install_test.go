package cli

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/clients/clienttest"
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
	if inst == nil || inst.Scope(clients.Claude) != "local" {
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

// An installed tool is installed again where it is; --user and --project
// move it, so it's only at the new scope afterwards.
func TestInstallScopeFlags(t *testing.T) {
	home := sandbox(t)
	repo := filepath.Join(home, "repo") // not HOME: ~/.codex would be the repo's .codex
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	t.Chdir(repo)
	if err := registry.AddUserEntry(registry.Entry{ID: "hi", Command: "sh"}); err != nil {
		t.Fatal(err)
	}
	at := func(scope string) map[string]string {
		return map[string]string{clients.Claude: scope, clients.Codex: scope}
	}
	steps := []struct {
		args []string
		want map[string]string
		note string
	}{
		{[]string{"hi", "-y", "--project"}, at("project"), ""},
		{[]string{"hi", "-y"}, at("project"), ""}, // stays
		{[]string{"hi", "-y", "--user"}, at("user"), "moved from project to user scope"},
		{[]string{"hi", "-y"}, at("user"), ""},
		{[]string{"hi", "-y", "--project"}, at("project"), "moved from user to project scope"},
	}
	for _, st := range steps {
		out, err := captureStdout(t, func() error { return runInstall(st.args) })
		if err != nil {
			t.Fatalf("install %v: %v", st.args, err)
		}
		if got := clienttest.Placements(t, "hi", repo); !maps.Equal(got, st.want) {
			t.Fatalf("install %v: registered at %v, want %v", st.args, got, st.want)
		}
		if st.note != "" && !strings.Contains(out, st.note) {
			t.Fatalf("install %v should say it moved:\n%s", st.args, out)
		}
	}
	if err := runInstall([]string{"hi", "--user", "--project"}); err == nil {
		t.Fatal("--user with --project should be refused")
	}
}
