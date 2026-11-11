package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
)

const (
	headerLines = 3
	footerLines = 2
)

func (m model) listWidth() int {
	w := m.width / 3
	if w < 28 {
		w = 28
	}
	return w
}

func (m model) detailWidth() int {
	w := m.width - m.listWidth() - 7
	if w < 30 {
		w = 30
	}
	return w
}

func (m model) bodyHeight() int {
	h := m.height - headerLines - footerLines
	if h < 8 {
		h = 8
	}
	return h
}

func (m model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	if m.width == 0 {
		return v
	}
	var body string
	switch m.mode {
	case modeLoading:
		body = "\n  " + accentStyle.Render(spinnerFrames[m.spinner]) + dimStyle.Render(" Looking for Claude Code and Codex…")
	case modeFatal:
		body = "\n" + lipgloss.NewStyle().PaddingLeft(2).Render(errorStyle.Render("✗ ")+textStyle.Render(m.fatal)+"\n\n"+dimStyle.Render("Press any key to exit."))
	default:
		sep := faintStyle.Padding(0, 2).Render(strings.Repeat("│\n", m.bodyHeight()-1) + "│")
		list, detail := m.renderList, m.renderDetail
		if m.inImport {
			list, detail = m.renderImportList, m.renderImportDetail
			if m.mode == modeForm && m.form != nil {
				detail = func() string { return "\n" + m.form.View() }
			}
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(m.listWidth()).Render(list()),
			sep,
			lipgloss.NewStyle().Width(m.detailWidth()).Render(detail()),
		)
	}
	body = lipgloss.NewStyle().Height(m.bodyHeight()).MaxHeight(m.bodyHeight()).PaddingLeft(1).Render(body)
	v.SetContent(m.renderHeader() + "\n" + body + "\n" + m.renderFooter())
	return v
}

func (m model) renderHeader() string {
	bar := lipgloss.NewStyle().Background(headerBg).Foreground(headerFg).Width(m.width)
	ver := Version
	if ver != "dev" && !strings.HasPrefix(ver, "v") {
		ver = "v" + ver
	}
	// Every segment carries the bar's background: an inner Render resets
	// styles at its end, which would otherwise punch a hole in the bar.
	seg := lipgloss.NewStyle().Background(headerBg).Foreground(headerFg)
	title := seg.Bold(true).Render(" ⚒ toolshed") + seg.Render("  add MCP tools to Claude Code & Codex")
	gap := m.width - lipgloss.Width(title) - len(ver) - 1
	if gap < 1 {
		gap = 1
	}
	line := title + seg.Render(strings.Repeat(" ", gap)+ver+" ")
	return bar.Render("") + "\n" + line + "\n" + bar.Render("")
}

// row is one rendered line of the list: a section header or an entry.
type row struct {
	header string
	entry  int // index into reg.Entries, -1 for headers
}

func (m model) rows() []row {
	var out []row
	last := ""
	for i, e := range m.reg.Entries {
		if e.Source != last {
			title := "YOUR TOOLS"
			if e.Source == registry.SourceBuiltin {
				title = "EXAMPLES"
			}
			out = append(out, row{header: title, entry: -1})
			last = e.Source
		}
		out = append(out, row{entry: i})
	}
	return out
}

func (m model) renderList() string {
	rows := m.rows()
	// Each entry takes 2 lines, headers 2 (title + gap); scroll to keep the cursor visible.
	maxRows := (m.bodyHeight() - 1) / 2
	start := 0
	for i, r := range rows {
		if r.entry == m.cursor {
			if i >= maxRows {
				start = i - maxRows + 1
			}
			break
		}
	}
	if m.cursor == 0 {
		start = 0
	}
	var b strings.Builder
	w := m.listWidth()
	for _, r := range rows[start:min(len(rows), start+maxRows)] {
		if r.entry < 0 {
			b.WriteString("\n" + sectionStyle.Render(r.header) + "\n")
			continue
		}
		e := m.reg.Entries[r.entry]
		badges := m.badges(e)
		name := truncate(e.DisplayName(), w-lipgloss.Width(badges)-4)
		summary := e.Summary()
		if summary == "" { // no description: show what it runs instead
			summary = e.URL
			if !e.IsRemote() {
				summary = strings.TrimSpace(e.Command + " " + strings.Join(e.Args, " "))
			}
		}
		summary = truncate(summary, w-3)
		pad := w - 3 - lipgloss.Width(name) - lipgloss.Width(badges)
		if pad < 1 {
			pad = 1
		}
		if r.entry == m.cursor {
			bar := accentStyle.Render("▌")
			b.WriteString(bar + " " + accentStyle.Render(name) + strings.Repeat(" ", pad) + badges + "\n")
			b.WriteString(bar + " " + dimStyle.Render(summary) + "\n")
		} else {
			b.WriteString("  " + textStyle.Render(name) + strings.Repeat(" ", pad) + badges + "\n")
			b.WriteString("  " + faintStyle.Render(summary) + "\n")
		}
	}
	return b.String()
}

