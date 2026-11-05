package bandwidth

import (
	"testing"
	"time"
)

var day0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestBehind(t *testing.T) {
	now := day0.AddDate(0, 0, 47).Add(3 * time.Hour)
	tests := []struct {
		name   string
		last   time.Time
		want   int
		wantOK bool
	}{
		{"unknown", time.Time{}, 0, false},
		{"47 days", day0, 47, true},
		{"under a day", now.Add(-23 * time.Hour), 0, true},
		{"exactly a day", now.Add(-24 * time.Hour), 1, true},
		{"in the future", now.Add(time.Hour), 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Behind(tt.last, now)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("Behind = %d, %v; want %d, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func size(day string, mib int64) Size { return Size{Day: day, Bytes: mib << 20} }

func TestTrend(t *testing.T) {
	tests := []struct {
		name      string
		sizes     []Size
		wantBytes int64
		wantOK    bool
	}{
		{"none", nil, 0, false},
		{"one sample", []Size{size("2026-10-01", 100)}, 0, false},
		{"under a week", []Size{size("2026-10-01", 100), size("2026-10-07", 400)}, 0, false},
		{"exactly a week", []Size{size("2026-10-01", 100), size("2026-10-08", 350)}, 250 << 20, true},
		{"two weeks", []Size{size("2026-10-01", 100), size("2026-10-08", 350), size("2026-10-15", 600)}, 250 << 20, true},
		{
			"uneven spacing scales to a week",
			[]Size{size("2026-10-01", 0), size("2026-10-04", 300), size("2026-10-15", 1400)}, 700 << 20, true,
		},
		{
			"an upgrade resets the run",
			[]Size{size("2026-09-01", 100), size("2026-09-15", 2000), size("2026-09-16", 0), size("2026-09-20", 100), size("2026-10-01", 300)},
			(300 << 20) * 7 / 15, true,
		},
		{"upgraded yesterday", []Size{size("2026-09-01", 100), size("2026-09-20", 2000), size("2026-09-21", 10)}, 0, false},
		{"not growing", []Size{size("2026-10-01", 500), size("2026-10-10", 500)}, 0, false},
		{"bad date", []Size{{Day: "later", Bytes: 1}, size("2026-10-10", 500)}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Trend(tt.sizes)
			if ok != tt.wantOK {
				t.Fatalf("Trend ok = %v, want %v (got %d)", ok, tt.wantOK, got)
			}
			if ok && got != tt.wantBytes {
				t.Errorf("Trend = %d bytes, want %d", got, tt.wantBytes)
			}
		})
	}
}

// s builds a sample of mib MiB, in secs seconds, for pkgs packages.
func s(mib int64, secs float64, pkgs int) Sample {
	return Sample{Time: day0, Bytes: mib << 20, Seconds: secs, Packages: pkgs}
}

func TestEstimateTime(t *testing.T) {
	tests := []struct {
		name          string
		prefetch      []Sample
		upgrades      []Sample
		bytes         int64
		packages      int
		wantOK        bool
		want          time.Duration
		pre, up       int
		combined, dlo bool
	}{
		{name: "no samples", bytes: 1 << 30, packages: 100},
		{
			name:     "one prefetch only: download time, no install",
			prefetch: []Sample{s(100, 100, 10)}, // 1 MiB/s
			bytes:    600 << 20, packages: 50,
			wantOK: true, want: 600 * time.Second, pre: 1, dlo: true,
		},
		{
			name:     "prefetch and upgrade: install time per package added",
			prefetch: []Sample{s(100, 100, 10)}, // 1 MiB/s
			// 200 MiB takes 200 s to download, so 100 s of install for 20 packages: 5 s each.
			upgrades: []Sample{s(200, 300, 20)},
			bytes:    500 << 20, packages: 40,
			wantOK: true, want: 500*time.Second + 200*time.Second, pre: 1, up: 1,
		},
		{
			name:     "median of prefetch speeds ignores an outlier",
			prefetch: []Sample{s(100, 100, 1), s(100, 100, 1), s(100, 1, 1)}, // 1, 1 and 100 MiB/s
			bytes:    100 << 20,
			wantOK:   true, want: 100 * time.Second, pre: 3, dlo: true,
		},
		{
			name:     "median of an even count is the mean of the middle two",
			prefetch: []Sample{s(100, 100, 1), s(100, 50, 1)}, // 1 and 2 MiB/s: 1.5
			bytes:    150 << 20,
			wantOK:   true, want: 100 * time.Second, pre: 2, dlo: true,
		},
		{
			name:     "an upgrade that was faster than its download adds no negative install time",
			prefetch: []Sample{s(100, 100, 1)},
			upgrades: []Sample{s(200, 100, 10)},
			bytes:    100 << 20, packages: 10,
			wantOK: true, want: 100 * time.Second, pre: 1, up: 1,
		},
		{
			name:     "samples too small to measure are ignored",
			prefetch: []Sample{s(0, 5, 1), {Bytes: 5 << 20, Seconds: 0, Packages: 1}},
			bytes:    100 << 20,
		},
		{
			name:     "upgrades only: whole-upgrade speed, install time included",
			upgrades: []Sample{s(100, 200, 10), s(300, 300, 30), s(100, 100, 5)}, // 0.5, 1, 1 MiB/s
			bytes:    300 << 20, packages: 60,
			wantOK: true, want: 300 * time.Second, up: 3, combined: true,
		},
		{
			name:     "nothing left to download after a prefetch",
			prefetch: []Sample{s(100, 100, 10)},
			upgrades: []Sample{s(100, 150, 10)}, // 50 s of install for 10 packages
			bytes:    0, packages: 20,
			wantOK: true, want: 100 * time.Second, pre: 1, up: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTime(tt.prefetch, tt.upgrades, tt.bytes, tt.packages)
			if got.OK != tt.wantOK {
				t.Fatalf("OK = %v, want %v (%+v)", got.OK, tt.wantOK, got)
			}
			if !tt.wantOK {
				return
			}
			if diff := got.Duration - tt.want; diff < -time.Second || diff > time.Second {
				t.Errorf("Duration = %v, want %v", got.Duration, tt.want)
			}
			if got.Prefetches != tt.pre || got.Upgrades != tt.up || got.Combined != tt.combined || got.DownloadOnly != tt.dlo {
				t.Errorf("basis = %+v, want pre=%d up=%d combined=%v downloadOnly=%v", got, tt.pre, tt.up, tt.combined, tt.dlo)
			}
		})
	}
}

func TestEstimateText(t *testing.T) {
	tests := []struct {
		name string
		e    Estimate
		want string
	}{
		{"none", Estimate{}, "no estimate yet"},
		{"upgrades", Estimate{OK: true, Duration: 14 * time.Minute, Upgrades: 6, Combined: true}, "~14 min (from your last 6 upgrades)"},
		{"one upgrade", Estimate{OK: true, Duration: 90 * time.Second, Upgrades: 1, Combined: true}, "~2 min (from your last 1 upgrade)"},
		{"both", Estimate{OK: true, Duration: 80 * time.Minute, Prefetches: 2, Upgrades: 6}, "~1 h 20 min (from your last 2 prefetches and 6 upgrades)"},
		{"download only", Estimate{OK: true, Duration: 3 * time.Minute, Prefetches: 1, DownloadOnly: true}, "~3 min (download only, from your last 1 prefetch)"},
		{"under a minute", Estimate{OK: true, Duration: 10 * time.Second, Upgrades: 3, Combined: true}, "<1 min (from your last 3 upgrades)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.Text(); got != tt.want {
				t.Errorf("Text = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "<1 min"},
		{29 * time.Second, "<1 min"},
		{30 * time.Second, "1 min"},
		{14*time.Minute + 20*time.Second, "14 min"},
		{59 * time.Minute, "59 min"},
		{61 * time.Minute, "1 h"},
		{62*time.Minute + 40*time.Second, "1 h 5 min"},
		{2 * time.Hour, "2 h"},
		{2*time.Hour + 20*time.Minute, "2 h 20 min"},
	}
	for _, tt := range tests {
		if got := Duration(tt.d); got != tt.want {
			t.Errorf("Duration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
