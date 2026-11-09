package shed

import (
	"slices"
	"testing"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
)

func TestSecretName(t *testing.T) {
	for _, k := range []string{"GITHUB_TOKEN", "OPENAI_API_KEY", "DB_PASSWORD", "CLIENT_SECRET", "AWS_ACCESS_KEY_ID", "SSH_KEY", "GH_PAT", "AUTH_HEADER"} {
		if !SecretName(k) {
			t.Errorf("%s should look like a secret", k)
		}
	}
	for _, k := range []string{"LOG_LEVEL", "REGION", "JIRA_SITE", "KEYBOARD_LAYOUT", "PORT"} {
		if SecretName(k) {
			t.Errorf("%s shouldn't look like a secret", k)
		}
	}
}

func TestSecretValue(t *testing.T) {
	for v, want := range map[string]bool{
		"postgresql://app:hunter2@db.internal/app": true,
		"redis://:s3cret@cache:6379":               true,
		"https://user@example.com":                 false,
		"https://example.com/mcp":                  false,
		"debug":                                    false,
	} {
		if got := SecretValue(v); got != want {
			t.Errorf("SecretValue(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestImports(t *testing.T) {
	claude := func(cfg clients.Config) *clients.Installation {
		return &clients.Installation{ClaudeScope: "user", Config: cfg}
	}
	reg := &registry.Registry{Entries: []*registry.Entry{{ID: "managed", Command: "sh"}}}
	installed := map[string]*clients.Installation{
		"managed": claude(clients.Config{Command: "sh"}),
		"gh": claude(clients.Config{Type: "stdio", Command: "npx", Args: []string{"-y", "gh-mcp"},
			Env: map[string]string{"GITHUB_TOKEN": "ghp_123", "LOG_LEVEL": "debug", "EMPTY_TOKEN": ""}}),
		"Docs_Server": claude(clients.Config{Type: "http", URL: "https://docs.example/mcp"}),
		"legacy":      claude(clients.Config{Type: "sse", URL: "https://old.example/sse"}),
		"headers":     claude(clients.Config{Type: "http", URL: "https://x.example/mcp", Headers: map[string]string{"Authorization": "Bearer x"}}),
		"orphan": claude(clients.Config{Type: "stdio", Command: "/opt/toolshed",
			Args: []string{"exec", "orphan", "--secret", "TOKEN=github", "--", "srv", "--flag"}}),
		"split": {ClaudeScope: "user", Config: clients.Config{Command: "a"}, Codex: true, CodexConfig: clients.Config{Command: "b"}},
	}
	got := map[string]Import{}
	for _, im := range Imports(reg, installed) {
		got[im.Name] = im
	}
	if _, ok := got["managed"]; ok {
		t.Error("tools already in the registry aren't imported")
	}

	gh := got["gh"]
	e := gh.Build()
	if e.Command != "npx" || !slices.Equal(e.Args, []string{"-y", "gh-mcp"}) {
		t.Errorf("gh command: %+v", e)
	}
	inputs := map[string]registry.Input{}
	for _, in := range e.Inputs {
		inputs[in.Key] = in
	}
	if in := inputs["GITHUB_TOKEN"]; !in.Secret || gh.Values["GITHUB_TOKEN"] != "ghp_123" {
		t.Errorf("GITHUB_TOKEN should become a secret input: %+v %v", e.Inputs, gh.Values)
	}
	if in, ok := inputs["LOG_LEVEL"]; !ok || in.Secret || gh.Values["LOG_LEVEL"] != "debug" {
		t.Errorf("LOG_LEVEL should be a plain input: %+v", e.Inputs)
	}
	if in := inputs["EMPTY_TOKEN"]; in.Secret || !in.Optional {
		t.Errorf("an empty var is an optional plain input: %+v", in)
	}
	if len(e.Env) != 0 {
		t.Errorf("no values go into the registry, got env %v", e.Env)
	}

	if d := got["Docs_Server"]; !d.Rename || d.Entry.ID != "docs-server" || d.Entry.URL != "https://docs.example/mcp" {
		t.Errorf("Docs_Server should be renamed to docs-server: %+v", d)
	}
	for _, n := range []string{"legacy"} {
		if got[n].Skip == "" {
			t.Errorf("%s should be skipped", n)
		}
	}

	orphan := got["orphan"]
	o := orphan.Build()
	if o.Command != "srv" || len(o.Inputs) != 1 || o.Inputs[0].Share != "github" {
		t.Errorf("orphan should unwrap and keep its keyring group: %+v", o)
	}
	if s := got["split"]; !slices.Equal(s.Clients, []string{clients.Claude}) || len(s.Notes) == 0 {
		t.Errorf("split: only Claude's should be imported, with a note: %+v", s)
	}
}
