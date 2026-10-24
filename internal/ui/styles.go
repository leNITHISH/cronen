package ui

import (
	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"

	"github.com/leNITHISH/cronen/internal/risk"
)

// Styles holds the lipgloss styles used by the UI. Colors are kept for
// severity; everything else is bold, faint or plain.
type Styles struct {
	Title    lipgloss.Style
	Subtle   lipgloss.Style
	Bold     lipgloss.Style
	Section  lipgloss.Style
	Selected lipgloss.Style
	Rule     lipgloss.Style
	Banner   lipgloss.Style
	Error    lipgloss.Style
	Info     lipgloss.Style
	Warn     lipgloss.Style
	Critical lipgloss.Style
	Help     help.Styles
}

// DefaultStyles returns the styles for a dark or light terminal background.
func DefaultStyles(isDark bool) Styles {
	c := lipgloss.LightDark(isDark)
	red := c(lipgloss.Color("#B3261E"), lipgloss.Color("#F2B8B5"))
	yellow := c(lipgloss.Color("#8A6100"), lipgloss.Color("#E8C26A"))
	blue := c(lipgloss.Color("#1F5FA8"), lipgloss.Color("#8AB4F8"))
	return Styles{
		Title:    lipgloss.NewStyle().Bold(true),
		Subtle:   lipgloss.NewStyle().Faint(true),
		Bold:     lipgloss.NewStyle().Bold(true),
		Section:  lipgloss.NewStyle().Bold(true).Faint(true),
		Selected: lipgloss.NewStyle().Reverse(true),
		Rule:     lipgloss.NewStyle().Faint(true),
		Banner:   lipgloss.NewStyle().Bold(true).Foreground(red),
		Error:    lipgloss.NewStyle().Foreground(red),
		Info:     lipgloss.NewStyle().Foreground(blue),
		Warn:     lipgloss.NewStyle().Foreground(yellow),
		Critical: lipgloss.NewStyle().Bold(true).Foreground(red),
		Help:     help.DefaultStyles(isDark),
	}
}

// Severity returns the style for a severity level.
func (s Styles) Severity(sev risk.Severity) lipgloss.Style {
	switch sev {
	case risk.Info:
		return s.Info
	case risk.Warn:
		return s.Warn
	case risk.Critical:
		return s.Critical
	}
	return lipgloss.NewStyle()
}

// severityIcon is the marker shown next to a flagged package.
func severityIcon(sev risk.Severity) string {
	switch sev {
	case risk.Info:
		return "ℹ"
	case risk.Warn:
		return "⚠"
	case risk.Critical:
		return "‼"
	}
	return " "
}
