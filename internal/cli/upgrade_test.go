package cli

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
)

// fakeNPM serves the newest version of every npm package as version.
func fakeNPM(t *testing.T, version string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/latest") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"version":"` + version + `"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("npm_config_registry", srv.URL)
}

func TestAddPinsPackages(t *testing.T) {
	sandbox(t)
	fakeNPM(t, "1.4.2")
	if err := runAdd([]string{"notes", "--npx", "@acme/notes", "--no-check"}); err != nil {
		t.Fatal(err)
	}
	if err := runAdd([]string{"loose", "--npx", "@acme/loose", "--no-check", "--no-pin"}); err != nil {
		t.Fatal(err)
	}
	reg, _ := registry.Load()
	if got := reg.Get("notes").Args; !slices.Equal(got, []string{"-y", "@acme/notes@1.4.2"}) {
		t.Fatalf("add should pin, got %v", got)
	}
	if got := reg.Get("loose").Args; !slices.Equal(got, []string{"-y", "@acme/loose"}) {
		t.Fatalf("--no-pin should leave it alone, got %v", got)
	}
}

func TestUpgradeMovesThePinAndReinstalls(t *testing.T) {
	sandbox(t)
	fakeNPM(t, "2.0.0")
	_ = registry.AddUserEntry(registry.Entry{ID: "notes", Command: "npx", Args: []string{"-y", "@acme/notes@1.0.0"}})
	_ = registry.AddUserEntry(registry.Entry{ID: "plain", Command: "sh"})
	if err := clients.Register(clients.Claude, "notes", clients.Config{Type: "stdio", Command: "npx", Args: []string{"-y", "@acme/notes@1.0.0"}}, "user"); err != nil {
		t.Fatal(err)
	}

	if err := runUpgrade([]string{"--dry-run"}); err != nil {
		t.Fatal(err)
	}
	reg, _ := registry.Load()
	if reg.Get("notes").Args[1] != "@acme/notes@1.0.0" {
		t.Fatal("--dry-run must not change anything")
	}

	if err := runUpgrade(nil); err != nil {
		t.Fatal(err)
	}
	reg, _ = registry.Load()
	if got := reg.Get("notes").Args[1]; got != "@acme/notes@2.0.0" {
		t.Fatalf("registry not upgraded: %s", got)
	}
	if got := clients.Detect()["notes"].Config().Args; !slices.Equal(got, []string{"-y", "@acme/notes@2.0.0"}) {
		t.Fatalf("installed tool not re-registered: %v", got)
	}
	if err := runUpgrade([]string{"plain"}); err == nil {
		t.Fatal("upgrading a tool without a package should fail")
	}
}

// If the registry changed what an installed tool runs, update shows it and
// needs -y (or a yes at the prompt) before re-registering.
func TestUpdateReviewsChanges(t *testing.T) {
	sandbox(t)
	_ = registry.AddUserEntry(registry.Entry{ID: "notes", Command: "sh", Args: []string{"-c", "evil"}})
	if err := clients.Register(clients.Claude, "notes", clients.Config{Type: "stdio", Command: "sh"}, "user"); err != nil {
		t.Fatal(err)
	}
	err := runUpdate([]string{"notes"})
	if err == nil || !strings.Contains(err.Error(), "-y") {
		t.Fatalf("update without a terminal should refuse a change, got %v", err)
	}
	if got := clients.Detect()["notes"].Config().Args; len(got) != 0 {
		t.Fatalf("refused update still changed the registration: %v", got)
	}
	if err := runInstall([]string{"notes", "-y"}); err != nil {
		t.Fatal(err)
	}
	if got := clients.Detect()["notes"].Config().Args; !slices.Equal(got, []string{"-c", "evil"}) {
		t.Fatalf("install -y should accept the change, got %v", got)
	}
	if err := runUpdate([]string{"notes"}); err != nil {
		t.Fatalf("nothing changed now, update should just work: %v", err)
	}
}

func TestMCPRefusesChangedTools(t *testing.T) {
	cs := session(t)
	_ = registry.AddUserEntry(registry.Entry{ID: "notes", Command: "sh", Args: []string{"-c", "evil"}})
	if err := clients.Register(clients.Claude, "notes", clients.Config{Type: "stdio", Command: "sh"}, "user"); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"install_server", "inspect_server"} {
		out, isErr := callTool(t, cs, tool, map[string]any{"id": "notes"})
		if !isErr || !strings.Contains(out, "toolshed update notes") {
			t.Fatalf("%s should refuse a changed tool: %s", tool, out)
		}
	}
}
