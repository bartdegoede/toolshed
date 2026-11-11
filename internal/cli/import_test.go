package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/secrets"
)

func TestImportMovesSecretsToTheKeyring(t *testing.T) {
	sandbox(t)
	secrets.UseMockForTest(true)
	cfg := clients.Config{Type: "stdio", Command: "sh", Args: []string{"-c", "srv"},
		Env: map[string]string{"API_TOKEN": "s3cret", "LOG_LEVEL": "debug"}}
	if err := clients.Register(clients.Claude, "My_Tool", cfg, "user"); err != nil {
		t.Fatal(err)
	}

	if err := runImport([]string{"--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if reg, _ := registry.Load(); reg.Get("my-tool") != nil {
		t.Fatal("--dry-run must not change anything")
	}
	if err := runImport(nil); err == nil {
		t.Fatal("without a terminal, import needs -y")
	}

	if err := runImport([]string{"-y"}); err != nil {
		t.Fatal(err)
	}
	reg, _ := registry.Load()
	e := reg.Get("my-tool")
	if e == nil || e.Command != "sh" || len(e.Env) != 0 || !slices.Equal(e.SecretKeys(), []string{"API_TOKEN"}) || len(e.Inputs) != 2 {
		t.Fatalf("registry entry: %+v", e)
	}
	if v, _ := secrets.Get("my-tool", "API_TOKEN"); v != "s3cret" {
		t.Fatalf("secret not in the keyring: %q", v)
	}
	installed := clients.Detect()
	if installed["My_Tool"] != nil {
		t.Fatal("the old name should be gone")
	}
	got := installed["my-tool"]
	if got == nil || !slices.Contains(got.Config().Args, "exec") || got.Config().Env["API_TOKEN"] != "" || got.Config().Env["LOG_LEVEL"] != "debug" {
		t.Fatalf("should be re-registered through toolshed exec without the token: %+v", got)
	}
	if err := runImport([]string{"-y"}); err != nil {
		t.Fatalf("importing again is a no-op: %v", err)
	}
	if err := runImport([]string{"my-tool", "-y"}); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("importing a managed tool by name: %v", err)
	}
}

func TestAskAndMask(t *testing.T) {
	for _, c := range []struct {
		in        string
		def, want bool
	}{{"\n", true, true}, {"\n", false, false}, {"y\n", false, true}, {"no\n", true, false}, {"", true, true}} {
		withStdin(t, c.in)
		if got := ask("q?", c.def); got != c.want {
			t.Errorf("ask(%q, %v) = %v", c.in, c.def, got)
		}
	}
	if got := mask("ghp_abcdefghijklmnop"); got != "ghp_… (20 chars)" || mask("short") != "****" {
		t.Errorf("mask: %q", got)
	}
}

// Importing a remote server with a token that only Antigravity has: toolshed
// would register it through a headers command, which Antigravity can't run,
// so Antigravity's registration is left exactly as it is and the server is
// only added to the registry, instead of failing (or removing it).
func TestImportLeavesWhatAntigravityCantTake(t *testing.T) {
	home := sandbox(t)
	secrets.UseMockForTest(true)
	agy := filepath.Join(home, ".gemini", "config", "mcp_config.json")
	before := `{"mcpServers": {"tok": {"serverUrl": "https://t.example/mcp", "headers": {"Authorization": "Bearer sk-live-abcdefghijkl"}}}}`
	_ = os.MkdirAll(filepath.Dir(agy), 0o755)
	_ = os.WriteFile(agy, []byte(before), 0o600)
	out, err := captureStdout(t, func() error { return runImport([]string{"tok", "-y"}) })
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "Antigravity's registration is left as it is") || !strings.Contains(out, "only added to your registry") {
		t.Fatalf("should say what it did:\n%s", out)
	}
	if reg, _ := registry.Load(); reg.Get("tok") == nil {
		t.Fatal("not added to the registry")
	}
	if data, _ := os.ReadFile(agy); string(data) != before {
		t.Fatalf("Antigravity's registration changed:\n%s", data)
	}
}