// badges renders one dot per client: colored when installed there, faint when not.
func (m model) badges(e *registry.Entry) string {
	if e.Pending {
		return warnStyle.Render("review")
	}
	inst := m.inst[e.ID]
	var parts []string
	for _, c := range clients.All {
		switch {
		case inst != nil && inst.Has(c):
			parts = append(parts, clientStyle(c).Render("●"))
		case !e.SupportsClient(c):
			parts = append(parts, faintStyle.Render(" "))
		default:
			parts = append(parts, faintStyle.Render("○"))
		}
	}
	return strings.Join(parts, " ")
}

func (m model) renderDetail() string {
	if m.mode == modeForm && m.form != nil {
		return "\n" + m.form.View()
	}
	e := m.selected()
	if e == nil {
		msg := "\n" + dimStyle.Render("No tools yet. Press ") + keyStyle.Render("a") + dimStyle.Render(" to add one.")
		if n := m.importable(); n > 0 {
			msg += "\n\n" + dimStyle.Render(fmt.Sprintf("Claude Code and Codex already have %d server%s that toolshed doesn't manage. Press ", n, plural(n))) + keyStyle.Render("m") + dimStyle.Render(" to import them.")
		}
		return msg
	}
	w := m.detailWidth()
	var b strings.Builder
	tag := "example"
	if e.Source == registry.SourceUser {
		tag = "your tool"
	}
	b.WriteString("\n" + nameStyle.Render(e.DisplayName()) + "  " + faintStyle.Render(e.ID+" · "+tag) + "\n\n")
	if e.Pending {
		b.WriteString(warnStyle.Width(w).Render("Added by your agent. Check what it runs below, then press i to approve it.") + "\n\n")
	}
	if d := strings.Join(strings.Fields(e.Description), " "); d != "" {
		b.WriteString(textStyle.Width(w).Render(d) + "\n\n")
	}

	b.WriteString(labelStyle.Render("RUNS") + "\n")
	if e.IsRemote() {
		b.WriteString("  " + codeStyle.Render("⇄ "+e.URL) + "\n")
		if e.HeaderCmd != "" {
			b.WriteString("  " + dimStyle.Render("auth headers from: ") + codeStyle.Render(truncate(e.HeaderCmd, w-22)) + "\n")
		}
		for _, name := range sortedKeys(e.Headers) {
			b.WriteString("  " + dimStyle.Render(name+": ") + codeStyle.Render(truncate(e.Headers[name], w-len(name)-6)) + "\n")
		}
	} else {
		b.WriteString("  " + codeStyle.Render(truncate("$ "+strings.Join(append([]string{e.Command}, e.Args...), " "), w-2)) + "\n")
	}
	var only []string
	for _, c := range clients.All {
		if e.SupportsClient(c) {
			only = append(only, clients.Label(c))
		}
	}
	if len(only) < len(clients.All) {
		b.WriteString("  " + warnStyle.Render(strings.Join(only, ", ")+" only") + "\n")
	}

	if len(e.Inputs) > 0 {
		b.WriteString("\n" + labelStyle.Render("SETTINGS") + "\n")
		for _, in := range e.Inputs {
			lock, note := "  ", ""
			if in.Secret {
				lock, note = "🔒", "keyring"
			}
			if in.Optional {
				note = strings.TrimPrefix(note+", optional", ", ")
			}
			line := lock + " " + textStyle.Render(in.Key)
			if note != "" {
				line += "  " + faintStyle.Render(note)
			}
			b.WriteString("  " + line + "\n")
		}
	}

	b.WriteString("\n" + labelStyle.Render("INSTALLED") + "\n")
	inst := m.inst[e.ID]
	for _, c := range clients.All {
		label := clients.Label(c)
		var status string
		switch {
		case inst != nil && inst.Has(c):
			status = successStyle.Render("installed") + faintStyle.Render(" ("+inst.Scope(c)+")")
		case !contains(m.avail, c):
			status = faintStyle.Render("not on this machine")
		case !e.SupportsClient(c):
			status = faintStyle.Render("not supported")
		default:
			status = dimStyle.Render("—")
		}
		dot := clientStyle(c).Render("●")
		if inst == nil || !inst.Has(c) {
			dot = faintStyle.Render("○")
		}
		fmt.Fprintf(&b, "  %s %-12s %s\n", dot, label, status)
	}

	b.WriteString("\n" + labelStyle.Render("TOOLS") + "\n")
	b.WriteString(m.renderTools(e, w))
	return b.String()
}

