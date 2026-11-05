package summary

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/bandwidth"
	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/system"
)

func TestLoadMetered(t *testing.T) {
	route := fixtures.Bytes(fixtures.ProcNetRoute)
	tests := []struct {
		name  string
		route []byte
		nmcli system.FakeResult
		want  MeteredInfo
	}{
		{"metered", route, system.FakeResult{Stdout: fixtures.Bytes(fixtures.NmcliMeteredYes)}, MeteredInfo{true, "wlan0"}},
		{"metered, guessed", route, system.FakeResult{Stdout: fixtures.Bytes(fixtures.NmcliMeteredGuessed)}, MeteredInfo{true, "wlan0"}},
		{"not metered", route, system.FakeResult{Stdout: fixtures.Bytes(fixtures.NmcliMeteredNo)}, MeteredInfo{false, "wlan0"}},
		{"unknown", route, system.FakeResult{Stdout: fixtures.Bytes(fixtures.NmcliMeteredUnknown)}, MeteredInfo{false, "wlan0"}},
		{"no nmcli: skipped silently", route, system.FakeResult{Err: system.ErrCommandNotFound}, MeteredInfo{false, "wlan0"}},
		{"NetworkManager not running", route, system.FakeResult{Code: 8, Stderr: []byte("Error: NetworkManager is not running.")}, MeteredInfo{false, "wlan0"}},
		{"no default route: nmcli is not run", []byte("Iface\tDestination\n"), system.FakeResult{}, MeteredInfo{}},
		{"no route file: nmcli is not run", nil, system.FakeResult{}, MeteredInfo{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{}
			if tt.route != nil {
				files[RoutePath] = tt.route
			}
			r := system.NewFakeRunner()
			r.Set(fixtures.NmcliMeteredCommand, tt.nmcli)
			got := LoadMetered(context.Background(), r, system.NewFakeFS(files))
			if got != tt.want {
				t.Errorf("LoadMetered = %+v, want %+v", got, tt.want)
			}
			ran := len(r.Calls()) > 0
			if wantRan := tt.want.Device != ""; ran != wantRan {
				t.Errorf("nmcli ran = %v (%q), want %v", ran, r.Calls(), wantRan)
			}
		})
	}
}

func TestGatherMeteredWarning(t *testing.T) {
	src := fixtureSources()
	src.Disk = fixtures.Disk()
	r := fixtures.Runner()
	r.Set(fixtures.NmcliMeteredCommand, system.FakeResult{Stdout: fixtures.Bytes(fixtures.NmcliMeteredYes)})
	src.Runner = r

	s := Gather(context.Background(), src)
	if !s.Metered.Metered || !hasProblem(s, "metered") {
		t.Fatalf("metered %+v, problems %+v", s.Metered, s.Preflight)
	}
	var j map[string]any
	if err := json.Unmarshal(s.JSON(), &j); err != nil || j["metered"] != true {
		t.Errorf("json metered = %v (%v)", j["metered"], err)
	}

	// The limit is configurable, and 0 turns the check off with no nmcli call.
	src.Config.MeteredWarnMiB = 5000
	if s := Gather(context.Background(), src); hasProblem(s, "metered") {
		t.Errorf("a 5000 MiB limit still warns: %+v", s.Preflight)
	}
	off := fixtureSources()
	off.Disk = fixtures.Disk()
	off.Config.MeteredWarnMiB = 0
	r2 := fixtures.Runner()
	off.Runner = r2
	s = Gather(context.Background(), off)
	for _, c := range r2.Calls() {
		if strings.HasPrefix(c, "nmcli") {
			t.Errorf("metered_warn_mib = 0 still ran %q", c)
		}
	}
	if s.Metered.Metered {
		t.Error("metered with the check off")
	}
}

func hasProblem(s Summary, check string) bool {
	for _, p := range s.Preflight {
		if string(p.Check) == check {
			return true
		}
	}
	return false
}

func TestGatherBandwidthAndETA(t *testing.T) {
	src := fixtureSources()
	s := Gather(context.Background(), src)
	if !s.Bandwidth.Shown() || s.Bandwidth.DaysBehind != 2 || s.Bandwidth.Estimate.OK {
		t.Fatalf("bandwidth %+v", s.Bandwidth)
	}
	if !strings.Contains(s.Text(), "bandwidth: 2 days behind · 18 packages") || !strings.Contains(s.Text(), "no estimate yet") {
		t.Errorf("text lacks the bandwidth line:\n%s", s.Text())
	}
	var j map[string]any
	_ = json.Unmarshal(s.JSON(), &j)
	if _, has := j["eta_seconds"]; has {
		t.Error("eta_seconds present without samples")
	}

	for _, x := range fixtures.DemoPrefetchSamples() {
		src.State.AddPrefetchSample(x)
	}
	for _, x := range fixtures.DemoUpgradeSamples() {
		src.State.AddUpgradeSample(x)
	}
	s = Gather(context.Background(), src)
	_ = json.Unmarshal(s.JSON(), &j)
	eta, has := j["eta_seconds"].(float64)
	if !has || eta <= 0 || int64(eta) != int64(s.Bandwidth.Estimate.Duration.Seconds()+0.5) && int64(eta) != int64(s.Bandwidth.Estimate.Duration.Seconds()) {
		t.Errorf("eta_seconds = %v, estimate %v", j["eta_seconds"], s.Bandwidth.Estimate.Duration)
	}
}

