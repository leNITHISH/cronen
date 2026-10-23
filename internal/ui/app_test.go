package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestQuitKeys(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyPressMsg
		quit bool
	}{
		{"q", tea.KeyPressMsg{Code: 'q', Text: "q"}, true},
		{"ctrl+c", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, true},
		{"j", tea.KeyPressMsg{Code: 'j', Text: "j"}, false},
		{"c", tea.KeyPressMsg{Code: 'c', Text: "c"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cmd := New(Options{}).Update(tt.msg)
			gotQuit := cmd != nil && isQuit(cmd())
			if gotQuit != tt.quit {
				t.Fatalf("quit = %v, want %v", gotQuit, tt.quit)
			}
		})
	}
}

func isQuit(msg tea.Msg) bool {
	_, ok := msg.(tea.QuitMsg)
	return ok
}

func TestView(t *testing.T) {
	v := New(Options{Demo: true}).View()
	if !strings.Contains(v.Content, "cronen (demo)") || !strings.Contains(v.Content, "q quit") {
		t.Fatalf("view = %q", v.Content)
	}
	if !v.AltScreen {
		t.Error("want alt screen")
	}
}
