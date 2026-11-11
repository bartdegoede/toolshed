package shed

import (
	"testing"

	"github.com/bartdegoede/toolshed/internal/clients"
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
