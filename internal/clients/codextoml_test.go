package clients

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

const userConfig = `model = "gpt-5" # my model

[projects."/repo"]
trust_level = "trusted"

# the tool I use most
[mcp_servers.my-tool]
startup_timeout_sec = 30
disabled_tools = ["danger"]
command = "old"
args = [
  "-v",
  "x",   # comment in an array
]
cwd = "/work"

[mcp_servers.my-tool.env]
OLD = "1"

# per-tool approval
[mcp_servers.my-tool.tools.search]
approval_mode = "approve"

[mcp_servers.other]
command = "keep-me"
`

func decode(t *testing.T, doc []byte) map[string]map[string]any {
	t.Helper()
	var f struct {
		MCPServers map[string]map[string]any `toml:"mcp_servers"`
	}
	if _, err := toml.Decode(string(doc), &f); err != nil {
		t.Fatalf("invalid TOML: %v\n%s", err, doc)
	}
	return f.MCPServers
}

func mustEdit(t *testing.T, doc, name, legacy string, s *codexServer) string {
	t.Helper()
	out, err := editCodexServer([]byte(doc), name, legacy, s)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestEditKeepsTheUsersSettings(t *testing.T) {
	out := mustEdit(t, userConfig, "my-tool", "", &codexServer{Command: "new", Args: []string{"a b", `q"`}, Env: map[string]string{"NEW": "2"}})
	for _, want := range []string{
		`model = "gpt-5" # my model`, "# the tool I use most", "startup_timeout_sec = 30", `disabled_tools = ["danger"]`,
		`cwd = "/work"`, "# per-tool approval", `approval_mode = "approve"`, `command = "keep-me"`, `trust_level = "trusted"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lost %q:\n%s", want, out)
		}
	}
	for _, gone := range []string{`"old"`, "OLD", "comment in an array", "[mcp_servers.my-tool.env]"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q should be replaced:\n%s", gone, out)
		}
	}
	srv := decode(t, []byte(out))["my-tool"]
	if srv["command"] != "new" || srv["startup_timeout_sec"] != int64(30) {
		t.Fatalf("decoded: %v", srv)
	}
	if args := srv["args"].([]any); len(args) != 2 || args[0] != "a b" || args[1] != `q"` {
		t.Fatalf("args: %v", args)
	}
	if env := srv["env"].(map[string]any); len(env) != 1 || env["NEW"] != "2" {
		t.Fatalf("env: %v", env)
	}
	if srv["tools"] == nil {
		t.Fatal("per-tool settings lost")
	}
}

func TestEditSwitchToRemoteDropsStdioKeys(t *testing.T) {
	out := mustEdit(t, userConfig, "my-tool", "", &codexServer{URL: "https://x.example/mcp", HeadersHelper: "toolshed headers my-tool"})
	srv := decode(t, []byte(out))["my-tool"]
	for _, k := range []string{"command", "args", "env", "cwd"} {
		if _, ok := srv[k]; ok {
			t.Errorf("%s should be gone for a remote server:\n%s", k, out)
		}
	}
	if srv["url"] != "https://x.example/mcp" || srv["http_headers_helper"] != "toolshed headers my-tool" || srv["startup_timeout_sec"] != int64(30) {
		t.Fatalf("remote: %v", srv)
	}
}

func TestEditKeepsRemoteAuthItDoesntOwn(t *testing.T) {
	doc := "[mcp_servers.r]\nurl = \"https://old\"\nbearer_token_env_var = \"MY_TOKEN\"\nhttp_headers = { X-Team = \"a\" }\n"
	srv := decode(t, []byte(mustEdit(t, doc, "r", "", &codexServer{URL: "https://new"})))["r"]
	if srv["url"] != "https://new" || srv["bearer_token_env_var"] != "MY_TOKEN" || srv["http_headers"] == nil {
		t.Fatalf("user's auth settings should stay: %v", srv)
	}
	// Setting a headers helper replaces static headers (they move to the keyring).
	srv = decode(t, []byte(mustEdit(t, doc, "r", "", &codexServer{URL: "https://new", HeadersHelper: "h"})))["r"]
	if srv["http_headers"] != nil || srv["http_headers_helper"] != "h" {
		t.Fatalf("static headers should be replaced by the helper: %v", srv)
	}
}

func TestEditAddsAndRemoves(t *testing.T) {
	for _, doc := range []string{"", "model = \"x\"", "model = \"x\"\n"} {
		out := mustEdit(t, doc, "dotted.name", "", &codexServer{Command: "sh"})
		if !strings.Contains(out, `[mcp_servers."dotted.name"]`) || decode(t, []byte(out))["dotted.name"]["command"] != "sh" {
			t.Fatalf("add to %q:\n%s", doc, out)
		}
	}
	out := mustEdit(t, userConfig, "my-tool", "", nil)
	servers := decode(t, []byte(out))
	if _, ok := servers["my-tool"]; ok || servers["other"]["command"] != "keep-me" {
		t.Fatalf("remove: %v\n%s", servers, out)
	}
	if strings.Contains(out, "approval_mode") || !strings.Contains(out, `trust_level = "trusted"`) {
		t.Fatalf("remove should take the subtables and nothing else:\n%s", out)
	}
}

// toolshed used to register "my-tool" as "my_tool" in Codex.
func TestEditRenamesTheLegacyName(t *testing.T) {
	doc := strings.ReplaceAll(userConfig, "mcp_servers.my-tool", "mcp_servers.my_tool")
	out := mustEdit(t, doc, "my-tool", "my_tool", &codexServer{Command: "new"})
	servers := decode(t, []byte(out))
	if _, ok := servers["my_tool"]; ok {
		t.Fatalf("old name still there:\n%s", out)
	}
	if s := servers["my-tool"]; s["command"] != "new" || s["startup_timeout_sec"] != int64(30) || s["tools"] == nil {
		t.Fatalf("renamed server lost its settings: %v\n%s", s, out)
	}
	out = mustEdit(t, doc, "my-tool", "my_tool", nil)
	if _, ok := decode(t, []byte(out))["my_tool"]; ok {
		t.Fatal("removing should also remove the legacy name")
	}
}

func TestEditRefusesInlineServers(t *testing.T) {
	for _, doc := range []string{
		"[mcp_servers]\nmy-tool = { command = \"sh\" }\n",
		"mcp_servers.my-tool.command = \"sh\"\n",
	} {
		if _, err := editCodexServer([]byte(doc), "my-tool", "", &codexServer{Command: "x"}); !errors.Is(err, errCodexLayout) {
			t.Errorf("%q: want errCodexLayout, got %v", doc, err)
		}
	}
}

func TestWriteCodexServerKeepsFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(userConfig), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := writeCodexServer(path, "my-tool", "", &codexServer{Command: "new"}); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
	if err := writeCodexServer(filepath.Join(t.TempDir(), "new", "config.toml"), "x", "", &codexServer{Command: "sh"}); err != nil {
		t.Fatalf("creating a new config: %v", err)
	}
}
