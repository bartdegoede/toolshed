// Package tui is toolshed's interactive terminal UI: a split-pane list of
// tools with inline add / install / uninstall / probe actions.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/pkgs"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/shed"
)

// Version is shown in the header; set by the CLI.
var Version = "dev"

// Run starts the TUI.
func Run() error {
	_, err := tea.NewProgram(newModel()).Run()
	return err
}

type mode int

const (
	modeLoading mode = iota
	modeFatal        // registry broken or no client installed
	modeBrowse
	modeForm
	modeBusy
	modeImport // the import screen (see importview.go)
)

type formKind int

const (
	formAdd formKind = iota
	formInstall
	formUninstall
	formDelete
	formApprove
	formReview // an installed tool changed in the registry: accept before re-installing
	formImport // which values of a server being imported are secrets
)

type probeState struct {
	running bool
	tools   []shed.Tool
	err     error
}

type model struct {
	mode    mode
	fatal   string
	reg     *registry.Registry
	inst    map[string]*clients.Installation
	avail   []string
	cursor  int
	width   int
	height  int
	spinner int
	busy    string
	flash   string
	flashOK bool
	flashID int
	probes  map[string]*probeState
	justAdd string // ID of a just-added tool, for the post-probe hint

	form     *huh.Form
	formKind formKind
	addSpec  *addSpec
	install  *installForm
	// Form answers are pointers: Bubble Tea copies the model on every
	// update, so a form bound to a field of the model would write into a
	// copy that's already been thrown away.
	confirm *bool

	imports    []shed.Import // servers the clients have that aren't in the toolshed
	impCursor  int
	impSecrets *[]string // keys ticked as secrets in the import form
	inImport   bool     // the import screen is open (also while its form or import runs)
}

// ── messages ──

type loadedMsg struct {
	reg   *registry.Registry
	inst  map[string]*clients.Installation
	avail []string
	err   error
}
type detectedMsg struct {
	inst map[string]*clients.Installation
}
type doneMsg struct {
	text string
	err  error
}
type probeMsg struct {
	id    string
	tools []shed.Tool
	err   error
}
type tickMsg struct{}
type flashClearMsg struct{ id int }

func newModel() model {
	return model{probes: map[string]*probeState{}, inst: map[string]*clients.Installation{}}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(load, tick())
}

func load() tea.Msg {
	reg, err := registry.Load()
	return loadedMsg{reg: reg, err: err, inst: clients.Detect(), avail: clients.AvailableClients()}
}

func detect() tea.Msg { return detectedMsg{clients.Detect()} }

func tick() tea.Cmd {
	return tea.Tick(spinnerInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *model) setFlash(text string, ok bool) tea.Cmd {
	m.flashID++
	m.flash, m.flashOK = text, ok
	id := m.flashID
	return tea.Tick(flashDuration, func(time.Time) tea.Msg { return flashClearMsg{id} })
}

func (m model) selected() *registry.Entry {
	if m.reg == nil || m.cursor < 0 || m.cursor >= len(m.reg.Entries) {
		return nil
	}
	return m.reg.Entries[m.cursor]
}

func (m model) animating() bool {
	if m.mode == modeLoading || m.mode == modeBusy {
		return true
	}
	for _, p := range m.probes {
		if p.running {
			return true
		}
	}
	return false
}

// ── update ──

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.form != nil {
			m.form = m.form.WithWidth(m.detailWidth())
		}
	case tickMsg:
		if m.animating() {
			m.spinner = (m.spinner + 1) % len(spinnerFrames)
			return m, tick()
		}
		return m, nil
	case flashClearMsg:
		if msg.id == m.flashID {
			m.flash = ""
		}
		return m, nil
	case loadedMsg:
		switch {
		case msg.err != nil:
			m.mode, m.fatal = modeFatal, msg.err.Error()
		case len(msg.avail) == 0:
			m.mode, m.fatal = modeFatal, noClientsText
		default:
			m.mode, m.reg, m.inst, m.avail = modeBrowse, msg.reg, msg.inst, msg.avail
			m.cursor = max(0, min(m.cursor, len(m.reg.Entries)-1))
			m.refreshImports()
			if m.inImport {
				m.mode = modeImport
				if len(m.imports) == 0 {
					m.mode, m.inImport = modeBrowse, false
				}
			}
		}
		return m, nil
	case detectedMsg:
		m.inst = msg.inst
		m.refreshImports()
		return m, nil
	case importedMsg:
		if msg.err != nil {
			m.mode = modeImport
			return m, m.setFlash("✗ "+oneLine(msg.err.Error()), false)
		}
		return m, tea.Batch(load, m.setFlash("✓ "+msg.text, true))
	case doneMsg:
		m.mode = modeBrowse
		if msg.err != nil {
			return m, tea.Batch(detect, m.setFlash("✗ "+oneLine(msg.err.Error()), false))
		}
		return m, tea.Batch(detect, m.setFlash("✓ "+msg.text, true))
	case probeMsg:
		m.probes[msg.id] = &probeState{tools: msg.tools, err: msg.err}
		if msg.id == m.justAdd {
			m.justAdd = ""
			if msg.err == nil {
				return m, m.setFlash(fmt.Sprintf("✓ %d tools found · press i to install", len(msg.tools)), true)
			}
			return m, m.setFlash("✗ added, but the server didn't respond: "+oneLine(msg.err.Error()), false)
		}
		return m, nil
	}

	switch m.mode {
	case modeForm:
		return m.updateForm(msg)
	case modeFatal:
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return m, tea.Quit
		}
	case modeLoading, modeBusy:
		if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case modeBrowse:
		if k, ok := msg.(tea.KeyPressMsg); ok {
			return m.handleKey(k.String())
		}
	case modeImport:
		if k, ok := msg.(tea.KeyPressMsg); ok {
			return m.handleImportKey(k.String())
		}
	}
	return m, nil
}

