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
	Diff      key.Binding
	// Acknowledge acknowledges the flags on the selected package.
	Acknowledge key.Binding
	// ShowAcked shows or hides acknowledged packages.
	ShowAcked key.Binding
	Upgrade   key.Binding
	// Screens switches to a screen by number, 1 to 5.
	Screens key.Binding
	// Mark marks a package on the Packages screen; CycleFilter and
	// CycleSort change what it lists and in which order.
	Mark        key.Binding
	CycleFilter key.Binding
	CycleSort   key.Binding
	// Remove removes the marked packages; Pacdiff merges .pacnew files.
	Remove  key.Binding
	Pacdiff key.Binding
	// Prefetch downloads the pending packages now, to install later.
	Prefetch key.Binding
	// Install opens the install helper ("I need X now").
	Install key.Binding
	// Downgrade installs an older cached version of a package.
	Downgrade key.Binding
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
		Diff: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "review AUR diff"),
		),
		Acknowledge: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "acknowledge flags"),
		),
		ShowAcked: key.NewBinding(
			key.WithKeys("A"),
			key.WithHelp("A", "show/hide acknowledged"),
		),
		Upgrade: key.NewBinding(
			key.WithKeys("U"),
			key.WithHelp("U", "upgrade"),
		),
		Screens: key.NewBinding(
			key.WithKeys("1", "2", "3", "4", "5"),
			key.WithHelp("1-5", "switch screen"),
		),
		Mark: key.NewBinding(
			key.WithKeys("space"),
			key.WithHelp("space", "mark"),
		),
		CycleFilter: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "package filter"),
		),
		CycleSort: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "package sort (updates: by download size)"),
		),
		Remove: key.NewBinding(
			key.WithKeys("X"),
			key.WithHelp("X", "remove marked (preview first)"),
		),
		Pacdiff: key.NewBinding(
			key.WithKeys("P"),
			key.WithHelp("P", "merge .pacnew (housekeeping)"),
		),
		Prefetch: key.NewBinding(
			key.WithKeys("F"),
			key.WithHelp("F", "prefetch: download updates now"),
		),
		Install: key.NewBinding(
			key.WithKeys("I"),
			key.WithHelp("I", "install helper: I need X now"),
		),
		Downgrade: key.NewBinding(
			key.WithKeys("D"),
			key.WithHelp("D", "downgrade from cache"),
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
		{k.HalfDown, k.HalfUp, k.Focus, k.Filter, k.Screens, k.Mark, k.CycleFilter, k.CycleSort, k.Remove, k.Pacdiff, k.Downgrade},
		{k.News, k.MarkRead, k.Upstream, k.Changelog, k.Diff},
		{k.Acknowledge, k.ShowAcked},
		{k.Upgrade, k.Prefetch, k.Install, k.Refresh, k.Back, k.Help, k.Quit},
	}
}
