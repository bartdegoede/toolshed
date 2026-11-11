package shed

import (
	"strings"
	"testing"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/secrets"
)

func TestChanges(t *testing.T) {
	secrets.UseMockForTest(true)
	e := &registry.Entry{ID: "jira", Command: "jira-mcp", Args: []string{"--site", "acme"},
		Env: map[string]string{"MODE": "ro"}, Inputs: []registry.Input{{Key: "TOKEN", Secret: true}}}
	wrapped := clients.Config{Type: "stdio", Command: "/opt/homebrew/opt/toolshed/bin/toolshed",
		Args: []string{"exec", "jira", "--secret", "TOKEN=jira", "--", "jira-mcp", "--site", "acme"},
		Env:  map[string]string{"MODE": "ro", "REGION": "eu"}}
	claude := inClaude

	if got := Changes(e, claude(wrapped)); len(got) != 0 {
		t.Fatalf("same entry should have no changes, got %v", got)
	}
	moved := wrapped
	moved.Command = "/Users/me/go/bin/toolshed" // toolshed's own path doesn't count
	if got := Changes(e, claude(moved)); len(got) != 0 {
		t.Fatalf("a moved toolshed binary isn't a change, got %v", got)
	}
	if got := Changes(e, nil); got != nil {
		t.Fatalf("not installed: %v", got)
	}

	cases := []struct {
		name string
		edit func(*registry.Entry)
		want string
	}{
		{"command", func(x *registry.Entry) { x.Command = "evil" }, "runs evil --site acme (was: jira-mcp --site acme)"},
		{"args", func(x *registry.Entry) { x.Args = []string{"--site", "other"} }, "runs jira-mcp --site other"},
		{"env value", func(x *registry.Entry) { x.Env = map[string]string{"MODE": "rw"} }, "sets MODE=rw (was: ro)"},
		{"new env", func(x *registry.Entry) { x.Env = map[string]string{"MODE": "ro", "NODE_OPTIONS": "--require x"} }, "sets NODE_OPTIONS=--require x (wasn't set)"},
		{"secret group", func(x *registry.Entry) { x.Inputs[0].Share = "github" }, "gets TOKEN from github's keyring entry (was: jira's)"},
		{"new secret", func(x *registry.Entry) { x.Inputs = append(x.Inputs, registry.Input{Key: "OTHER", Secret: true}) }, "gets the secret OTHER"},
		{"now remote", func(x *registry.Entry) { *x = registry.Entry{ID: "jira", URL: "https://x.example/mcp"} }, "connects to https://x.example/mcp (was: jira-mcp --site acme)"},
	}
	for _, c := range cases {
		x := *e
		x.Inputs = append([]registry.Input(nil), e.Inputs...)
		c.edit(&x)
		got := strings.Join(Changes(&x, claude(wrapped)), "\n")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: want %q in:\n%s", c.name, c.want, got)
		}
	}
}

func TestChangesComparesEveryClientOnce(t *testing.T) {
	e := &registry.Entry{ID: "x", Command: "new"}
	old := clients.Config{Type: "stdio", Command: "old"}
	got := Changes(e, inBoth(old, old))
	if len(got) != 1 {
		t.Fatalf("same change in both clients should be listed once, got %v", got)
	}
	// Only Codex out of date.
	got = Changes(e, inBoth(clients.Config{Command: "new"}, old))
	if len(got) != 1 {
		t.Fatalf("expected Codex's change, got %v", got)
	}
}

func TestRemoteChanges(t *testing.T) {
	e := &registry.Entry{ID: "gh", URL: "https://a.example/mcp", HeaderCmd: "gh-token"}
	inst := inClaude(clients.Config{Type: "http", URL: "https://a.example/mcp", HeadersHelper: "gh-token"})
	if got := Changes(e, inst); len(got) != 0 {
		t.Fatalf("no change expected, got %v", got)
	}
	e2 := *e
	e2.HeaderCmd = "curl evil"
	if got := strings.Join(Changes(&e2, inst), "\n"); !strings.Contains(got, "curl evil") {
		t.Fatalf("headers helper change not reported: %s", got)
	}
}
