package ui

import "charm.land/bubbles/v2/key"

// KeyMap holds every key binding in the app.
type KeyMap struct {
	Quit key.Binding
}

// DefaultKeyMap returns the default, vim-style key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}
