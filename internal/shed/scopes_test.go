package shed

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/clients/clienttest"
	"github.com/bartdegoede/toolshed/internal/registry"
)

// Each client gets the nearest scope it has: Codex has no "local", so a
// Claude-local install is a user install there.
func TestScopeForEachClient(t *testing.T) {
	cases := []struct{ scope, claude, codex string }{
		{"user", "user", "user"},
		{"local", "local", "user"},
		{"project", "project", "project"},
	}
	for _, c := range cases {
		opts := InstallOptions{Scope: c.scope}
		if got := opts.scopeFor(clients.Claude); got != c.claude {
			t.Errorf("%s: claude got %s, want %s", c.scope, got, c.claude)
		}
		if got := opts.scopeFor(clients.Codex); got != c.codex {
			t.Errorf("%s: codex got %s, want %s", c.scope, got, c.codex)
		}
	}
}

// Re-installing keeps every client at the scope it's at now, and reports the
// first client's scope.
func TestKeepScopes(t *testing.T) {
	inst := &clients.Installation{Regs: map[string]clients.Registration{
		clients.Claude: {Scope: "project"},
		clients.Codex:  {Scope: "user"},
	}}
	opts := KeepScopes(inst)
	if opts.Scope != "project" || opts.scopeFor(clients.Claude) != "project" || opts.scopeFor(clients.Codex) != "user" {
		t.Fatalf("scopes not kept: %+v", opts)
	}
	codexOnly := &clients.Installation{Regs: map[string]clients.Registration{clients.Codex: {Scope: "project"}}}
	if opts := KeepScopes(codexOnly); opts.scopeFor(clients.Codex) != "project" || len(opts.Clients) != 1 {
		t.Fatalf("codex-only install not kept: %+v", opts)
	}
}

// Claude Code's private per-project "local" scope has no counterpart in
// Antigravity: an install there skips it (with a note) instead of making the
// tool global or shared, and takes it out of Antigravity if it was there.
func TestLocalScopeSkipsAntigravity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("TOOLSHED_HOME", filepath.Join(home, ".toolshed"))
	mocks, _ := filepath.Abs("../../test/mocks")
	t.Setenv("PATH", mocks+string(os.PathListSeparator)+os.Getenv("PATH"))
	repo := filepath.Join(home, "repo")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	t.Chdir(repo)
	e := &registry.Entry{ID: "hi", Command: "sh"}
	if _, err := Install(e, Values{}, InstallOptions{Scope: "user"}); err != nil {
		t.Fatal(err)
	}
	res, err := Install(e, Values{}, InstallOptions{Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(res.Notes, "Antigravity has no local scope, so hi was removed from it") {
		t.Fatalf("notes: %v", res.Notes)
	}
	want := map[string]string{clients.Claude: "local", clients.Codex: "user"} // Codex has no local scope either, but "user" is its nearest
	if got := clienttest.Placements(t, "hi", repo); !maps.Equal(got, want) {
		t.Fatalf("registered at %v, want %v", got, want)
	}
}
