package shed

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/secrets"
)

func TestDoctorFindings(t *testing.T) {
	secrets.UseMockForTest(true)
	_ = secrets.Set("ok", "TOKEN", "x")
	stdio := func(cmd string, args ...string) *clients.Installation {
		return &clients.Installation{ClaudeScope: "user", Config: clients.Config{Type: "stdio", Command: cmd, Args: args}}
	}
	wrap := func(self, id, cmd string) *clients.Installation {
		return stdio(self, "exec", id, "--secret", "TOKEN="+id, "--", cmd)
	}
	reg := regOf(
		&registry.Entry{ID: "ok", Command: "sh", Inputs: []registry.Input{{Key: "TOKEN", Secret: true}}},
		&registry.Entry{ID: "changed", Command: "sh", Args: []string{"-c", "evil"}},
		&registry.Entry{ID: "nocmd", Command: "definitely-not-a-command-xyz"},
		&registry.Entry{ID: "nosecret", Command: "sh", Inputs: []registry.Input{{Key: "TOKEN", Secret: true}}},
		&registry.Entry{ID: "moved", Command: "sh", Inputs: []registry.Input{{Key: "TOKEN", Secret: true}}},
		&registry.Entry{ID: "loose", Command: "npx", Args: []string{"-y", "pkg"}},
		&registry.Entry{ID: "notinstalled", Command: "sh"},
	)
	_ = secrets.Set("moved", "TOKEN", "x")
	self := mustSelf(t)
	installed := map[string]*clients.Installation{
		"ok":       wrap(self, "ok", "sh"),
		"changed":  stdio("sh"),
		"nocmd":    stdio("definitely-not-a-command-xyz"),
		"nosecret": wrap(self, "nosecret", "sh"),
		"moved":    wrap("/gone/bin/toolshed", "moved", "sh"),
		"loose":    stdio("npx", "-y", "pkg"),
		"orphan":   wrap(self, "orphan", "sh"),
		"manual":   stdio("other-server"),
	}

	got := map[string]Health{}
	for _, h := range Doctor(context.Background(), reg, installed, DoctorOptions{}) {
		got[h.ID] = h
	}
	if _, ok := got["notinstalled"]; ok {
		t.Error("doctor should only report installed tools")
	}
	if h := got["ok"]; !h.OK() {
		t.Errorf("ok: unexpected findings %+v", h.Findings)
	}
	want := map[string]struct{ level, text string }{
		"changed":  {Error, "changed in the registry"},
		"nocmd":    {Error, "isn't on PATH"},
		"nosecret": {Error, "TOKEN isn't in your keyring"},
		"moved":    {Error, "/gone/bin/toolshed, which is gone"},
		"loose":    {Warning, "isn't pinned"},
	}
	for id, w := range want {
		h := got[id]
		if len(h.Findings) == 0 || h.Findings[0].Level != w.level || !strings.Contains(h.Findings[0].Text, w.text) {
			t.Errorf("%s: want %s %q, got %+v", id, w.level, w.text, h.Findings)
		}
	}
	if got["loose"].HasErrors() {
		t.Error("an unpinned package is a warning, not an error")
	}
	if o := Orphans(reg, installed); len(o) != 1 || o[0] != "orphan" {
		t.Errorf("orphans = %v, want [orphan] (manual isn't toolshed's)", o)
	}
}

func regOf(entries ...*registry.Entry) *registry.Registry {
	return &registry.Registry{Entries: entries}
}

// mustSelf is a stand-in toolshed binary for registrations to point at.
func mustSelf(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "toolshed")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}
