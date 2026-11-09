package shed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/pkgs"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/secrets"
)

// Problem levels: a Warning still works but needs attention, an Error means
// the tool won't work (or isn't what you approved) until it's fixed.
const (
	Warning = "warning"
	Error   = "error"
)

// Finding is one problem doctor found, with what to do about it.
type Finding struct {
	Level string `json:"level"`
	Text  string `json:"text"`
	Fix   string `json:"fix,omitempty"` // a command to run
}

// Health is doctor's report on one installed tool.
type Health struct {
	ID       string    `json:"id"`
	Clients  []string  `json:"clients"`
	Tools    int       `json:"tools"` // tools the server offers; -1 when it wasn't started
	Findings []Finding `json:"findings"`
}

// OK reports whether nothing at all was found.
func (h Health) OK() bool { return len(h.Findings) == 0 }

// HasErrors reports whether any finding is an Error.
func (h Health) HasErrors() bool {
	for _, f := range h.Findings {
		if f.Level == Error {
			return true
		}
	}
	return false
}

// DoctorOptions controls Doctor.
type DoctorOptions struct {
	Start    bool // start every server and list its tools (slow for npx/uvx)
	Parallel int  // servers started at once; default 4
}

// Doctor checks every installed tool in the registry:
//
//   - the registry entry still matches what the clients run (Changes);
//   - a `toolshed exec` wrapper points at a toolshed binary that exists;
//   - the command is on PATH, and its secrets are in the keyring;
//   - npx/uvx packages are pinned;
//   - with opts.Start, the server starts and lists its tools.
//
// Tools whose entry changed aren't started: that would run a command the
// user hasn't accepted.
func Doctor(ctx context.Context, reg *registry.Registry, installed map[string]*clients.Installation, opts DoctorOptions) []Health {
	var out []Health
	for _, e := range reg.Entries {
		if inst := installed[e.ID]; inst != nil {
			out = append(out, check(e, inst))
		}
	}
	if !opts.Start {
		return out
	}
	if opts.Parallel <= 0 {
		opts.Parallel = 4
	}
	sem := make(chan struct{}, opts.Parallel)
	var wg sync.WaitGroup
	for i := range out {
		h := &out[i]
		e := reg.Get(h.ID)
		if h.HasErrors() {
			continue // changed, or missing a command or secret: starting it tells us nothing new
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			tools, err := Probe(ctx, e)
			if err != nil {
				h.Findings = append(h.Findings, Finding{Level: Error, Text: "doesn't start: " + err.Error(), Fix: "toolshed tools " + e.ID})
				return
			}
			h.Tools = len(tools)
		}()
	}
	wg.Wait()
	return out
}

func check(e *registry.Entry, inst *clients.Installation) Health {
	h := Health{ID: e.ID, Clients: inst.Clients(), Tools: -1}
	add := func(level, text, fix string) { h.Findings = append(h.Findings, Finding{level, text, fix}) }

	if changes := Changes(e, inst); len(changes) > 0 {
		for _, c := range changes {
			add(Error, "changed in the registry since it was installed: "+c, "toolshed update "+e.ID)
		}
		return h // the rest describes the new entry, which isn't what's installed
	}
	for _, cfg := range registrations(inst) {
		self := ""
		if _, _, wrapped := Unwrap(cfg); wrapped {
			self = cfg.Command
		} else if c, ok := UnwrapHeaders(cfg.HeadersHelper); ok {
			self = c.Self
		}
		if self != "" && filepath.IsAbs(self) {
			if _, err := os.Stat(self); err != nil {
				add(Error, fmt.Sprintf("registered with toolshed at %s, which is gone", self), "toolshed update "+e.ID)
				break
			}
		}
	}
	if err := CheckCommand(e); err != nil {
		add(Error, fmt.Sprintf("%s isn't on PATH", e.Command), "")
	}
	if secrets.Available() {
		for _, in := range e.Inputs {
			if !in.Secret || in.Optional {
				continue
			}
			if v, err := secrets.Get(e.SecretGroup(in), in.Key); err != nil || v == "" {
				text := in.Key + " isn't in your keyring"
				if err != nil && !errors.Is(err, secrets.ErrNotFound) {
					text = fmt.Sprintf("can't read %s from your keyring: %v", in.Key, err)
				}
				add(Error, text, "toolshed install "+e.ID)
			}
		}
	}
	if r, ok := pkgs.Find(e); ok && !r.Exact {
		add(Warning, fmt.Sprintf("%s isn't pinned to a version, so it runs whatever is newest", r.Name), "toolshed upgrade "+e.ID)
	}
	return h
}

func registrations(inst *clients.Installation) []clients.Config {
	var cfgs []clients.Config
	if inst.ClaudeScope != "" {
		cfgs = append(cfgs, inst.Config)
	}
	if inst.Codex {
		cfgs = append(cfgs, inst.CodexConfig)
	}
	return cfgs
}

// Orphans returns servers registered through `toolshed exec` that are no
// longer in the registry (deleted or renamed there): they still work, but
// toolshed can't update or uninstall them by name anymore.
func Orphans(reg *registry.Registry, installed map[string]*clients.Installation) []string {
	var out []string
	for name, inst := range installed {
		if reg.Get(name) != nil {
			continue
		}
		for _, cfg := range registrations(inst) {
			_, _, wrapped := Unwrap(cfg)
			_, helper := UnwrapHeaders(cfg.HeadersHelper)
			if wrapped || helper {
				out = append(out, name)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}
