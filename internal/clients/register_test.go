package clients

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func useMockClaude(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	mocks, _ := filepath.Abs("../../test/mocks")
	t.Setenv("PATH", mocks+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(home)
	return home
}

func claudeServers(t *testing.T, home string) map[string]Config {
	t.Helper()
	var f struct {
		MCPServers map[string]Config `json:"mcpServers"`
	}
	data, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	_ = json.Unmarshal(data, &f)
	return f.MCPServers
}

// Replacing is remove-then-add; when the add fails the old registration
// must come back instead of the tool silently disappearing.
func TestRegisterKeepsPreviousOnFailedReplace(t *testing.T) {
	home := useMockClaude(t)
	if err := Register(Claude, "demo", Config{Type: "stdio", Command: "good-server"}, "user"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MOCK_CLAUDE_FAIL_ADD", "broken-server")
	err := Register(Claude, "demo", Config{Type: "stdio", Command: "broken-server"}, "user")
	if err == nil || !strings.Contains(err.Error(), "kept the previous registration") {
		t.Fatalf("expected a failure that kept the old registration, got %v", err)
	}
	if got := claudeServers(t, home)["demo"].Command; got != "good-server" {
		t.Fatalf("previous registration lost: command = %q", got)
	}
}

func TestRegisterNewFailureHasNothingToRestore(t *testing.T) {
	home := useMockClaude(t)
	t.Setenv("MOCK_CLAUDE_FAIL_ADD", "broken-server")
	err := Register(Claude, "demo", Config{Type: "stdio", Command: "broken-server"}, "user")
	if err == nil || strings.Contains(err.Error(), "kept") {
		t.Fatalf("expected a plain failure, got %v", err)
	}
	if _, ok := claudeServers(t, home)["demo"]; ok {
		t.Fatal("nothing should be registered")
	}
}