func (m model) renderTools(e *registry.Entry, w int) string {
	p := m.probes[e.ID]
	switch {
	case p == nil:
		return "  " + dimStyle.Render("press ") + keyStyle.Render("t") + dimStyle.Render(" to start the server and list its tools") + "\n"
	case p.running:
		return "  " + accentStyle.Render(spinnerFrames[m.spinner]) + dimStyle.Render(" starting server…") + "\n"
	case p.err != nil:
		return "  " + errorStyle.Width(w-2).Render("✗ "+oneLine(p.err.Error())) + "\n"
	case len(p.tools) == 0:
		return "  " + dimStyle.Render("the server responded but offers no tools") + "\n"
	}
	var b strings.Builder
	b.WriteString("  " + successStyle.Render(fmt.Sprintf("✓ %d tools, live from the server", len(p.tools))) + "\n")
	nameW := 0
	for _, t := range p.tools {
		nameW = max(nameW, len(t.Name))
	}
	nameW = min(nameW, w/2)
	room := m.bodyHeight() - 22 // keep the list from pushing past the pane
	for i, t := range p.tools {
		if room > 0 && i >= room {
			b.WriteString("  " + faintStyle.Render(fmt.Sprintf("… and %d more", len(p.tools)-i)) + "\n")
			break
		}
		mark := "  "
		if t.Destructive {
			mark = warnStyle.Render("⚠ ")
		}
		name := fmt.Sprintf("%-*s", nameW, truncate(t.Name, nameW))
		desc := truncate(t.Description, w-nameW-7)
		b.WriteString("  " + mark + textStyle.Render(name) + "  " + faintStyle.Render(desc) + "\n")
	}
	return b.String()
}

func (m model) renderFooter() string {
	var text string
	switch {
	case m.flash != "" && m.mode != modeForm:
		style := successStyle
		if !m.flashOK {
			style = errorStyle
		}
		text = style.Render(truncate(m.flash, m.width-4))
	case m.mode == modeBusy:
		text = accentStyle.Render(spinnerFrames[m.spinner]) + " " + dimStyle.Render(m.busy+"…")
	case m.mode == modeForm && m.formKind == formImport:
		text = keyHelp("space", "tick", "enter", "import", "esc", "cancel")
	case m.mode == modeForm:
		text = keyHelp("enter", "next", "shift+tab", "back", "esc", "cancel")
	case m.mode == modeImport:
		keys := []string{"↑↓", "move"}
		if im := m.selectedImport(); im != nil && im.Skip == "" {
			keys = append(keys, "enter", "import")
		}
		text = keyHelp(append(keys, "esc", "back")...)
	case m.mode == modeBrowse:
		keys := []string{"↑↓", "move", "a", "add"}
		if e := m.selected(); e != nil && e.Pending {
			keys = append(keys, "i", "approve", "d", "delete")
		} else if e != nil {
			if m.inst[e.ID] != nil {
				keys = append(keys, "i", "reinstall", "u", "uninstall")
			} else {
				keys = append(keys, "i", "install")
			}
			keys = append(keys, "t", "tools")
			if e.Source == registry.SourceUser {
				keys = append(keys, "d", "delete")
			}
		}
		if n := m.importable(); n > 0 {
			keys = append(keys, "m", fmt.Sprintf("import %d", n))
		}
		text = keyHelp(append(keys, "q", "quit")...)
	}
	return faintStyle.Render(strings.Repeat("─", max(m.width, 1))) + "\n " + text
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func keyHelp(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, keyStyle.Render(pairs[i])+" "+dimStyle.Render(pairs[i+1]))
	}
	return strings.Join(parts, faintStyle.Render("  ·  "))
}

func truncate(s string, n int) string {
	if n <= 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
