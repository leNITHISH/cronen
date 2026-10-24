package ui

import "charm.land/bubbles/v2/key"

// KeyMap holds every key binding in the app. It implements help.KeyMap, so
// the help screen is generated from it.
type KeyMap struct {
	Up        key.Binding
	Down      key.Binding
	Top       key.Binding
	Bottom    key.Binding
	HalfDown  key.Binding
	HalfUp    key.Binding
	Focus     key.Binding
	Filter    key.Binding
	News      key.Binding
	MarkRead  key.Binding
	Upstream  key.Binding
	Changelog key.Binding
	Upgrade   key.Binding
	Back      key.Binding
	Refresh   key.Binding
	Help      key.Binding
	Quit      key.Binding

	// Accept ends filter input, keeping the filter.
	Accept key.Binding
	// Confirm and Cancel answer the upgrade confirmation.
	Confirm key.Binding
	Cancel  key.Binding
}

// DefaultKeyMap returns the default, vim-style key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("k", "up"),
			key.WithHelp("k/↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("j", "down"),
			key.WithHelp("j/↓", "down"),
		),
		Top: key.NewBinding(
			key.WithKeys("g", "home"),
			key.WithHelp("g", "top"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("G", "end"),
			key.WithHelp("G", "bottom"),
		),
		HalfDown: key.NewBinding(
			key.WithKeys("ctrl+d"),
			key.WithHelp("ctrl+d", "half page down"),
		),
		HalfUp: key.NewBinding(
			key.WithKeys("ctrl+u"),
			key.WithHelp("ctrl+u", "half page up"),
		),
		Focus: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "switch pane"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter packages"),
		),
		News: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "news"),
		),
		MarkRead: key.NewBinding(
			key.WithKeys("m"),
			key.WithHelp("m", "mark news read"),
		),
		Upstream: key.NewBinding(
			key.WithKeys("o"),
			key.WithHelp("o", "open upstream"),
		),
		Changelog: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "open changelog"),
		),
		Upgrade: key.NewBinding(
			key.WithKeys("U"),
			key.WithHelp("U", "upgrade"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Accept: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "apply"),
		),
		Confirm: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "upgrade"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("n", "esc"),
			key.WithHelp("n/esc", "cancel"),
		),
	}
}

// ShortHelp implements help.KeyMap.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}

// FullHelp implements help.KeyMap.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.HalfDown, k.HalfUp, k.Focus, k.Filter},
		{k.News, k.MarkRead, k.Upstream, k.Changelog},
		{k.Upgrade, k.Refresh, k.Back, k.Help, k.Quit},
	}
}
