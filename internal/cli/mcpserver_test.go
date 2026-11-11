package cli

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/clients/clienttest"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/secrets"
)

// session starts `toolshed mcp` in-process with an isolated HOME, toolshed
// home and mock claude/codex CLIs, and returns a connected client.
func session(t *testing.T) *mcp.ClientSession {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("TOOLSHED_HOME", filepath.Join(home, ".toolshed"))
	t.Setenv("TOOLSHED_REGISTRY", "")
	mocks, _ := filepath.Abs("../../test/mocks")
	t.Setenv("PATH", mocks+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(home)
	secrets.UseMockForTest(false)

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := newMCPServer().Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res.Content[0].(*mcp.TextContent).Text, res.IsError
}

func TestMCPAddAndInstall(t *testing.T) {
	cs := session(t)

	out, isErr := callTool(t, cs, "add_server", map[string]any{"kind": "url", "source": "https://mcp.docs.example.com/mcp"})
	if isErr || !strings.Contains(out, "Added docs") || !strings.Contains(out, "toolshed approve docs") {
		t.Fatalf("add_server: %s", out)
	}
	reg, _ := registry.Load()
	if e := reg.Get("docs"); e == nil || e.Source != registry.SourceUser || !e.Pending {
		t.Fatalf("expected a pending user entry, got %+v", e)
	}

	// Nothing the agent added runs or installs before the user approves it.
	for _, tool := range []string{"inspect_server", "install_server"} {
		if out, isErr := callTool(t, cs, tool, map[string]any{"id": "docs"}); !isErr || !strings.Contains(out, "not approved") {
			t.Fatalf("%s on a pending server should refuse: %s", tool, out)
		}
	}
	if err := registry.ApproveUserEntry("docs"); err != nil { // the user, in a terminal
		t.Fatal(err)
	}

	out, isErr = callTool(t, cs, "install_server", map[string]any{"id": "docs"})
	if isErr || !strings.Contains(out, "Installed docs into claude") {
		t.Fatalf("install_server: %s", out)
	}
	data, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".claude.json"))
	if !strings.Contains(string(data), "mcp.docs.example.com") {
		t.Fatalf("not registered with claude: %s", data)
	}

	out, _ = callTool(t, cs, "list_servers", nil)
	if !strings.Contains(out, `"installed_in": [`+"\n"+`      "claude"`) {
		t.Fatalf("list_servers should show docs installed:\n%s", out)
	}

	out, isErr = callTool(t, cs, "uninstall_server", map[string]any{"id": "docs"})
	if isErr || !strings.Contains(out, "Uninstalled") {
		t.Fatalf("uninstall_server: %s", out)
	}
}

func TestMCPNeverTakesSecrets(t *testing.T) {
	cs := session(t)

	out, isErr := callTool(t, cs, "add_server", map[string]any{"kind": "command", "source": "sh", "id": "jira", "secret_env": []string{"JIRA_TOKEN"}})
	if isErr || !strings.Contains(out, "toolshed install jira") {
		t.Fatalf("add_server with secrets should point the user to the terminal: %s", out)
	}
	out, isErr = callTool(t, cs, "install_server", map[string]any{"id": "jira", "values": map[string]any{"JIRA_TOKEN": "oops"}})
	if !isErr || !strings.Contains(out, "never pass secrets") {
		t.Fatalf("secret values must be refused: %s", out)
	}
	_ = registry.ApproveUserEntry("jira")
	out, isErr = callTool(t, cs, "install_server", map[string]any{"id": "jira"})
	if !isErr || !strings.Contains(out, "needs JIRA_TOKEN") {
		t.Fatalf("missing secret should explain itself: %s", out)
	}
}

