package cli

import (
	"maps"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/clients/clienttest"
	"github.com/bartdegoede/toolshed/internal/registry"
)

// A second copy of a tool in one client (what an older toolshed left when
// reinstalling at another scope): doctor names it and the command to remove
// it, and that command removes exactly that copy.
func TestDoctorFindsSecondCopy(t *testing.T) {
	home := sandbox(t)
	repo := filepath.Join(home, "repo") // not HOME: ~/.codex would be the repo's .codex
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	t.Chdir(repo)
	if err := registry.AddUserEntry(registry.Entry{ID: "hi", Command: "sh"}); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"project", "user"} { // what the old reinstall did
		if out, err := exec.Command("claude", "mcp", "add-json", "hi", `{"type":"stdio","command":"sh"}`, "-s", scope).CombinedOutput(); err != nil {
			t.Fatalf("claude mcp add-json: %v %s", err, out)
		}
	}

	out, err := captureStdout(t, func() error { return runDoctor([]string{"--no-start"}) })
	if err != nil {
		t.Fatalf("a second copy is a warning, not a failure: %v\n%s", err, out)
	}
	fix := "toolshed uninstall hi --client claude --scope project"
	if !strings.Contains(out, "Claude Code also has it at project scope, but uses the user one here") || !strings.Contains(out, fix) {
		t.Fatalf("doctor should point out the second copy and how to remove it:\n%s", out)
	}
	if strings.Contains(out, "running something else") {
		t.Fatalf("both copies run the same thing:\n%s", out)
	}

	if err := runUninstall(strings.Fields(fix)[2:]); err != nil {
		t.Fatal(err)
	}
	if got, want := clienttest.Placements(t, "hi", repo), map[string]string{clients.Claude: "user"}; !maps.Equal(got, want) {
		t.Fatalf("after the fix: %v, want %v", got, want)
	}
	if out, _ := captureStdout(t, func() error { return runDoctor([]string{"--no-start"}) }); strings.Contains(out, "also has it") {
		t.Fatalf("doctor still reports a second copy:\n%s", out)
	}

	if err := runUninstall([]string{"hi", "--scope", "user", "--purge"}); err == nil {
		t.Fatal("--purge with --scope should be refused")
	}
	if err := runUninstall([]string{"hi", "--client", "codex"}); err == nil || !strings.Contains(err.Error(), "isn't registered in Codex") {
		t.Fatalf("removing a copy that isn't there should say so, got %v", err)
	}
}

// When the copies run different things, doctor says so: the hidden one isn't
// just a stale duplicate.
func TestDoctorSecondCopyRunsSomethingElse(t *testing.T) {
	home := sandbox(t)
	repo := filepath.Join(home, "repo")
	_ = exec.Command("git", "init", "-q", repo).Run()
	t.Chdir(repo)
	_ = registry.AddUserEntry(registry.Entry{ID: "hi", Command: "sh"})
	_ = exec.Command("claude", "mcp", "add-json", "hi", `{"type":"stdio","command":"other"}`, "-s", "project").Run()
	_ = exec.Command("claude", "mcp", "add-json", "hi", `{"type":"stdio","command":"sh"}`, "-s", "user").Run()
	out, _ := captureStdout(t, func() error { return runDoctor([]string{"--no-start"}) })
	if !strings.Contains(out, "Claude Code also has it at project scope (running something else), but uses the user one here") {
		t.Fatalf("doctor should say the copies differ:\n%s", out)
	}
}
