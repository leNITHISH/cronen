package config

import (
	"strings"
	"testing"
	"time"
)

func TestSnapshotConfigDefaultsAndParsing(t *testing.T) {
	d := Default()
	if !d.Snapshots || d.SnapshotTool != "" || d.SnapshotBeforeUpgrade != SnapshotAsk ||
		d.SnapshotMaxAgeDays != 7 || d.SnapshotListSudo || d.SnapperConfig != "root" {
		t.Errorf("defaults = %+v", d)
	}
	cfg, err := Parse([]byte(`
snapshot_tool = "snapper"
snapshot_before_upgrade = "always"
snapshot_max_age_days = 3
snapshot_list_sudo = true
snapper_config = "home"
snapshots = false
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SnapshotTool != "snapper" || cfg.SnapshotBeforeUpgrade != SnapshotAlways || cfg.SnapshotMaxAgeDays != 3 ||
		!cfg.SnapshotListSudo || cfg.SnapperConfig != "home" || cfg.Snapshots {
		t.Errorf("parsed = %+v", cfg)
	}
}

func TestSnapshotConfigInvalidValuesFallBack(t *testing.T) {
	for _, c := range []struct{ toml, want string }{
		{`snapshot_tool = "btrfs"`, "snapshot_tool"},
		{`snapshot_before_upgrade = "sometimes"`, "snapshot_before_upgrade"},
		{`snapshot_max_age_days = 0`, "snapshot_max_age_days"},
		{`snapshot_max_age_days = -4`, "snapshot_max_age_days"},
		{`snapper_config = "--help"`, "snapper_config"},
		{`snapper_config = ""`, "snapper_config"},
		{`snapper_config = "a b; rm"`, "snapper_config"},
	} {
		cfg, err := Parse([]byte(c.toml))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v", c.toml, err)
		}
		d := Default()
		if cfg.SnapshotTool != d.SnapshotTool || cfg.SnapshotBeforeUpgrade != d.SnapshotBeforeUpgrade ||
			cfg.SnapshotMaxAgeDays != d.SnapshotMaxAgeDays || cfg.SnapperConfig != d.SnapperConfig {
			t.Errorf("%s: did not fall back: %+v", c.toml, cfg)
		}
	}
}

func TestStateSnapshotRecords(t *testing.T) {
	var s State
	at := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	s.AddSnapshot(SnapshotRecord{At: at, Tool: "timeshift", Comment: "cronen: before -Syu (3 packages)"})
	s.AddSnapshot(SnapshotRecord{Tool: "timeshift"}) // no time: ignored
	s.AddSnapshot(SnapshotRecord{At: at})            // no tool: ignored
	if got := s.Snapshots(); len(got) != 1 || !got[0].At.Equal(at) {
		t.Fatalf("snapshots = %+v", got)
	}
	for i := range 60 {
		s.AddSnapshot(SnapshotRecord{At: at.Add(time.Duration(i) * time.Hour), Tool: "snapper"})
	}
	if got := s.Snapshots(); len(got) != 50 || got[49].At != at.Add(59*time.Hour) {
		t.Errorf("kept %d, last %v", len(got), got[len(got)-1].At)
	}

	data, err := s.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var back State
	if err := back.UnmarshalJSON(data); err != nil {
		t.Fatal(err)
	}
	if len(back.Snapshots()) != 50 || len(s.Clone().Snapshots()) != 50 {
		t.Errorf("round trip %d, clone %d", len(back.Snapshots()), len(s.Clone().Snapshots()))
	}
	// The clone is independent.
	c := s.Clone()
	c.AddSnapshot(SnapshotRecord{At: at.Add(100 * time.Hour), Tool: "x"})
	if s.Snapshots()[49].Tool == "x" {
		t.Error("clone shares the records")
	}
}

func TestStateV2FileWithoutSnapshotsLoads(t *testing.T) {
	var s State
	if err := s.UnmarshalJSON([]byte(`{"version":2,"read_news":["a"]}`)); err != nil {
		t.Fatal(err)
	}
	if len(s.Snapshots()) != 0 || !s.IsRead("a") {
		t.Errorf("%+v", s)
	}
	if err := s.UnmarshalJSON([]byte(`{"version":1,"read_news":[]}`)); err != nil {
		t.Fatal(err)
	}
}
