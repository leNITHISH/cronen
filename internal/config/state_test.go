package config

import (
	"errors"
	"io/fs"
	"reflect"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/system"
)

const statePath = "/home/u/.local/state/cronen/state.json"

func TestStateZeroValue(t *testing.T) {
	var s State
	if s.IsRead("x") {
		t.Fatal("zero state has read news")
	}
	s.MarkRead("b", "a", "", "b")
	if !s.IsRead("a") || !s.IsRead("b") || s.IsRead("") {
		t.Fatalf("read = %q", s.ReadNews())
	}
	if got := s.ReadNews(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("ReadNews = %q", got)
	}
}

func TestStateRoundTrip(t *testing.T) {
	fsys := system.NewFakeFS(nil)
	var s State
	s.LastUpgrade = time.Date(2026, 10, 20, 19, 2, 11, 0, time.FixedZone("", 5*60*60+30*60))
	s.MarkRead("https://archlinux.org/news/a/", "https://archlinux.org/news/b/")
	if err := SaveState(fsys, statePath, s); err != nil {
		t.Fatal(err)
	}

	got, err := LoadState(fsys, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastUpgrade.Equal(s.LastUpgrade) {
		t.Fatalf("LastUpgrade = %v, want %v", got.LastUpgrade, s.LastUpgrade)
	}
	if !reflect.DeepEqual(got.ReadNews(), s.ReadNews()) {
		t.Fatalf("ReadNews = %q, want %q", got.ReadNews(), s.ReadNews())
	}

	// A never-upgraded state stays zero through a round trip.
	if err := SaveState(fsys, statePath, State{}); err != nil {
		t.Fatal(err)
	}
	got, err = LoadState(fsys, statePath)
	if err != nil || !got.LastUpgrade.IsZero() || len(got.ReadNews()) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestLoadState(t *testing.T) {
	tests := []struct {
		name     string
		data     string // "" with missing=true means no file
		missing  bool
		readErr  error
		wantErr  bool
		wantRead []string
		wantLast time.Time
	}{
		{name: "missing file", missing: true, wantRead: []string{}},
		{name: "unreadable", data: "{}", readErr: fs.ErrPermission, wantErr: true, wantRead: []string{}},
		{name: "empty file", data: "", wantErr: true, wantRead: []string{}},
		{name: "garbage", data: "\x00not json", wantErr: true, wantRead: []string{}},
		{name: "truncated", data: `{"version":1,"read_news":["a"`, wantErr: true, wantRead: []string{}},
		{name: "wrong types", data: `{"read_news":"a","last_upgrade":5}`, wantErr: true, wantRead: []string{}},
		{name: "newer version", data: `{"version":99,"read_news":["a"]}`, wantErr: true, wantRead: []string{}},
		{name: "null", data: "null", wantRead: []string{}},
		{name: "empty object", data: "{}", wantRead: []string{}},
		{name: "null fields", data: `{"version":1,"read_news":null,"last_upgrade":null}`, wantRead: []string{}},
		{
			name:     "valid",
			data:     `{"version":1,"read_news":["b","a"],"last_upgrade":"2026-10-20T13:32:11Z"}`,
			wantRead: []string{"a", "b"},
			wantLast: time.Date(2026, 10, 20, 13, 32, 11, 0, time.UTC),
		},
		{name: "no version field", data: `{"read_news":["a"]}`, wantRead: []string{"a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{}
			if !tt.missing {
				files[statePath] = []byte(tt.data)
			}
			fsys := system.NewFakeFS(files)
			fsys.ReadErr = tt.readErr

			s, err := LoadState(fsys, statePath)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.readErr != nil && !errors.Is(err, tt.readErr) {
				t.Fatalf("err = %v, want %v", err, tt.readErr)
			}
			if got := s.ReadNews(); !reflect.DeepEqual(got, tt.wantRead) {
				t.Fatalf("ReadNews = %q, want %q", got, tt.wantRead)
			}
			if !s.LastUpgrade.Equal(tt.wantLast) {
				t.Fatalf("LastUpgrade = %v, want %v", s.LastUpgrade, tt.wantLast)
			}
			// Whatever was loaded must be usable.
			s.MarkRead("new")
			if !s.IsRead("new") {
				t.Fatal("MarkRead after load failed")
			}
		})
	}
}

type failingFS struct{ system.FS }

func (failingFS) WriteFile(string, []byte, fs.FileMode) error { return fs.ErrPermission }

func TestSaveStateError(t *testing.T) {
	err := SaveState(failingFS{system.NewFakeFS(nil)}, statePath, State{})
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("got %v", err)
	}
}

func TestSaveStateOSFS(t *testing.T) {
	// The real FS creates the state directory on first save.
	p := t.TempDir() + "/state/cronen/state.json"
	var s State
	s.MarkRead("g")
	if err := SaveState(system.OSFS{}, p, s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(system.OSFS{}, p)
	if err != nil || !got.IsRead("g") {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestStateClone(t *testing.T) {
	var s State
	s.LastUpgrade = time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	s.MarkRead("a")
	c := s.Clone()
	s.MarkRead("b")
	c.MarkRead("c")
	if got := c.ReadNews(); !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Errorf("clone read = %q, want [a c]", got)
	}
	if got := s.ReadNews(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("original read = %q, want [a b]", got)
	}
	if !c.LastUpgrade.Equal(s.LastUpgrade) {
		t.Errorf("clone LastUpgrade = %v", c.LastUpgrade)
	}
}
