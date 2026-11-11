package tui

import (
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/secrets"
)

// These tests drive whole TUI flows with key presses and check what they
// did: the registry, the clients' config (through the mock claude CLI and a
// temporary Codex config), and the mock keyring. Checking that a form opens
// isn't enough: confirm forms once ignored every answer, and tests that only
// looked at the screen passed.

func sandboxTUI(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("TOOLSHED_HOME", filepath.Join(home, ".toolshed"))
	t.Setenv("TOOLSHED_REGISTRY", "")
	mocks, _ := filepath.Abs("../../test/mocks")
	t.Setenv("PATH", mocks+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(home)
	secrets.UseMockForTest(true)
	oldSpin, oldFlash := spinnerInterval, flashDuration
	spinnerInterval, flashDuration = time.Millisecond, time.Millisecond
	t.Cleanup(func() { spinnerInterval, flashDuration = oldSpin, oldFlash })
}

// start opens the TUI on the sandbox, the way Run does.
func start(t *testing.T) tea.Model {
	t.Helper()
	var m tea.Model = newModel()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m, _ = m.Update(load())
	if mm := m.(model); mm.mode != modeBrowse {
		t.Fatalf("TUI didn't load: mode %v %s", mm.mode, mm.fatal)
	}
	return m
}

func key(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

var (
	enter = tea.KeyPressMsg{Code: tea.KeyEnter}
	left  = tea.KeyPressMsg{Code: tea.KeyLeft}
	down  = tea.KeyPressMsg{Code: tea.KeyDown}
	space = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
)

func registerBoth(t *testing.T, name string, cfg clients.Config) {
	t.Helper()
	for _, c := range clients.All {
		if err := clients.Register(c, name, cfg, "user"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTUIUninstall(t *testing.T) {
	sandboxTUI(t)
	_ = registry.AddUserEntry(registry.Entry{ID: "hi", Command: "sh"})
	registerBoth(t, "hi", clients.Config{Type: "stdio", Command: "sh"})
	m := start(t)
	m = press(t, m, key('u'), enter) // the default is No
	if clients.Detect()["hi"] == nil {
		t.Fatal("No uninstalled it")
	}
	press(t, m, key('u'), left, enter)
	if inst := clients.Detect()["hi"]; inst != nil {
		t.Fatalf("still installed after Yes: %v", inst.Clients())
	}
	if reg, _ := registry.Load(); reg.Get("hi") == nil {
		t.Fatal("uninstall must keep it in the registry")
	}
}

func TestTUIDelete(t *testing.T) {
	sandboxTUI(t)
	_ = registry.AddUserEntry(registry.Entry{ID: "hi", Command: "sh"})
	registerBoth(t, "hi", clients.Config{Type: "stdio", Command: "sh"})
	m := start(t)
	m = press(t, m, key('d'), enter)
	if reg, _ := registry.Load(); reg.Get("hi") == nil {
		t.Fatal("No deleted it")
	}
	press(t, m, key('d'), left, enter)
	if reg, _ := registry.Load(); reg.Get("hi") != nil {
		t.Fatal("still in the registry after Yes")
	}
	if clients.Detect()["hi"] != nil {
		t.Fatal("delete should uninstall it too")
	}
}

func TestTUIApproveThenInstall(t *testing.T) {
	sandboxTUI(t)
	_ = registry.AddUserEntry(registry.Entry{ID: "agentic", Command: "sh", Pending: true})
	m := start(t)
	m = press(t, m, key('i'), left, enter)
	if reg, _ := registry.Load(); reg.Get("agentic").Pending {
		t.Fatal("Yes didn't approve it")
	}
	m = press(t, m, key('i')) // nothing to ask (no inputs, not a git repo): installs straight away
	inst := clients.Detect()["agentic"]
	if inst == nil || !slices.Equal(inst.Clients(), []string{clients.Claude, clients.Codex}) {
		t.Fatalf("not installed in both clients: %+v", inst)
	}
	_ = m
}

// In a git repo the install form asks where to install, for every tool:
// "This repo only" puts it in the repo's config of each client it supports.
func TestTUIInstallForThisRepo(t *testing.T) {
	for name, tc := range map[string]struct {
		only []string
		want map[string]string // client -> scope
	}{
		"both clients": {nil, map[string]string{clients.Claude: "project", clients.Codex: "project"}},
		"codex only":   {[]string{clients.Codex}, map[string]string{clients.Codex: "project"}},
	} {
		t.Run(name, func(t *testing.T) {
			sandboxTUI(t)
			// The repo can't be HOME: ~/.codex/config.toml would be its .codex/config.toml.
			repo := filepath.Join(os.Getenv("HOME"), "repo")
			if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
				t.Fatalf("git init: %v %s", err, out)
			}
			t.Chdir(repo)
			_ = registry.AddUserEntry(registry.Entry{ID: "hi", Command: "sh", Clients: tc.only})
			press(t, start(t), key('i'), down, enter) // Install for: This repo only
			inst := clients.Detect()["hi"]
			if inst == nil {
				t.Fatal("not installed")
			}
			got := map[string]string{}
			for _, c := range inst.Clients() {
				got[c] = inst.Scope(c)
			}
			if !maps.Equal(got, tc.want) {
				t.Fatalf("installed at %v, want %v", got, tc.want)
			}
		})
	}
}

// Reinstalling keeps a tool where it is: accepting the form's default must
// not add a copy at another scope, which Detect wouldn't show and uninstall
// wouldn't remove.
func TestTUIReinstallKeepsScopes(t *testing.T) {
	for name, tc := range map[string]struct {
		at   map[string]string // client -> scope before; empty: not installed
		want map[string]string // client -> scopes found in the config files after
	}{
		"in this repo":   {map[string]string{clients.Claude: "project", clients.Codex: "project"}, map[string]string{clients.Claude: "project", clients.Codex: "project"}},
		"all projects":   {map[string]string{clients.Claude: "user", clients.Codex: "user"}, map[string]string{clients.Claude: "user", clients.Codex: "user"}},
		"local and user": {map[string]string{clients.Claude: "local", clients.Codex: "user"}, map[string]string{clients.Claude: "local", clients.Codex: "user"}},
		"not installed":  {nil, map[string]string{clients.Claude: "user", clients.Codex: "user"}},
	} {
		t.Run(name, func(t *testing.T) {
			sandboxTUI(t)
			repo := filepath.Join(os.Getenv("HOME"), "repo") // not HOME: see TestTUIInstallForThisRepo
			if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
				t.Fatalf("git init: %v %s", err, out)
			}
			t.Chdir(repo)
			_ = registry.AddUserEntry(registry.Entry{ID: "hi", Command: "sh"})
			for c, scope := range tc.at {
				if err := clients.Register(c, "hi", clients.Config{Type: "stdio", Command: "sh"}, scope); err != nil {
					t.Fatal(err)
				}
			}
			press(t, start(t), key('i'), enter) // accept the default
			if got := placements(t, "hi", repo); !maps.Equal(got, tc.want) {
				t.Fatalf("registered at %v, want %v", got, tc.want)
			}
		})
	}
}

// placements reads every scope's config file for name, so a second copy
// shows up as "user+project" instead of being hidden like Detect hides it.
func placements(t *testing.T, name, repo string) map[string]string {
	t.Helper()
	home := os.Getenv("HOME")
	found := map[string][]string{}
	var claude struct {
		MCPServers map[string]any `json:"mcpServers"`
		Projects   map[string]struct {
			MCPServers map[string]any `json:"mcpServers"`
		} `json:"projects"`
	}
	readJSONFile(t, filepath.Join(home, ".claude.json"), &claude)
	if _, ok := claude.MCPServers[name]; ok {
		found[clients.Claude] = append(found[clients.Claude], "user")
	}
	for _, p := range claude.Projects {
		if _, ok := p.MCPServers[name]; ok {
			found[clients.Claude] = append(found[clients.Claude], "local")
		}
	}
	var mcp struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	readJSONFile(t, filepath.Join(repo, ".mcp.json"), &mcp)
	if _, ok := mcp.MCPServers[name]; ok {
		found[clients.Claude] = append(found[clients.Claude], "project")
	}
	for scope, path := range map[string]string{"user": filepath.Join(home, ".codex", "config.toml"), "project": filepath.Join(repo, ".codex", "config.toml")} {
		if data, _ := os.ReadFile(path); strings.Contains(string(data), "[mcp_servers."+name+"]") {
			found[clients.Codex] = append(found[clients.Codex], scope)
		}
	}
	out := map[string]string{}
	for c, scopes := range found {
		slices.Sort(scopes)
		out[c] = strings.Join(scopes, "+")
	}
	return out
}

func readJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return
	}
	if err != nil || json.Unmarshal(data, v) != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
}

func TestTUIReviewChange(t *testing.T) {
	sandboxTUI(t)
	_ = registry.AddUserEntry(registry.Entry{ID: "hi", Command: "sh", Args: []string{"-c", "new"}})
	registerBoth(t, "hi", clients.Config{Type: "stdio", Command: "sh"})
	m := start(t)
	m = press(t, m, key('i'), enter) // No: keep what's installed
	if got := clients.Detect()["hi"].Config().Args; len(got) != 0 {
		t.Fatalf("No changed the registration: %v", got)
	}
	press(t, m, key('i'), left, enter)
	inst := clients.Detect()["hi"]
	if claude, codex := inst.Regs[clients.Claude].Config.Args, inst.Regs[clients.Codex].Config.Args; !slices.Equal(claude, []string{"-c", "new"}) || !slices.Equal(codex, []string{"-c", "new"}) {
		t.Fatalf("Yes didn't install the new version: claude %v, codex %v", claude, codex)
	}
}

func TestTUIImportUsesTheTicks(t *testing.T) {
	sandboxTUI(t)
	registerBoth(t, "byhand", clients.Config{Type: "stdio", Command: "sh",
		Env: map[string]string{"API_TOKEN": "sk-live-abcdefghijkl", "LEVEL": "debug"}})
	m := start(t)
	// API_TOKEN (first) is ticked by its name: untick it, tick LEVEL, import.
	press(t, m, key('m'), enter, space, down, space, enter)
	reg, _ := registry.Load()
	e := reg.Get("byhand")
	if e == nil {
		t.Fatal("not imported")
	}
	if !slices.Equal(e.SecretKeys(), []string{"LEVEL"}) {
		t.Fatalf("the ticks weren't used: secrets %v", e.SecretKeys())
	}
	if v, _ := secrets.Get("byhand", "LEVEL"); v != "debug" {
		t.Fatalf("LEVEL should be in the keyring, got %q", v)
	}
	env := clients.Detect()["byhand"].Config().Env
	if env["API_TOKEN"] != "sk-live-abcdefghijkl" || env["LEVEL"] != "" {
		t.Fatalf("claude's env should keep API_TOKEN and lose LEVEL: %v", env)
	}
}

// Bubble Tea copies the model on every update, so a form must never write
// into a field of it (huh's Value(&m.x)): the answer would land in a copy
// that's already been thrown away. Bind forms to pointers the model holds.
func TestNoFormBindsToModelFields(t *testing.T) {
	bad := regexp.MustCompile(`\bValue\(&m\.`)
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, _ := os.ReadFile(f)
		for i, line := range strings.Split(string(data), "\n") {
			if bad.MatchString(line) {
				t.Errorf("%s:%d binds a form to a field of the model: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}
