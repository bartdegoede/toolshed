package shed

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/secrets"
)

func tool() *registry.Entry {
	return &registry.Entry{
		ID: "demo", Command: "demo-server", Args: []string{"--flag"},
		Env: map[string]string{"FIXED": "1"},
		Inputs: []registry.Input{
			{Key: "TOKEN", Secret: true},
			{Key: "REGION", Default: "eu"},
			{Key: "NICKNAME", Optional: true},
		},
	}
}

func TestResolvePrecedence(t *testing.T) {
	secrets.UseMockForTest(true)
	e := tool()

	vals, missing := Resolve(e, nil, nil)
	if len(missing) != 1 || missing[0].Key != "TOKEN" {
		t.Fatalf("expected TOKEN missing, got %+v", missing)
	}
	if vals["REGION"] != "eu" {
		t.Errorf("default not applied: %v", vals)
	}

	_ = secrets.Set("demo", "TOKEN", "from-keyring")
	inst := inClaude(clients.Config{Env: map[string]string{"REGION": "us"}})
	vals, missing = Resolve(e, inst, Values{"NICKNAME": "nick"})
	if len(missing) != 0 || vals["TOKEN"] != "from-keyring" || vals["REGION"] != "us" || vals["NICKNAME"] != "nick" {
		t.Fatalf("unexpected resolve: %v missing=%v", vals, missing)
	}

	vals, _ = Resolve(e, inst, Values{"REGION": "ap"})
	if vals["REGION"] != "ap" {
		t.Errorf("override should beat installed value, got %q", vals["REGION"])
	}
}

func TestBuildConfigWrapsSecretsWithKeyring(t *testing.T) {
	secrets.UseMockForTest(true)
	cfg, err := BuildConfig(tool(), Values{"TOKEN": "s3cret", "REGION": "eu"}, "user")
	if err != nil {
		t.Fatal(err)
	}
	want := "exec demo --secret TOKEN=demo -- demo-server --flag"
	if got := strings.Join(cfg.Args, " "); got != want {
		t.Fatalf("exec args = %q, want %q", got, want)
	}
	if _, leaked := cfg.Env["TOKEN"]; leaked {
		t.Fatal("secret must not be written into the client config")
	}
	if cfg.Env["REGION"] != "eu" || cfg.Env["FIXED"] != "1" {
		t.Errorf("non-secret env missing: %v", cfg.Env)
	}
	if v, _ := secrets.Get("demo", "TOKEN"); v != "s3cret" {
		t.Errorf("secret not stored in keyring, got %q", v)
	}
}

func TestBuildConfigWithoutKeyringUsesEnv(t *testing.T) {
	secrets.UseMockForTest(false)
	cfg, err := BuildConfig(tool(), Values{"TOKEN": "s3cret"}, "user")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Command != "demo-server" || cfg.Env["TOKEN"] != "s3cret" {
		t.Fatalf("expected plain command with env secret, got %+v", cfg)
	}
}

func TestBuildConfigRemote(t *testing.T) {
	cfg, _ := BuildConfig(&registry.Entry{ID: "r", URL: "https://x/mcp", HeaderCmd: "h"}, nil, "user")
	if cfg.Type != "http" || cfg.URL != "https://x/mcp" || cfg.HeadersHelper != "h" {
		t.Fatalf("bad remote config: %+v", cfg)
	}
}

func TestExecEnvExplicitEnvWins(t *testing.T) {
	secrets.UseMockForTest(true)
	_ = secrets.Set("demo", "TOKEN", "from-keyring")
	t.Setenv("TOKEN", "from-env")
	env := strings.Join(EntryEnv(tool()), "\n")
	if !strings.Contains(env, "TOKEN=from-env") || strings.Contains(env, "TOKEN=from-keyring") {
		t.Fatalf("explicit env should win over keyring:\n%s", env)
	}
	if !strings.Contains(env, "FIXED=1") || !strings.Contains(env, "REGION=eu") {
		t.Fatalf("fixed env / defaults missing")
	}
}

func TestExecEnvFillsSecretFromKeyring(t *testing.T) {
	secrets.UseMockForTest(true)
	_ = secrets.Set("demo", "TOKEN", "from-keyring")
	os.Unsetenv("TOKEN")
	if env := strings.Join(EntryEnv(tool()), "\n"); !strings.Contains(env, "TOKEN=from-keyring") {
		t.Fatal("secret should be injected from the keyring")
	}
}

func TestCheckCommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	e := &registry.Entry{ID: "x", Command: "my-server"}
	if err := CheckCommand(e); err == nil || !strings.Contains(err.Error(), "not found on PATH") {
		t.Fatalf("expected not-found error, got %v", err)
	}
	_ = os.WriteFile(filepath.Join(dir, "my-server"), []byte("#!/bin/sh\n"), 0o755)
	if err := CheckCommand(e); err != nil {
		t.Fatal(err)
	}
}

func TestCellarToOpt(t *testing.T) {
	for in, want := range map[string]string{
		"/opt/homebrew/Cellar/toolshed/1.2.0/bin/toolshed": "/opt/homebrew/opt/toolshed/bin/toolshed",
		"/usr/local/bin/toolshed":                          "/usr/local/bin/toolshed",
	} {
		if got := CellarToOpt(in); got != want {
			t.Errorf("CellarToOpt(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestPendingToolsNeverRunOrInstall(t *testing.T) {
	secrets.UseMockForTest(true)
	e := &registry.Entry{ID: "sneaky", Command: "sh", Args: []string{"-c", "exit 0"}, Pending: true}
	if _, err := Probe(t.Context(), e); !errors.Is(err, ErrPending) {
		t.Fatalf("Probe of a pending tool: %v", err)
	}
	if _, err := Install(e, nil, InstallOptions{}); !errors.Is(err, ErrPending) {
		t.Fatalf("Install of a pending tool: %v", err)
	}
}

func TestProjectScopeUsesBareToolshedCommand(t *testing.T) {
	secrets.UseMockForTest(true)
	cfg, err := BuildConfig(tool(), Values{"TOKEN": "s3cret", "REGION": "eu"}, "project")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Command != "toolshed" {
		t.Fatalf(".mcp.json is shared, so the wrapper must be on PATH, not %q", cfg.Command)
	}
	if cfg, _ = BuildConfig(tool(), Values{"TOKEN": "s3cret"}, "user"); !filepath.IsAbs(cfg.Command) {
		t.Fatalf("user scope should keep the absolute path, got %q", cfg.Command)
	}
}

func TestProjectScopeRefusesPlaintextSecrets(t *testing.T) {
	secrets.UseMockForTest(false) // no keyring
	bin := t.TempDir()
	_ = os.WriteFile(filepath.Join(bin, "demo-server"), []byte("#!/bin/sh\n"), 0o755)
	_ = os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	t.Setenv("PATH", bin)
	_, err := Install(tool(), Values{"TOKEN": "s3cret", "REGION": "eu"}, InstallOptions{Scope: "project"})
	if err == nil || !strings.Contains(err.Error(), "plain text") {
		t.Fatalf("expected a refusal to write the secret into .mcp.json, got %v", err)
	}
}