func (m model) handleKey(key string) (tea.Model, tea.Cmd) {
	e := m.selected()
	switch key {
	case "q", "ctrl+c", "esc":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.reg != nil && m.cursor < len(m.reg.Entries)-1 {
			m.cursor++
		}
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = len(m.reg.Entries) - 1
	case "m":
		return m.openImports()
	case "a":
		m.addSpec = &addSpec{}
		return m.openForm(formAdd, newAddForm(m.addSpec, m.reg, m.detailWidth()))
	case "t", "i", "enter":
		if e == nil {
			return m, nil
		}
		if e.Pending { // agent-added: show what it runs before anything starts it
			m.confirm = new(bool)
			desc := "Your agent added it. Approving lets toolshed run:\n" + e.CommandLine()
			if keys := e.SecretKeys(); len(keys) > 0 {
				desc += "\nwith these secrets from your keyring: " + strings.Join(keys, ", ")
			}
			return m.openForm(formApprove, newConfirmForm("Approve "+e.DisplayName()+"?", desc, m.confirm, m.detailWidth()))
		}
		if key == "t" {
			return m, m.startProbe(e)
		}
		if changes := shed.Changes(e, m.inst[e.ID]); len(changes) > 0 {
			m.confirm = new(bool)
			desc := "Since you installed it, it now:\n• " + strings.Join(changes, "\n• ")
			if keys := e.SecretKeys(); len(keys) > 0 {
				desc += "\nand it gets " + strings.Join(keys, ", ") + " from your keyring."
			}
			return m.openForm(formReview, newConfirmForm(e.DisplayName()+" changed. Install the new version?", desc, m.confirm, m.detailWidth()))
		}
		return m.openInstall(e)
	case "u":
		if e != nil && m.inst[e.ID] != nil {
			m.confirm = new(bool)
			return m.openForm(formUninstall, newConfirmForm("Uninstall "+e.DisplayName()+"?",
				"Removes it from "+strings.Join(m.inst[e.ID].Clients(), " and ")+". Saved secrets are kept.", m.confirm, m.detailWidth()))
		}
	case "d":
		if e != nil && e.Source == registry.SourceUser {
			m.confirm = new(bool)
			desc := "Removes it from " + m.reg.UserPath
			if m.inst[e.ID] != nil {
				desc += " and uninstalls it"
			}
			return m.openForm(formDelete, newConfirmForm("Delete "+e.DisplayName()+"?", desc+".", m.confirm, m.detailWidth()))
		}
	}
	return m, nil
}

// confirmed reports whether the user said yes in the last confirm form.
func (m model) confirmed() bool { return m.confirm != nil && *m.confirm }

func (m model) openForm(kind formKind, form *huh.Form) (tea.Model, tea.Cmd) {
	m.mode, m.formKind, m.form, m.flash = modeForm, kind, form, ""
	return m, form.Init()
}

func (m model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	back := modeBrowse
	if m.inImport {
		back = modeImport
	}
	if k, ok := msg.(tea.KeyPressMsg); ok && (k.String() == "esc" || k.String() == "ctrl+c") {
		m.mode, m.form = back, nil
		return m, nil
	}
	fm, cmd := m.form.Update(msg)
	if f, ok := fm.(*huh.Form); ok {
		m.form = f
	}
	switch m.form.State {
	case huh.StateAborted:
		m.mode, m.form = back, nil
		return m, nil
	case huh.StateCompleted:
		m.mode, m.form = back, nil
		return m.submitForm()
	}
	return m, cmd
}

