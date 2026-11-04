package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/system"
)

func TestStateV1StillLoads(t *testing.T) {
	fsys := system.NewFakeFS(map[string][]byte{
		statePath: []byte(`{"version":1,"read_news":["a"],"last_upgrade":"2026-10-20T13:32:11Z"}`),
	})
	s, err := LoadState(fsys, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !s.IsRead("a") || s.LastUpgrade.IsZero() || len(s.Maintainers()) != 0 {
		t.Fatalf("state = %+v", s)
	}
	// Saving it writes version 2, and it loads again.
	if err := SaveState(fsys, statePath, s); err != nil {
		t.Fatal(err)
	}
	data, _ := fsys.ReadFile(statePath)
	if !strings.Contains(string(data), `"version": 2`) {
		t.Errorf("saved %s", data)
	}
	if _, err := LoadState(fsys, statePath); err != nil {
		t.Fatal(err)
	}
}

func TestStateMaintainers(t *testing.T) {
	var s State
	if _, ok := s.Maintainer("foo"); ok {
		t.Fatal("zero state has a maintainer")
	}
	s.SetMaintainer(Maintainer{PkgBase: "zed", Maintainer: "z"})
	s.SetMaintainer(Maintainer{PkgBase: "foo", Maintainer: "alice", CoMaintainers: []string{"y", "x"}})
	s.SetMaintainer(Maintainer{Maintainer: "nobody"}) // no package base: ignored
	s.SetMaintainer(Maintainer{PkgBase: "orphan"})

	want := []Maintainer{
		{PkgBase: "foo", Maintainer: "alice", CoMaintainers: []string{"x", "y"}},
		{PkgBase: "orphan"},
		{PkgBase: "zed", Maintainer: "z"},
	}
	if got := s.Maintainers(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Maintainers = %+v, want %+v", got, want)
	}

	fsys := system.NewFakeFS(nil)
	if err := SaveState(fsys, statePath, s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(fsys, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Maintainers(), want) {
		t.Fatalf("after a round trip: %+v", got.Maintainers())
	}

	// A clone doesn't share the records with its source.
	c := s.Clone()
	c.SetMaintainer(Maintainer{PkgBase: "foo", Maintainer: "mallory"})
	if m, _ := s.Maintainer("foo"); m.Maintainer != "alice" {
		t.Errorf("clone changed the original: %+v", m)
	}
	if m, _ := c.Maintainer("zed"); m.Maintainer != "z" {
		t.Errorf("clone lost a record: %+v", m)
	}
}

func TestStateNewerVersionStillRejected(t *testing.T) {
	fsys := system.NewFakeFS(map[string][]byte{statePath: []byte(`{"version":3,"aur_maintainers":[{"pkgbase":"a","maintainer":"b"}]}`)})
	s, err := LoadState(fsys, statePath)
	if err == nil || len(s.Maintainers()) != 0 {
		t.Fatalf("got %+v, %v", s, err)
	}
}

func TestStateAcknowledgements(t *testing.T) {
	var s State
	if got := s.Acknowledged("foo", "1-1"); got != nil {
		t.Fatalf("zero state: %v", got)
	}
	if s.Acknowledge("", "1-1", "kernel") || s.Acknowledge("foo", "", "kernel") || s.Acknowledge("foo", "1-1") {
		t.Fatal("an incomplete acknowledgement was recorded")
	}
	if !s.Acknowledge("foo", "1-1", "watchlist", "kernel") {
		t.Fatal("not recorded")
	}
	if s.Acknowledge("foo", "1-1", "kernel") {
		t.Error("an already acknowledged rule reported a change")
	}
	// A new rule for the same version is added.
	s.Acknowledge("foo", "1-1", "major-bump")
	if got, want := s.Acknowledged("foo", "1-1"), []string{"kernel", "major-bump", "watchlist"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rules = %v, want %v", got, want)
	}
	// Only the exact version counts.
	if got := s.Acknowledged("foo", "1-2"); got != nil {
		t.Errorf("another version: %v", got)
	}
	// A new version replaces the old record.
	s.Acknowledge("foo", "1-2", "kernel")
	if got := s.Acknowledged("foo", "1-1"); got != nil {
		t.Errorf("the old version is still acknowledged: %v", got)
	}
	s.Acknowledge("bar", "2", "held")

	fsys := system.NewFakeFS(nil)
	if err := SaveState(fsys, statePath, s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(fsys, statePath)
	if err != nil {
		t.Fatal(err)
	}
	want := []Acknowledgement{{Name: "bar", Version: "2", Rules: []string{"held"}}, {Name: "foo", Version: "1-2", Rules: []string{"kernel"}}}
	if !reflect.DeepEqual(got.Acknowledgements(), want) {
		t.Fatalf("after a round trip: %+v", got.Acknowledgements())
	}

	c := s.Clone()
	c.Acknowledge("foo", "1-2", "watchlist")
	if g := s.Acknowledged("foo", "1-2"); !reflect.DeepEqual(g, []string{"kernel"}) {
		t.Errorf("clone changed the original: %v", g)
	}
	if g := c.Acknowledged("bar", "2"); !reflect.DeepEqual(g, []string{"held"}) {
		t.Errorf("clone lost a record: %v", g)
	}
}
