package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// tab is one entry of the tab bar under the header. Its position is the
// number key that opens it.
type tab struct {
	screen screen
	label  string
}

var tabs = []tab{
	{screenUpdates, "Updates"},
	{screenNews, "News"},
	{screenPackages, "Packages"},
	{screenHistory, "History"},
	{screenHousekeeping, "Housekeeping"},
}

// isTab reports whether s is one of the five tab screens.
func isTab(s screen) bool {
	for _, t := range tabs {
		if t.screen == s {
			return true
		}
	}
	return false
}

// activeTab is the tab to highlight: the current screen, or the one a
// modal screen was opened from.
func (m Model) activeTab() screen {
	switch {
	case m.screen == screenHelp:
		return m.prevScreen
	case isTab(m.screen):
		return m.screen
	}
	return screenUpdates
}

// tabBar renders the tab bar in w cells.
func (m Model) tabBar(w int) string {
	active := m.activeTab()
	var parts []string
	for i, t := range tabs {
		label := " " + itoa(i+1) + " " + t.label + " "
		if t.screen == active {
			parts = append(parts, m.styles.Selected.Render(label))
		} else {
			parts = append(parts, m.styles.Subtle.Render(label))
		}
	}
	return fitLine(" "+strings.Join(parts, " "), w)
}

// setScreen switches to s. The name filter belongs to a screen, so it is
// cleared on the way.
func (m *Model) setScreen(s screen) tea.Cmd {
	if m.screen == s {
		return nil
	}
	if m.filterQuery() != "" {
		m.clearFilter()
	}
	m.screen, m.focus = s, focusList
	return m.layout()
}

// screenForKey maps the 1–5 keys to their screens.
func screenForKey(k string) (screen, bool) {
	if len(k) != 1 || k[0] < '1' || int(k[0]-'1') >= len(tabs) {
		return 0, false
	}
	return tabs[k[0]-'1'].screen, true
}
