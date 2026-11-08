package cli

import (
	"bufio"
	"strings"
	"testing"

	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/secrets"
)

func withStdin(t *testing.T, s string) {
	t.Helper()
	old := stdin
	stdin = bufio.NewReader(strings.NewReader(s))
	t.Cleanup(func() { stdin = old })
}

func TestSecretSetWritesTheKeyring(t *testing.T) {
	sandbox(t)
	secrets.UseMockForTest(true)
	_ = registry.AddUserEntry(registry.Entry{ID: "jira", Command: "sh", Inputs: []registry.Input{{Key: "JIRA_TOKEN", Secret: true}}})
	_ = registry.AddUserEntry(registry.Entry{ID: "two", Command: "sh", Inputs: []registry.Input{{Key: "A", Secret: true}, {Key: "B", Secret: true}}})

	withStdin(t, "new-token\n")
	if err := runSecret([]string{"set", "jira"}); err != nil {
		t.Fatal(err)
	}
	if v, _ := secrets.Get("jira", "JIRA_TOKEN"); v != "new-token" {
		t.Fatalf("keyring has %q", v)
	}

	if err := runSecret([]string{"set", "two"}); err == nil || !strings.Contains(err.Error(), "A, B") {
		t.Fatalf("several secrets: want a question naming them, got %v", err)
	}
	withStdin(t, "b-value\n")
	if err := runSecret([]string{"set", "two", "B"}); err != nil {
		t.Fatal(err)
	}
	if v, _ := secrets.Get("two", "B"); v != "b-value" {
		t.Fatalf("B = %q", v)
	}
	withStdin(t, "\n")
	if err := runSecret([]string{"set", "jira"}); err == nil {
		t.Fatal("an empty value must not overwrite the secret")
	}
	if err := runSecret([]string{"set", "jira", "NOPE"}); err == nil {
		t.Fatal("unknown key should fail")
	}
}

func TestCompletionScripts(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "fish"} {
		var b strings.Builder
		switch sh {
		case "bash":
			bashCompletion(&b)
		case "zsh":
			zshCompletion(&b)
		case "fish":
			fishCompletion(&b)
		}
		out := b.String()
		for _, want := range []string{"install", "doctor", "toolshed __ids"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s completion is missing %q", sh, want)
			}
		}
		if strings.Contains(out, "__ids'") || strings.Contains(out, " exec") {
			t.Errorf("%s completion offers hidden commands:\n%s", sh, out)
		}
	}
	if err := runCompletion([]string{"tcsh"}); err == nil {
		t.Fatal("unknown shell should fail")
	}
}
