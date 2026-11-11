package cli

import (
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