func TestBandwidthParts(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	last := now.AddDate(0, 0, -47)
	tx := pacman.Transaction{
		Updates:       make([]pacman.UpdateDetail, 300),
		New:           make([]pacman.NewPackage, 12),
		DownloadBytes: 1800 << 20,
	}
	var st config.State
	st.RecordPending("2026-11-06", 1000<<20)
	st.RecordPending("2026-11-20", 1800<<20)
	st.AddUpgradeSample(bandwidth.Sample{Bytes: 600 << 20, Packages: 100, Seconds: 600})

	got := Bandwidth(tx, last, now, st).Parts()
	want := []string{"47 days behind", "312 packages", "1.8 GiB", "+~400 MiB/week"}
	if !reflect.DeepEqual(got[:4], want) || !strings.HasPrefix(got[4], "~") || !strings.Contains(got[4], "1 upgrade") {
		t.Errorf("parts = %q, want %q then an estimate", got, want)
	}

	// Prefetched: the size part becomes "X of Y already downloaded".
	tx.DownloadBytes, tx.PrefetchedBytes = 100<<20, 1700<<20
	got = Bandwidth(tx, last, now, config.State{}).Parts()
	if got[2] != "1.7 of 1.8 GiB already downloaded" {
		t.Errorf("size part = %q", got[2])
	}
	tx.DownloadBytes, tx.PrefetchedBytes = 1000<<20, 200<<20
	if got = Bandwidth(tx, last, now, config.State{}).Parts(); got[2] != "200 MiB of 1.2 GiB already downloaded" {
		t.Errorf("mixed units = %q", got[2])
	}

	// Unknown last upgrade and a current system.
	if got = Bandwidth(tx, time.Time{}, now, config.State{}).Parts(); strings.Contains(got[0], "behind") {
		t.Errorf("unknown last upgrade shows %q", got[0])
	}
	if Bandwidth(pacman.Transaction{}, last, now, config.State{}).Shown() {
		t.Error("an empty transaction shows a bandwidth line")
	}
}

func TestRecordSamples(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 11, 20, 23, 30, 0, 0, loc)
	tx := pacman.Transaction{New: make([]pacman.NewPackage, 3), Updates: make([]pacman.UpdateDetail, 7), DownloadBytes: 50 << 20, PrefetchedBytes: 50 << 20}
	var st config.State
	if !RecordPending(&st, tx, now, loc) || RecordPending(&st, tx, now, loc) {
		t.Error("RecordPending should report a change only the first time")
	}
	if got := st.PendingSizes(); len(got) != 1 || got[0].Bytes != 100<<20 || got[0].Day != "2026-11-20" {
		t.Errorf("pending sizes = %+v (the whole download, cached included)", got)
	}
	RecordUpgrade(&st, tx, now, now.Add(10*time.Minute))
	RecordPrefetch(&st, tx, now, now.Add(100*time.Second))
	u, p := st.UpgradeSamples(), st.PrefetchSamples()
	if len(u) != 1 || u[0].Packages != 10 || u[0].Bytes != 50<<20 || u[0].Seconds != 600 || len(p) != 1 || p[0].Seconds != 100 {
		t.Errorf("samples: %+v %+v", u, p)
	}
	// A zero-length hand-off tells nothing and is not kept.
	RecordUpgrade(&st, tx, now, now)
	if len(st.UpgradeSamples()) != 1 {
		t.Error("a zero-duration sample was kept")
	}
}

func TestPrefetchArgv(t *testing.T) {
	cfg := config.Default()
	root := system.NewFakeFS(map[string][]byte{pacman.ConfPath: []byte("[options]\nCacheDir = /var/cache/pacman/pkg/\n")})
	got, err := PrefetchArgv(root, cfg)
	if err != nil || !reflect.DeepEqual(got, []string{"sudo", "checkupdates", "-d"}) {
		t.Errorf("root-owned cache: %q, %v", got, err)
	}

	user := system.NewFakeFS(map[string][]byte{pacman.ConfPath: []byte("[options]\nCacheDir = /home/me/cache/\n")})
	user.SetWritable("/home/me/cache/")
	got, err = PrefetchArgv(user, cfg)
	if err != nil || !reflect.DeepEqual(got, []string{"checkupdates", "-d"}) {
		t.Errorf("writable cache: %q, %v", got, err)
	}

	cfg.PrivilegeCommand = "doas"
	if got, _ = PrefetchArgv(root, cfg); !reflect.DeepEqual(got, []string{"doas", "checkupdates", "-d"}) {
		t.Errorf("doas: %q", got)
	}
	cfg.PrefetchCommand = "my-fetch --now"
	if got, _ = PrefetchArgv(root, cfg); !reflect.DeepEqual(got, []string{"my-fetch", "--now"}) {
		t.Errorf("prefetch_command: %q", got)
	}
	// An unreadable pacman.conf means the default cache directory.
	if got, _ = PrefetchArgv(system.NewFakeFS(nil), config.Default()); got[0] != "sudo" {
		t.Errorf("no pacman.conf: %q", got)
	}
}

func TestParallelDownloadsHint(t *testing.T) {
	conf := func(s string) system.FS {
		return system.NewFakeFS(map[string][]byte{pacman.ConfPath: []byte(s)})
	}
	if h := ParallelDownloadsHint(conf("[options]\n")); !strings.Contains(h, "ParallelDownloads = 5") || !strings.Contains(h, "one package at a time") {
		t.Errorf("missing key: %q", h)
	}
	if h := ParallelDownloadsHint(conf("[options]\nParallelDownloads = 1\n")); h == "" {
		t.Error("ParallelDownloads = 1 still downloads sequentially")
	}
	if h := ParallelDownloadsHint(conf("[options]\nParallelDownloads = 5\n")); h != "" {
		t.Errorf("already set: %q", h)
	}
	if h := ParallelDownloadsHint(system.NewFakeFS(nil)); h != "" {
		t.Errorf("unreadable pacman.conf: %q", h)
	}
}
