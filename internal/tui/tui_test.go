package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/shed"
)

func TestSuggestID(t *testing.T) {
	cases := []struct{ kind, src, want string }{
		{kindNPX, "@modelcontextprotocol/server-memory", "memory"},
		{kindNPX, "@acme/notes-mcp@1.2.3 --flag", "notes"},
		{kindUVX, "mcp-server-fetch", "fetch"},
		{kindCommand, "/usr/local/bin/kubectl-mcp serve", "kubectl"},
		{kindCommand, "My_Tool", "my-tool"},
		{kindURL, "https://mcp.deepwiki.com/mcp", "deepwiki"},
		{kindURL, "https://api.githubcopilot.com/mcp/", "githubcopilot"},
		{kindURL, "not a url", ""},
		{kindNPX, "", ""},
	}
	for _, c := range cases {
		if got := registry.SuggestID(c.kind, c.src); got != c.want {
			t.Errorf("suggestID(%s, %q) = %q, want %q", c.kind, c.src, got, c.want)
		}
	}
}

func TestAddSpecToEntry(t *testing.T) {
	e, err := addSpec{Kind: kindNPX, Source: "@acme/server-notes --readonly", Secrets: "api_token, OTHER"}.toEntry()
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "notes" || e.Command != "npx" || strings.Join(e.Args, " ") != "-y @acme/server-notes --readonly" {
		t.Fatalf("npx entry wrong: %+v", e)
	}
	if len(e.Inputs) != 2 || e.Inputs[0].Key != "API_TOKEN" || !e.Inputs[0].Secret {
		t.Fatalf("secrets wrong: %+v", e.Inputs)
	}

	e, err = addSpec{Kind: kindUVX, Source: "mcp-server-time", ID: "clock"}.toEntry()
	if err != nil || e.ID != "clock" || e.Command != "uvx" || e.Args[0] != "mcp-server-time" {
		t.Fatalf("uvx entry wrong: %+v %v", e, err)
	}

	e, err = addSpec{Kind: kindCommand, Source: "gh-mcp serve --stdio"}.toEntry()
	if err != nil || e.Command != "gh-mcp" || strings.Join(e.Args, " ") != "serve --stdio" {
		t.Fatalf("command entry wrong: %+v %v", e, err)
	}

	e, err = addSpec{Kind: kindURL, Source: "https://mcp.deepwiki.com/mcp", Secrets: "IGNORED"}.toEntry()
	if err != nil || e.URL == "" || len(e.Inputs) != 0 {
		t.Fatalf("url entry wrong: %+v %v", e, err)
	}

	if _, err := (addSpec{Kind: kindURL, Source: "ftp://nope"}).toEntry(); err == nil {
		t.Fatal("invalid url should fail validation")
	}
	if _, err := (addSpec{Kind: kindNPX, Source: "  "}).toEntry(); err == nil {
		t.Fatal("empty source should fail")
	}
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;:]*[a-zA-Z]`)

// TestRender drives the model through a load and renders it, guarding the
// layout against panics and making it easy to eyeball with -v.
// fixtureRegistry loads a fixed registry so render tests don't depend on
// the shipped examples.
func fixtureRegistry(t *testing.T) (*registry.Registry, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.yaml")
	fixture := `examples: false
tools:
  - id: hello
    name: Hello
    description: A greeting server.
    command: hello-mcp
    inputs: [{key: HELLO_API_TOKEN, secret: true, optional: true}]
  - id: docs
    name: Docs
    description: Remote docs.
    url: https://docs.example.com/mcp
`
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOOLSHED_REGISTRY", path)
	return registry.Load()
}

func TestRender(t *testing.T) {
	reg, err := fixtureRegistry(t)
	if err != nil {
		t.Fatal(err)
	}
	inst := map[string]*clients.Installation{"hello": {Name: "hello", ClaudeScope: "user", Codex: true}}
	var m tea.Model = newModel()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m, _ = m.Update(loadedMsg{reg: reg, inst: inst, avail: []string{"claude", "codex"}})
	mm := m.(model)
	mm.probes["hello"] = &probeState{tools: []shed.Tool{{Name: "hello", Description: "Return a greeting"}, {Name: "whoami", Description: "Show config", Destructive: true}}}
	out := ansi.ReplaceAllString(mm.View().Content, "")
	t.Log("\n" + out)
	for _, want := range []string{"toolshed", "YOUR TOOLS", "Hello", "RUNS", "hello-mcp", "INSTALLED", "2 tools", "whoami"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q", want)
		}
	}
	col := -1
	for _, line := range strings.Split(out, "\n")[3:30] {
		if c := strings.Index(line, "│"); c >= 0 {
			c = utf8.RuneCountInString(line[:c])
			if col >= 0 && c != col {
				t.Errorf("pane divider not aligned: column %d vs %d in %q", c, col, line)
				break
			}
			col = c
		}
	}
	if lines := strings.Count(out, "\n") + 1; lines > 36 {
		t.Errorf("render is %d lines, taller than the 36-line window", lines)
	}

	// Moving down and opening the add form must not panic.
	m, _ = mm.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if m.(model).mode != modeForm {
		t.Fatal("a should open the add form")
	}
	t.Log("\n" + ansi.ReplaceAllString(m.View().Content, ""))
}
