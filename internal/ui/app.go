// Package ui holds the Bubble Tea models for the cronen TUI.
package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/system"
)

// Options configures the root model.
type Options struct {
	// Demo reports that the app is running on bundled fixture data.
	Demo bool

	// System access. In demo mode these are fakes serving fixtures.
	Runner  system.Runner
	Fetcher system.Fetcher
	FS      system.FS
}

// Model is the root Bubble Tea model. For now it is a placeholder that
// renders a title and quits on q or ctrl+c.
type Model struct {
	opts   Options
	keys   KeyMap
	styles Styles
}

// New returns the root model.
func New(opts Options) Model {
	return Model{opts: opts, keys: DefaultKeyMap(), styles: DefaultStyles()}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok && key.Matches(msg, m.keys.Quit) {
		return m, tea.Quit
	}
	return m, nil
}

// View implements tea.Model.
func (m Model) View() tea.View {
	title := "cronen"
	if m.opts.Demo {
		title += " (demo)"
	}
	help := m.keys.Quit.Help()
	v := tea.NewView(m.styles.Title.Render(title) + "\n\n" +
		m.styles.Subtle.Render(help.Key+" "+help.Desc) + "\n")
	v.AltScreen = true
	return v
}
