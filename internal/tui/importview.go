package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/secrets"
	"github.com/bartdegoede/toolshed/internal/shed"
)

// The import screen lists the servers Claude Code and Codex have that aren't
// in the toolshed (shed.Imports), and imports them one at a time: the user
// ticks which values are secrets, and shed.Import.Apply does the rest.

type importedMsg struct {
	text string
	err  error
}

func (m *model) refreshImports() {
	if m.reg == nil {
		m.imports = nil
		return
	}
	m.imports = shed.Imports(m.reg, m.inst)
	if m.impCursor >= len(m.imports) {
		m.impCursor = max(0, len(m.imports)-1)
	}
}

// importable counts the servers that can be imported.
func (m model) importable() int {
	n := 0
	for _, im := range m.imports {
		if im.Skip == "" {
			n++
		}
	}
	return n
}

func (m model) selectedImport() *shed.Import {
	if m.impCursor < 0 || m.impCursor >= len(m.imports) {
		return nil
	}
	return &m.imports[m.impCursor]
}

func (m model) openImports() (tea.Model, tea.Cmd) {
	m.refreshImports()
	if len(m.imports) == 0 {
		return m, m.setFlash("Nothing to import: everything Claude Code and Codex have is in your toolshed", true)
	}
	m.mode, m.inImport, m.flash = modeImport, true, ""
	return m, nil
}

func (m model) handleImportKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "m":
		m.mode, m.inImport = modeBrowse, false
	case "up", "k":
		if m.impCursor > 0 {
			m.impCursor--
		}
	case "down", "j":
		if m.impCursor < len(m.imports)-1 {
			m.impCursor++
		}
	case "enter", "i":
		im := m.selectedImport()
		if im == nil {
			return m, nil
		}
		if im.Skip != "" {
			return m, m.setFlash("Can't import "+im.Name+": "+im.Skip, false)
		}
		ticked := []string{}
		m.impSecrets = &ticked
		var opts []huh.Option[string]
		for _, v := range im.Vars {
			if v.Value == "" {
				continue
			}
			o := huh.NewOption(fmt.Sprintf("%-22s %s", v.Key, mask(v.Value)), v.Key)
			if v.Secret {
				o = o.Selected(true)
				*m.impSecrets = append(*m.impSecrets, v.Key)
			}
			opts = append(opts, o)
		}
		where := "Secrets move to your keyring; the rest stay in the client's config."
		if !secrets.Available() {
			where = "There's no OS keyring here, so values stay in the client's config."
		}
		var field huh.Field
		if len(opts) > 0 {
			desc := "Ticked by their names; check them. " + where + "\nspace: tick · enter: import"
			field = huh.NewMultiSelect[string]().
				Title("Which of these are secrets?").
				Description(desc).
				Options(opts...).
				// huh sizes the list too small without a height, hiding the options.
				Height(len(opts) + lipgloss.Height(lipgloss.NewStyle().Width(m.detailWidth()-4).Render(desc)) + 2).
				Value(m.impSecrets)
		} else {
			yes := true
			m.confirm = &yes
			field = huh.NewConfirm().Title("Import " + im.Name + "?").
				Description("It's added to your toolshed and re-registered through it, where it is now.").
				Affirmative("Import").Negative("Cancel").Value(m.confirm)
		}
		return m.openForm(formImport, huh.NewForm(huh.NewGroup(field)).WithTheme(formTheme()).WithWidth(m.detailWidth()).WithShowHelp(false))
	}
	return m, nil
}

// submitImport runs the import the form was for.
func (m model) submitImport() (tea.Model, tea.Cmd) {
	im := m.selectedImport()
	if im == nil || (len(im.Vars) == 0 && !m.confirmed()) {
		m.mode = modeImport
		return m, nil
	}
	ticked := map[string]bool{}
	for _, k := range *m.impSecrets {
		ticked[k] = true
	}
	imp := *im
	imp.Vars = append([]shed.ImportVar(nil), im.Vars...)
	imp.Values = shed.Values{}
	for i := range imp.Vars {
		imp.Vars[i].Secret = ticked[imp.Vars[i].Key]
	}
	m.mode, m.busy = modeBusy, "Importing "+im.Name
	return m, tea.Batch(tick(), func() tea.Msg {
		res, err := imp.Apply(true)
		if err != nil {
			return importedMsg{err: err}
		}
		text := "Imported " + res.Entry.ID
		if len(res.Moved) > 0 {
			text += " · " + strings.Join(res.Moved, ", ") + " moved to your keyring"
		}
		if len(res.Notes) > 0 {
			text += " · " + res.Notes[0]
		}
		return importedMsg{text: text}
	})
}

