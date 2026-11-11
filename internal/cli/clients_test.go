package cli

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/shed"
)

// onTerminal makes install think it's talking to a person.
func onTerminal(t *testing.T) {
	old := stdinIsTerminal
	stdinIsTerminal = func() bool { return true }
	t.Cleanup(func() { stdinIsTerminal = old })
}

func installedIn(t *testing.T, id string) []string {
	t.Helper()
	inst := clients.Detect()[id]
	if inst == nil {
		return nil
	}
	return inst.Clients()
}

// The first install in a terminal asks which clients to use, saves the
// answer, and doesn't ask again.
func TestFirstInstallAsksForClients(t *testing.T) {
	sandbox(t)
	onTerminal(t)
	_ = registry.AddUserEntry(registry.Entry{ID: "one", Command: "sh"})
	_ = registry.AddUserEntry(registry.Entry{ID: "two", Command: "sh"})
	withStdin(t, "vim\nclaude, codex\n") // a typo is asked again
	out, err := captureStdout(t, func() error { return runInstall([]string{"one"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "unknown: vim") || !strings.Contains(out, "Saved: toolshed installs into Claude Code and Codex") {
		t.Fatalf("output:\n%s", out)
	}
	want := []string{clients.Claude, clients.Codex}
	if got := installedIn(t, "one"); !slices.Equal(got, want) {
		t.Fatalf("installed into %v, want %v", got, want)
	}
	withStdin(t, "") // asking again would fail: nothing to read
	if err := runInstall([]string{"two"}); err != nil {
		t.Fatal(err)
	}
	if got := installedIn(t, "two"); !slices.Equal(got, want) {
		t.Fatalf("installed into %v, want %v", got, want)
	}
}

func TestFirstInstallEnterTakesEveryClient(t *testing.T) {
	sandbox(t)
	onTerminal(t)
	_ = registry.AddUserEntry(registry.Entry{ID: "one", Command: "sh"})
	withStdin(t, "\n")
	if _, err := captureStdout(t, func() error { return runInstall([]string{"one"}) }); err != nil {
		t.Fatal(err)
	}
	if got, chosen := shed.DefaultClients(); !chosen || !slices.Equal(got, clients.All) {
		t.Fatalf("saved %v (%v), want every client", got, chosen)
	}
}

// Scripts (-y, or no terminal) and --client never get the question.
func TestInstallDoesntAskWhenItCant(t *testing.T) {
	for name, args := range map[string][]string{
		"-y":       {"one", "-y"},
		"--client": {"one", "--client", "codex"},
	} {
		t.Run(name, func(t *testing.T) {
			sandbox(t)
			onTerminal(t)
			_ = registry.AddUserEntry(registry.Entry{ID: "one", Command: "sh"})
			withStdin(t, "")
			if err := runInstall(args); err != nil {
				t.Fatal(err)
			}
			if _, chosen := shed.DefaultClients(); chosen {
				t.Fatal("nothing was asked, so nothing should be saved")
			}
		})
	}
}

func TestClientsCommand(t *testing.T) {
	sandbox(t)
	_ = registry.AddUserEntry(registry.Entry{ID: "one", Command: "sh"})
	if err := runInstall([]string{"one", "-y"}); err != nil { // every client: nothing chosen
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error { return runClients(nil) })
	if err != nil || !strings.Contains(out, "You haven't chosen clients yet") {
		t.Fatalf("%v\n%s", err, out)
	}
	out, err = captureStdout(t, func() error { return runClients([]string{"Codex", "claude"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "installs tools into Claude Code and Codex") ||
		!strings.Contains(out, "Antigravity still has one, left as it is (toolshed uninstall <id> --client antigravity removes one)") {
		t.Fatalf("output:\n%s", out)
	}
	if got, _ := shed.DefaultClients(); !slices.Equal(got, []string{clients.Claude, clients.Codex}) {
		t.Fatalf("saved %v", got)
	}
	out, _ = captureStdout(t, func() error { return runClients(nil) })
	if !strings.Contains(out, "antigravity  Antigravity on PATH, not used") || !strings.Contains(out, "Antigravity still has one") {
		t.Fatalf("output:\n%s", out)
	}
	if err := runClients([]string{"vim"}); err == nil {
		t.Fatal("an unknown client should be refused")
	}
	if got, _ := shed.DefaultClients(); !slices.Equal(got, []string{clients.Claude, clients.Codex}) {
		t.Fatalf("a refused change was saved: %v", got)
	}
}

// doctor shows a client you left out, and fails on a settings file install
// can't read, which `toolshed clients <name>...` replaces.
func TestDoctorAndTheChosenClients(t *testing.T) {
	sandbox(t)
	if err := runClients([]string{"claude", "codex"}); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error { return runDoctor([]string{"--no-start"}) })
	if err != nil || !strings.Contains(out, "antigravity not chosen: tools aren't installed into it") {
		t.Fatalf("%v\n%s", err, out)
	}
	_ = os.WriteFile(shed.SettingsPath(), []byte("clients: [claude\n"), 0o644)
	out, err = captureStdout(t, func() error { return runDoctor([]string{"--no-start"}) })
	if err == nil || !strings.Contains(out, "config.yaml") {
		t.Fatalf("a broken settings file should fail doctor: %v\n%s", err, out)
	}
	if err := runClients(nil); err == nil {
		t.Fatal("showing a broken choice should say it's broken")
	}
	if _, err := captureStdout(t, func() error { return runClients([]string{"codex"}) }); err != nil {
		t.Fatalf("choosing again should replace a broken file: %v", err)
	}
	if got, _ := shed.DefaultClients(); !slices.Equal(got, []string{clients.Codex}) {
		t.Fatalf("saved %v", got)
	}
}
