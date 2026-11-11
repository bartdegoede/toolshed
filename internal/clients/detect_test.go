package clients

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectClaudeAndCodex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	t.Chdir(t.TempDir()) // not a git repo
	_ = os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{
	  "mcpServers": {"hello": {"type":"stdio","command":"/bin/toolshed","args":["exec","hello"]},
	                 "docs": {"type":"http","url":"https://x/mcp"}}}`), 0o644)
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	_ = os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(`
model = "o3"   # unrelated settings are ignored

[mcp_servers.hello]
command = "/bin/toolshed"
args = [
  "exec",
  "hello",   # multi-line arrays and comments are fine
]

[mcp_servers.my_tool]
command = 'my-tool'
env = { TOKEN = "x" }

[mcp_servers.my_tool.tools.search]
approval = "never"
`), 0o644)

	got := Detect()
	if h := got["hello"]; h == nil || h.Scope(Claude) != "user" || !h.Has(Codex) {
		t.Fatalf("hello should be in both clients: %+v", h)
	}
	if d := got["docs"]; d == nil || d.Has(Codex) || d.Config().Type != "http" {
		t.Fatalf("docs should be claude-only http: %+v", d)
	}
	m := got["my-tool"]
	if m == nil || !m.Has(Codex) || m.Config().Command != "my-tool" || m.Config().Env["TOKEN"] != "x" {
		t.Fatalf("codex underscore name should fold to my-tool with its config: %+v", m)
	}
	if m.Regs[Codex].Key != "my_tool" {
		t.Fatalf("the folded name must remember its key in config.toml, got %q", m.Regs[Codex].Key)
	}
	if len(got) != 3 {
		t.Fatalf("subtables must not create ghost entries, got %d: %v", len(got), got)
	}
}

func TestDetectMissingFilesIsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	t.Setenv("CODEX_HOME", "")
	if got := Detect(); len(got) != 0 {
		t.Fatalf("expected nothing, got %v", got)
	}
}