func (m model) submitForm() (tea.Model, tea.Cmd) {
	e := m.selected()
	switch m.formKind {
	case formImport:
		return m.submitImport()
	case formAdd:
		entry, err := m.addSpec.toEntry()
		pinned := ""
		if err == nil {
			if v, perr := pkgs.PinLatest(context.Background(), &entry); perr == nil {
				pinned = " · pinned to " + v
			}
			err = registry.AddUserEntry(entry)
		}
		if err != nil {
			return m, m.setFlash("✗ "+oneLine(err.Error()), false)
		}
		reg, err := registry.Load()
		if err != nil {
			return m, m.setFlash("✗ "+oneLine(err.Error()), false)
		}
		m.reg = reg
		for i, x := range reg.Entries {
			if x.ID == entry.ID {
				m.cursor = i
			}
		}
		m.justAdd = entry.ID
		added := m.reg.Get(entry.ID)
		return m, tea.Batch(m.setFlash("✓ Added "+entry.ID+pinned+" · probing for tools…", true), m.startProbe(added))
	case formInstall:
		return m.startInstall(e)
	case formReview:
		if !m.confirmed() || e == nil {
			return m, nil
		}
		return m.openInstall(e)
	case formApprove:
		if !m.confirmed() || e == nil {
			return m, nil
		}
		if err := registry.ApproveUserEntry(e.ID); err != nil {
			return m, m.setFlash("✗ "+oneLine(err.Error()), false)
		}
		reg, err := registry.Load()
		if err != nil {
			return m, m.setFlash("✗ "+oneLine(err.Error()), false)
		}
		m.reg = reg
		return m, m.setFlash("✓ Approved "+e.ID+" · press i to install, t to see its tools", true)
	case formUninstall:
		if !m.confirmed() || e == nil {
			return m, nil
		}
		reg, inst := m.reg, m.inst[e.ID]
		return m.runBusy("Uninstalling "+e.DisplayName(), func() doneMsg {
			return doneMsg{"Uninstalled " + e.ID, shed.Uninstall(reg, e, inst, false)}
		})
	case formDelete:
		if !m.confirmed() || e == nil {
			return m, nil
		}
		reg, inst := m.reg, m.inst[e.ID]
		if m.cursor > 0 && m.cursor == len(m.reg.Entries)-1 {
			m.cursor--
		}
		model, cmd := m.runBusy("Deleting "+e.DisplayName(), func() doneMsg {
			if inst != nil {
				if err := shed.Uninstall(reg, e, inst, true); err != nil {
					return doneMsg{err: err}
				}
			}
			return doneMsg{"Deleted " + e.ID, registry.RemoveUserEntry(e.ID)}
		})
		return model, tea.Sequence(cmd, func() tea.Msg { return load() })
	}
	return m, nil
}

// openInstall asks for the tool's inputs, or installs straight away when it
// has none.
func (m model) openInstall(e *registry.Entry) (tea.Model, tea.Cmd) {
	vals, _ := shed.Resolve(e, m.inst[e.ID], nil)
	form, st := newInstallForm(e, vals, m.detailWidth())
	m.install = st
	if form == nil {
		return m.startInstall(e)
	}
	return m.openForm(formInstall, form)
}

func (m model) startInstall(e *registry.Entry) (tea.Model, tea.Cmd) {
	if e == nil {
		return m, nil
	}
	vals, scope := shed.Values{}, "user"
	if m.install != nil {
		vals, scope = m.install.collected(), m.install.scope
	}
	// Keep previously stored/installed values the form didn't change.
	resolved, _ := shed.Resolve(e, m.inst[e.ID], vals)
	return m.runBusy("Installing "+e.DisplayName(), func() doneMsg {
		res, err := shed.Install(e, resolved, shed.InstallOptions{Scope: scope})
		if err != nil {
			return doneMsg{err: err}
		}
		text := "Installed " + e.ID + " → " + strings.Join(res.Clients, ", ")
		if len(res.Notes) > 0 {
			text += " · " + res.Notes[0]
		}
		return doneMsg{text: text}
	})
}

func (m model) runBusy(label string, fn func() doneMsg) (tea.Model, tea.Cmd) {
	m.mode, m.busy = modeBusy, label
	return m, tea.Batch(func() tea.Msg { return fn() }, tick())
}

func (m *model) startProbe(e *registry.Entry) tea.Cmd {
	if p := m.probes[e.ID]; p != nil && p.running {
		return nil
	}
	m.probes[e.ID] = &probeState{running: true}
	entry := e
	return tea.Batch(tick(), func() tea.Msg {
		tools, err := shed.Probe(context.Background(), entry)
		return probeMsg{id: entry.ID, tools: tools, err: err}
	})
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

const noClientsText = `Neither Claude Code nor Codex is installed.

toolshed installs tools into them, so you need at least one:

  Claude Code   curl -fsSL https://claude.ai/install.sh | bash
  Codex         npm install -g @openai/codex`
