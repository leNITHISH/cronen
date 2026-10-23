package ui

import "charm.land/lipgloss/v2"

// Styles holds the lipgloss styles used by the UI.
type Styles struct {
	Title  lipgloss.Style
	Subtle lipgloss.Style
}

// DefaultStyles returns the default styles.
func DefaultStyles() Styles {
	return Styles{
		Title:  lipgloss.NewStyle().Bold(true),
		Subtle: lipgloss.NewStyle().Faint(true),
	}
}
