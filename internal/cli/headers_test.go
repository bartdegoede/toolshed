package cli

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/secrets"
)

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	err := fn()
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	return string(out), err
}

// What Claude Code and Codex run to get a remote tool's headers.
func TestHeadersCommandPrintsJSON(t *testing.T) {
	sandbox(t)
	secrets.UseMockForTest(true)
	_ = secrets.Set("gh", "GH_TOKEN", "s3cret")
	out, err := captureStdout(t, func() error {
		return runHeaders([]string{"gh", "--secret", "GH_TOKEN=gh", "--set", "TEAM=core", "--header", "Authorization=Bearer ${GH_TOKEN}", "--header", "X-Team=${TEAM}"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["Authorization"] != "Bearer s3cret" || got["X-Team"] != "core" {
		t.Fatalf("output %q: %v", out, err)
	}
	if _, err := captureStdout(t, func() error {
		return runHeaders([]string{"gh", "--secret", "MISSING=gh", "--header", "A=${MISSING}"})
	}); err == nil || !strings.Contains(err.Error(), "toolshed install gh") {
		t.Fatalf("a missing secret should say how to fix it: %v", err)
	}
}

func TestAddRemoteWithHeaders(t *testing.T) {
	sandbox(t)
	err := runAdd([]string{"gh", "--url", "https://x.example/mcp", "--header", "Authorization=Bearer ghp_literal"})
	if err == nil || !strings.Contains(err.Error(), "plain text") {
		t.Fatalf("a literal token in a header must be refused: %v", err)
	}
	if err := runAdd([]string{"gh", "--url", "https://x.example/mcp", "--header", "Authorization=Bearer ${GH_TOKEN}", "--secret", "GH_TOKEN"}); err != nil {
		t.Fatal(err)
	}
	reg, _ := registry.Load()
	e := reg.Get("gh")
	if e.Headers["Authorization"] != "Bearer ${GH_TOKEN}" || len(e.SecretKeys()) != 1 {
		t.Fatalf("entry: %+v", e)
	}
}

func TestMCPAddRemoteWithHeaders(t *testing.T) {
	cs := session(t)
	out, isErr := callTool(t, cs, "add_server", map[string]any{"kind": "url", "source": "https://api.acme.example/mcp",
		"headers": map[string]string{"Authorization": "Bearer sk-live-123"}})
	if !isErr || !strings.Contains(out, "never put secret values") {
		t.Fatalf("literal secret header must be refused: %s", out)
	}
	out, isErr = callTool(t, cs, "add_server", map[string]any{"kind": "url", "source": "https://api.acme.example/mcp",
		"headers": map[string]string{"Authorization": "Bearer ${ACME_TOKEN}"}, "secret_env": []string{"ACME_TOKEN"}})
	if isErr || !strings.Contains(out, "Added acme") {
		t.Fatalf("add_server: %s", out)
	}
}
