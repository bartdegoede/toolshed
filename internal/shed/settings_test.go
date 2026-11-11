package shed

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
)

// mocks is resolved once: tests change directory.
var mocks, _ = filepath.Abs("../../test/mocks")

// sandboxWith gives a test an isolated HOME and toolshed home, with only the
// named mock clients on PATH (and nothing else that could be a real client).
func sandboxWith(t *testing.T, names ...string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("TOOLSHED_HOME", filepath.Join(home, ".toolshed"))
	t.Setenv("TOOLSHED_REGISTRY", "")
	bin := t.TempDir()
	for _, c := range names {
		cmd := map[string]string{clients.Claude: "claude", clients.Codex: "codex", clients.Antigravity: "agy"}[c]
		if err := os.Symlink(filepath.Join(mocks, cmd), filepath.Join(bin, cmd)); err != nil {
			t.Fatal(err)
		}
	}
	if jq, err := exec.LookPath("jq"); err == nil { // the mock claude needs it
		_ = os.Symlink(jq, filepath.Join(bin, "jq"))
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Chdir(home)
}

func TestSettingsRoundTrip(t *testing.T) {
	sandboxWith(t)
	if s, err := LoadSettings(); err != nil || len(s.Clients) != 0 {
		t.Fatalf("no file should be no settings: %+v %v", s, err)
	}
	if err := SaveClients([]string{clients.Antigravity, clients.Claude}); err != nil {
		t.Fatal(err)
	}
	if got, chosen := DefaultClients(); !chosen || !slices.Equal(got, []string{clients.Claude, clients.Antigravity}) {
		t.Fatalf("got %v %v, want claude and antigravity, in display order", got, chosen)
	}
	if err := SaveClients(nil); err == nil {
		t.Fatal("an empty choice should be refused")
	}
	if err := SaveClients([]string{"vim"}); err == nil {
		t.Fatal("an unknown client should be refused")
	}
	// A newer toolshed may know clients this one doesn't: they're skipped.
	_ = os.WriteFile(SettingsPath(), []byte("clients: [zed, codex]\n"), 0o644)
	if got, _ := DefaultClients(); !slices.Equal(got, []string{clients.Codex}) {
		t.Fatalf("got %v, want codex", got)
	}
}

func TestTargetsUseTheChosenClients(t *testing.T) {
	sandboxWith(t, clients.Claude, clients.Codex, clients.Antigravity)
	e := &registry.Entry{ID: "hi", Command: "sh"}
	if got := Targets(e, nil, InstallOptions{}); !slices.Equal(got, clients.All) {
		t.Fatalf("nothing chosen: got %v, want every client", got)
	}
	_ = SaveClients([]string{clients.Claude, clients.Codex})
	if got := Targets(e, nil, InstallOptions{}); !slices.Equal(got, []string{clients.Claude, clients.Codex}) {
		t.Fatalf("got %v, want the chosen ones", got)
	}
	// A client that has the tool already gets it again, so a reinstall
	// doesn't leave a stale copy there.
	inAgy := &clients.Installation{Regs: map[string]clients.Registration{clients.Antigravity: {Scope: "user"}}}
	if got := Targets(e, inAgy, InstallOptions{}); !slices.Equal(got, clients.All) {
		t.Fatalf("got %v, want the chosen ones and antigravity", got)
	}
	if got := Targets(e, inAgy, InstallOptions{Clients: []string{clients.Codex}}); !slices.Equal(got, []string{clients.Codex}) {
		t.Fatalf("--client: got %v, want codex only", got)
	}
}

// Changing your choice doesn't take a tool out of a client, and installing
// it again still updates the copy there.
func TestReinstallUpdatesACopyOutsideTheChoice(t *testing.T) {
	sandboxWith(t, clients.Claude, clients.Codex, clients.Antigravity)
	e := &registry.Entry{ID: "hi", Command: "sh", Args: []string{"-c", "old"}}
	if _, err := Install(e, Values{}, InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	_ = SaveClients([]string{clients.Claude})
	e.Args = []string{"-c", "new"}
	res, err := Install(e, Values{}, KeepScopes(clients.Detect()["hi"]))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Clients, clients.All) {
		t.Fatalf("installed into %v, want every client that had it", res.Clients)
	}
	if got := clients.Detect()["hi"].Regs[clients.Antigravity].Config.Args; !slices.Equal(got, e.Args) {
		t.Fatalf("antigravity's copy wasn't updated: %v", got)
	}
}

func TestInstallIntoTheChosenClients(t *testing.T) {
	sandboxWith(t, clients.Claude, clients.Codex, clients.Antigravity)
	_ = SaveClients([]string{clients.Codex})
	res, err := Install(&registry.Entry{ID: "hi", Command: "sh"}, Values{}, InstallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := clients.Detect()["hi"].Clients(); !slices.Equal(got, []string{clients.Codex}) || !slices.Equal(res.Clients, got) {
		t.Fatalf("installed into %v, want codex only", got)
	}
}

func TestInstallWhenNoChosenClientIsHere(t *testing.T) {
	sandboxWith(t, clients.Claude)
	_ = SaveClients([]string{clients.Codex})
	_, err := Install(&registry.Entry{ID: "hi", Command: "sh"}, Values{}, InstallOptions{})
	if err == nil || !strings.Contains(err.Error(), "toolshed clients") {
		t.Fatalf("expected an error pointing at `toolshed clients`, got %v", err)
	}
	if clients.Detect()["hi"] != nil {
		t.Fatal("it was installed anyway")
	}
}

// A settings file toolshed can't read must not quietly mean "every client".
func TestInstallRefusesABrokenSettingsFile(t *testing.T) {
	sandboxWith(t, clients.Claude, clients.Codex)
	_ = os.MkdirAll(filepath.Dir(SettingsPath()), 0o755)
	_ = os.WriteFile(SettingsPath(), []byte("clients: [claude\n"), 0o644)
	_, err := Install(&registry.Entry{ID: "hi", Command: "sh"}, Values{}, InstallOptions{})
	if err == nil || !strings.Contains(err.Error(), "config.yaml") {
		t.Fatalf("expected an error naming the file, got %v", err)
	}
	if clients.Detect()["hi"] != nil {
		t.Fatal("it was installed anyway")
	}
	// --client doesn't depend on the settings.
	if _, err := Install(&registry.Entry{ID: "hi", Command: "sh"}, Values{}, InstallOptions{Clients: []string{clients.Codex}}); err != nil {
		t.Fatal(err)
	}
}

func TestNeedsClientChoice(t *testing.T) {
	sandboxWith(t, clients.Claude)
	if NeedsClientChoice() {
		t.Fatal("with one client there's nothing to choose")
	}
	sandboxWith(t, clients.Claude, clients.Antigravity)
	if !NeedsClientChoice() {
		t.Fatal("two clients and no choice yet: should ask")
	}
	if got := Unused(); len(got) != 0 {
		t.Fatalf("nothing chosen, so nothing is left out: %v", got)
	}
	_ = os.MkdirAll(filepath.Dir(SettingsPath()), 0o755)
	_ = os.WriteFile(SettingsPath(), []byte("clients: [claude\n"), 0o644)
	if NeedsClientChoice() {
		t.Fatal("a broken file is for Install to report, not to replace by asking")
	}
	_ = SaveClients([]string{clients.Claude})
	if NeedsClientChoice() {
		t.Fatal("chosen already: shouldn't ask again")
	}
	if got := Unused(); !slices.Equal(got, []string{clients.Antigravity}) {
		t.Fatalf("unused: got %v, want antigravity", got)
	}
}