func (m model) renderImportList() string {
	var b strings.Builder
	w := m.listWidth()
	b.WriteString("\n" + sectionStyle.Render("NOT IN YOUR TOOLSHED") + "\n")
	maxRows := (m.bodyHeight() - 2) / 2
	start := 0
	if m.impCursor >= maxRows {
		start = m.impCursor - maxRows + 1
	}
	for i := start; i < min(len(m.imports), start+maxRows); i++ {
		im := m.imports[i]
		var dots []string
		for _, c := range clients.All {
			if contains(im.Inst.Clients(), c) {
				dots = append(dots, clientStyle(c).Render("●"))
			} else {
				dots = append(dots, faintStyle.Render("○"))
			}
		}
		badges := strings.Join(dots, " ")
		name := truncate(im.Name, w-lipgloss.Width(badges)-4)
		summary := im.Entry.CommandLine()
		if im.Skip != "" {
			summary = "can't import: " + im.Skip
		}
		summary = truncate(summary, w-3)
		pad := max(1, w-3-lipgloss.Width(name)-lipgloss.Width(badges))
		if i == m.impCursor {
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

func (m model) renderImportDetail() string {
	im := m.selectedImport()
	if im == nil {
		return "\n" + dimStyle.Render("Nothing left to import.")
	}
	w := m.detailWidth()
	var b strings.Builder
	b.WriteString("\n" + nameStyle.Render(im.Name) + "  " + faintStyle.Render("not in your toolshed") + "\n\n")
	b.WriteString(textStyle.Width(w).Render("Importing adds it to your toolshed and registers it again through toolshed, where it is now, so its secrets can move out of the config file into your keyring.") + "\n\n")
	if im.Skip != "" {
		b.WriteString(warnStyle.Width(w).Render("Can't import it: "+im.Skip) + "\n\n")
	}

	b.WriteString(labelStyle.Render("REGISTERED IN") + "\n")
	for _, c := range im.Inst.Clients() {
		label := map[string]string{clients.Claude: "Claude Code", clients.Codex: "Codex"}[c]
		fmt.Fprintf(&b, "  %s %-12s %s\n", clientStyle(c).Render("●"), label, faintStyle.Render(im.Inst.Scope(c)))
	}
	b.WriteString("\n" + labelStyle.Render("RUNS") + "\n")
	b.WriteString("  " + codeStyle.Render(truncate(im.Entry.CommandLine(), w-2)) + "\n")
	if im.Rename && im.Skip == "" {
		b.WriteString("  " + dimStyle.Render("becomes ") + textStyle.Render(im.Entry.ID) + "\n")
	}
	for _, n := range im.Notes {
		b.WriteString("  " + warnStyle.Width(w-2).Render(n) + "\n")
	}

	if len(im.Vars) > 0 {
		b.WriteString("\n" + labelStyle.Render("VALUES") + "\n")
		for _, v := range im.Vars {
			lock, note := "  ", "stays in the config"
			if v.Secret {
				lock, note = "🔒", "looks like a secret"
			}
			if v.Value == "" {
				note = "empty"
			}
			fmt.Fprintf(&b, "  %s %s  %s  %s\n", lock, textStyle.Render(v.Key), faintStyle.Render(mask(v.Value)), faintStyle.Render(note))
		}
	}
	return b.String()
}

// mask shows enough of a value to recognise it, not enough to leak it.
func mask(v string) string {
	switch n := len([]rune(v)); {
	case n == 0:
		return `""`
	case n <= 8:
		return "****"
	default:
		return string([]rune(v)[:4]) + fmt.Sprintf("… (%d chars)", n)
	}
}
