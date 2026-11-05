package config

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/bandwidth"
	"github.com/leNITHISH/cronen/internal/system"
)

func sample(i int) bandwidth.Sample {
	return bandwidth.Sample{Time: time.Date(2026, 1, i, 0, 0, 0, 0, time.UTC), Bytes: int64(i) << 20, Packages: i, Seconds: float64(i)}
}

func TestStateSamplesKeepTheLastTen(t *testing.T) {
	var s State
	for i := 1; i <= 14; i++ {
		s.AddUpgradeSample(sample(i))
		s.AddPrefetchSample(sample(i))
	}
	for name, got := range map[string][]bandwidth.Sample{"upgrade": s.UpgradeSamples(), "prefetch": s.PrefetchSamples()} {
		if len(got) != 10 || got[0].Packages != 5 || got[9].Packages != 14 {
			t.Errorf("%s samples = %d long, first %d last %d; want the last 10 (5..14)", name, len(got), got[0].Packages, got[len(got)-1].Packages)
		}
	}
	// A sample that measured nothing is dropped.
	s.AddUpgradeSample(bandwidth.Sample{Bytes: 5, Seconds: 0})
	if len(s.UpgradeSamples()) != 10 {
		t.Error("a sample with no duration was kept")
	}
}

func TestStateRecordPending(t *testing.T) {
	var s State
	if !s.RecordPending("2026-10-01", 100) {
		t.Fatal("first sample not recorded")
	}
	if s.RecordPending("2026-10-01", 100) {
		t.Error("an identical sample reported a change")
	}
	if !s.RecordPending("2026-10-01", 150) || len(s.PendingSizes()) != 1 || s.PendingSizes()[0].Bytes != 150 {
		t.Errorf("same-day sample should replace: %+v", s.PendingSizes())
	}
	if s.RecordPending("2026-09-30", 1) || s.RecordPending("garbage", 1) || s.RecordPending("2026-10-02", -1) {
		t.Error("a bad or out-of-order sample was recorded")
	}
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 100 {
		s.RecordPending(day.AddDate(0, 0, i+400).Format(bandwidth.DayFormat), int64(i))
	}
	got := s.PendingSizes()
	if len(got) != 90 || got[89].Bytes != 99 {
		t.Errorf("kept %d samples, last %+v; want 90 ending at 99", len(got), got[len(got)-1])
	}
}

func TestStateBandwidthRoundTripAndClone(t *testing.T) {
	var s State
	s.AddUpgradeSample(sample(1))
	s.AddPrefetchSample(sample(2))
	s.RecordPending("2026-10-01", 5)
	fsys := system.NewFakeFS(nil)
	if err := SaveState(fsys, statePath, s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(fsys, statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range []State{got, s.Clone()} {
		if !reflect.DeepEqual(st.UpgradeSamples(), s.UpgradeSamples()) ||
			!reflect.DeepEqual(st.PrefetchSamples(), s.PrefetchSamples()) ||
			!reflect.DeepEqual(st.PendingSizes(), s.PendingSizes()) {
			t.Errorf("records lost: %+v", st)
		}
	}
	// A clone is independent of the original.
	c := s.Clone()
	c.AddUpgradeSample(sample(3))
	if len(s.UpgradeSamples()) != 1 {
		t.Error("clone shares samples with the original")
	}
}

func TestStateV2FileWithoutBandwidthRecordsLoads(t *testing.T) {
	fsys := system.NewFakeFS(map[string][]byte{statePath: []byte(`{"version":2,"read_news":["a"]}`)})
	got, err := LoadState(fsys, statePath)
	if err != nil || len(got.UpgradeSamples()) != 0 || !got.IsRead("a") {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestBandwidthConfig(t *testing.T) {
	cfg, err := Parse([]byte("metered_warn_mib = 250\nprefetch_command = \"sudo checkupdates -d\"\n"))
	if err != nil || cfg.MeteredWarnMiB != 250 || cfg.PrefetchCommand != "sudo checkupdates -d" {
		t.Fatalf("got %+v, %v", cfg, err)
	}
	if Default().MeteredWarnMiB != 100 {
		t.Errorf("default metered_warn_mib = %d, want 100", Default().MeteredWarnMiB)
	}
	cfg, err = Parse([]byte("metered_warn_mib = -" + strconv.Itoa(5) + "\n"))
	if err == nil || cfg.MeteredWarnMiB != 100 {
		t.Errorf("negative value: got %d, %v; want the default and an error", cfg.MeteredWarnMiB, err)
	}
	cfg, err = Parse([]byte("metered_warn_mib = 0\n"))
	if err != nil || cfg.MeteredWarnMiB != 0 {
		t.Errorf("0 must be allowed (check off): got %d, %v", cfg.MeteredWarnMiB, err)
	}
}
