package tui

import (
	"time"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const (
	spinnerInterval = 80 * time.Millisecond
	flashDuration   = 4 * time.Second
)

// Palette: warm jewel tones on a dark terminal.
var (
	accentColor  = lipgloss.Color("176") // mauve: selection
	primaryColor = lipgloss.Color("110") // steel blue: names
	successColor = lipgloss.Color("114") // green
	errorColor   = lipgloss.Color("204") // rose
	warningColor = lipgloss.Color("215") // amber
	textColor    = lipgloss.Color("253")
	subtextColor = lipgloss.Color("246")
	faintColor   = lipgloss.Color("240")
	headerBg     = lipgloss.Color("183")
	headerFg     = lipgloss.Color("233")
	claudeColor  = lipgloss.Color("209") // Claude orange
	codexColor   = lipgloss.Color("117") // Codex blue
)

var (
	textStyle    = lipgloss.NewStyle().Foreground(textColor)
	dimStyle     = lipgloss.NewStyle().Foreground(subtextColor)
	faintStyle   = lipgloss.NewStyle().Foreground(faintColor)
	nameStyle    = lipgloss.NewStyle().Foreground(primaryColor).Bold(true)
	accentStyle  = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	successStyle = lipgloss.NewStyle().Foreground(successColor)
	errorStyle   = lipgloss.NewStyle().Foreground(errorColor)
	warnStyle    = lipgloss.NewStyle().Foreground(warningColor)
	sectionStyle = lipgloss.NewStyle().Foreground(subtextColor).Bold(true)
	labelStyle   = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	codeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("223"))
	keyStyle     = lipgloss.NewStyle().Foreground(textColor).Bold(true)
)

// clientStyle colors a client's badge.
func clientStyle(client string) lipgloss.Style {
	if client == "codex" {
		return lipgloss.NewStyle().Foreground(codexColor)
	}
	return lipgloss.NewStyle().Foreground(claudeColor)
}

func formTheme() huh.Theme { return huh.ThemeFunc(huh.ThemeCatppuccin) }