// An agent must not be able to swap the command behind a tool the user
// already has: the replacement would inherit that tool's keyring secrets.
func TestMCPCannotReplaceExistingTool(t *testing.T) {
	cs := session(t)
	secrets.UseMockForTest(true)
	if err := registry.AddUserEntry(registry.Entry{ID: "jira", Command: "true",
		Inputs: []registry.Input{{Key: "JIRA_TOKEN", Secret: true}}}); err != nil {
		t.Fatal(err)
	}
	_ = secrets.Set("jira", "JIRA_TOKEN", "s3cret")

	out, isErr := callTool(t, cs, "add_server", map[string]any{"id": "jira", "kind": "command",
		"source": "sh -c env", "secret_env": []string{"JIRA_TOKEN"}})
	if !isErr || !strings.Contains(out, "already has a server called jira") {
		t.Fatalf("replacing an existing tool must be refused: %s", out)
	}
	reg, _ := registry.Load()
	if e := reg.Get("jira"); e.Command != "true" || e.Pending {
		t.Fatalf("existing tool was modified: %+v", e)
	}
}

func TestMCPInspectIsNotReadOnly(t *testing.T) {
	cs := session(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Name == "inspect_server" && tool.Annotations.ReadOnlyHint {
			t.Fatal("inspect_server starts programs; it must not claim to be read-only")
		}
	}
}

func TestMCPAddWithTrailingSlashDoesNotPanic(t *testing.T) {
	cs := session(t)
	if out, isErr := callTool(t, cs, "add_server", map[string]any{"kind": "npx", "source": "@acme/"}); !isErr {
		t.Fatalf("an id can't be derived from @acme/, expected an error: %s", out)
	}
}

func TestMCPErrors(t *testing.T) {
	cs := session(t)
	if out, isErr := callTool(t, cs, "add_server", map[string]any{"kind": "teleport", "source": "x"}); !isErr {
		t.Fatalf("bad kind should fail: %s", out)
	}
	if out, isErr := callTool(t, cs, "add_server", map[string]any{"kind": "npx", "source": "@modelcontextprotocol/server-memory"}); !isErr || !strings.Contains(out, "built-in") {
		t.Fatalf("re-adding a built-in should say so: %s", out)
	}
	if out, isErr := callTool(t, cs, "install_server", map[string]any{"id": "nope"}); !isErr {
		t.Fatalf("unknown id should fail: %s", out)
	}
}

// The agent can reinstall a tool where it is, but not move it: moving can
// take it out of a repository's committed config.
func TestMCPWontMoveScope(t *testing.T) {
	cs := session(t)
	repo := filepath.Join(os.Getenv("HOME"), "repo")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	t.Chdir(repo)
	_ = registry.AddUserEntry(registry.Entry{ID: "hi", Command: "sh"})
	for _, c := range []string{clients.Claude, clients.Codex} {
		if err := clients.Register(c, "hi", clients.Config{Type: "stdio", Command: "sh"}, "project"); err != nil {
			t.Fatal(err)
		}
	}
	inRepo := map[string]string{clients.Claude: "project", clients.Codex: "project"}
	inRepoNow := map[string]string{clients.Claude: "project", clients.Codex: "project", clients.Antigravity: "project"}

	out, isErr := callTool(t, cs, "install_server", map[string]any{"id": "hi", "scope": "user"})
	if !isErr || !strings.Contains(out, "toolshed install hi --user") {
		t.Fatalf("a move should be refused: %s", out)
	}
	if got := clienttest.Placements(t, "hi", repo); !maps.Equal(got, inRepo) {
		t.Fatalf("refused move changed the registrations: %v", got)
	}
	out, isErr = callTool(t, cs, "install_server", map[string]any{"id": "hi"})
	if isErr {
		t.Fatalf("reinstall where it is: %s", out)
	}
	if got := clienttest.Placements(t, "hi", repo); !maps.Equal(got, inRepoNow) { // and Antigravity, which didn't have it, gets it there too
		t.Fatalf("reinstall without a scope should keep it in the repo, got %v", got)
	}
}
