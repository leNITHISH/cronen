package config

import (
	"encoding/json"
	"testing"
	"time"
)

func TestArchiveInstallDefaultOff(t *testing.T) {
	if Default().ArchiveInstall {
		t.Fatal("archive_install must be off by default")
	}
	cfg, err := Parse([]byte("archive_install = true\n"))
	if err != nil || !cfg.ArchiveInstall {
		t.Fatalf("got %v, %v", cfg.ArchiveInstall, err)
	}
	cfg, err = Parse([]byte("aur = false\n"))
	if err != nil || cfg.ArchiveInstall {
		t.Fatalf("got %v, %v", cfg.ArchiveInstall, err)
	}
}

func TestArchiveInstallState(t *testing.T) {
	var s State
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	s.AddArchiveInstall(ArchiveInstall{Packages: []string{"htop"}, Day: "2026-09-18", Differ: 2, At: at})
	s.AddArchiveInstall(ArchiveInstall{Day: "2026-09-18"}) // no packages: ignored
	s.AddArchiveInstall(ArchiveInstall{Packages: []string{"x"}})
	if got := s.ArchiveInstalls(); len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	data, err := json.Marshal(s.Clone())
	if err != nil {
		t.Fatal(err)
	}
	var back State
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	got := back.ArchiveInstalls()
	if len(got) != 1 || got[0].Day != "2026-09-18" || got[0].Differ != 2 || got[0].Packages[0] != "htop" || !got[0].At.Equal(at) {
		t.Fatalf("round trip: %+v", got)
	}
	for i := range 30 {
		s.AddArchiveInstall(ArchiveInstall{Packages: []string{"p"}, Day: "2026-09-18", Differ: i})
	}
	if got := s.ArchiveInstalls(); len(got) != maxArchiveInstalls || got[len(got)-1].Differ != 29 {
		t.Fatalf("kept %d", len(got))
	}
	// A version 1 file (no archive_installs) still loads.
	var old State
	if err := json.Unmarshal([]byte(`{"version":1,"read_news":[]}`), &old); err != nil || len(old.ArchiveInstalls()) != 0 {
		t.Fatalf("v1: %v", err)
	}
}

func TestPartialConfig(t *testing.T) {
	if !Default().PartialCheck {
		t.Fatal("partial_check must be on by default")
	}
	cfg, err := Parse([]byte("partial_check = false\nlinkage_skip = [\"/opt/mine\", \"[\"]\n"))
	if cfg.PartialCheck {
		t.Error("partial_check = false ignored")
	}
	if len(cfg.LinkageSkip) != 1 || cfg.LinkageSkip[0] != "/opt/mine" || err == nil {
		t.Errorf("linkage_skip = %v, err %v: want the valid glob kept and the bad one reported", cfg.LinkageSkip, err)
	}
}
