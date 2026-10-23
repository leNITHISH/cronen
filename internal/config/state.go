package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"time"

	"github.com/leNITHISH/cronen/internal/system"
)

// stateVersion is written to the state file so the format can change later.
const stateVersion = 1

// State is what cronen remembers between runs. The zero value is an empty
// state and is ready to use.
type State struct {
	// LastUpgrade is when cronen last ran a successful upgrade, or the
	// zero time if it never has.
	LastUpgrade time.Time
	readNews    map[string]bool
}

type stateFile struct {
	Version     int        `json:"version"`
	ReadNews    []string   `json:"read_news"`
	LastUpgrade *time.Time `json:"last_upgrade,omitempty"`
}

// IsRead reports whether the news item with this GUID was marked read.
func (s *State) IsRead(guid string) bool {
	return s.readNews[guid]
}

// MarkRead marks news items as read.
func (s *State) MarkRead(guids ...string) {
	if s.readNews == nil {
		s.readNews = map[string]bool{}
	}
	for _, g := range guids {
		if g != "" {
			s.readNews[g] = true
		}
	}
}

// ReadNews returns the GUIDs marked read, sorted.
func (s *State) ReadNews() []string {
	out := make([]string, 0, len(s.readNews))
	for g := range s.readNews {
		out = append(out, g)
	}
	slices.Sort(out)
	return out
}

// MarshalJSON implements json.Marshaler.
func (s State) MarshalJSON() ([]byte, error) {
	f := stateFile{Version: stateVersion, ReadNews: s.ReadNews()}
	if !s.LastUpgrade.IsZero() {
		t := s.LastUpgrade
		f.LastUpgrade = &t
	}
	return json.Marshal(f)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *State) UnmarshalJSON(data []byte) error {
	var f stateFile
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	if f.Version > stateVersion {
		return fmt.Errorf("state version %d is newer than supported version %d", f.Version, stateVersion)
	}
	*s = State{}
	if f.LastUpgrade != nil {
		s.LastUpgrade = *f.LastUpgrade
	}
	s.MarkRead(f.ReadNews...)
	return nil
}

// LoadState reads the state file at path. A missing file yields an empty
// State and a nil error. An unreadable or corrupt file yields an empty
// State and an error; callers can carry on with it.
func LoadState(fsys system.FS, path string) (State, error) {
	data, err := fsys.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("reading state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("state %s: %w", path, err)
	}
	return s, nil
}

// SaveState atomically writes the state file at path, creating its
// directory if needed.
func SaveState(fsys system.FS, path string, s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := fsys.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing state: %w", err)
	}
	return nil
}
