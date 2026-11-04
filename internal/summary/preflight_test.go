package summary

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/preflight"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/system"
)

// preflightSources is the fixture system with the preflight checks wired
// in. Its news is marked read, so only preflight can raise exit code 30.
func preflightSources() (Sources, *system.FakeFS, *system.FakeDisk, *system.FakeFetcher) {
	src := fixtureSources()
	fsys, disk, f := fixtures.FS(), fixtures.Disk(), fixtures.Fetcher()
	src.FS, src.Disk, src.Fetcher = fsys, disk, f
	src.State.LastUpgrade = fixtures.Now.Add(-time.Hour)
	return src, fsys, disk, f
}

func mountBoot(t *testing.T, fsys *system.FakeFS) {
	t.Helper()
	mounts := string(fixtures.Bytes(fixtures.ProcSelfMounts)) + "/dev/nvme0n1p1 /boot vfat rw,relatime 0 0\n"
	if err := fsys.WriteFile("/proc/self/mounts", []byte(mounts), 0o644); err != nil {
		t.Fatal(err)
	}
}

func checks(ps []preflight.Problem) []preflight.ID {
	var ids []preflight.ID
	for _, p := range ps {
		ids = append(ids, p.Check)
	}
	return ids
}

func TestGatherPreflight(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, src *Sources, fsys *system.FakeFS, disk *system.FakeDisk, f *system.FakeFetcher)
		want  []preflight.ID
		code  int
	}{
		{"boot unmounted is critical on its own", func(*testing.T, *Sources, *system.FakeFS, *system.FakeDisk, *system.FakeFetcher) {},
			[]preflight.ID{preflight.CheckBootUnmounted}, ExitIntervention},
		{"healthy", func(t *testing.T, _ *Sources, fsys *system.FakeFS, _ *system.FakeDisk, _ *system.FakeFetcher) {
			mountBoot(t, fsys)
		}, nil, ExitAttention},
		{"boot full", func(t *testing.T, _ *Sources, fsys *system.FakeFS, disk *system.FakeDisk, _ *system.FakeFetcher) {
			mountBoot(t, fsys)
			disk.Set("/boot", system.DiskUsage{FreeBytes: 1000, ID: "esp"})
		}, []preflight.ID{preflight.CheckSpace}, ExitIntervention},
		{"root too small for the download", func(t *testing.T, _ *Sources, fsys *system.FakeFS, disk *system.FakeDisk, _ *system.FakeFetcher) {
			mountBoot(t, fsys)
			disk.Set("/", system.DiskUsage{FreeBytes: 100 << 20, ID: "root"})
		}, []preflight.ID{preflight.CheckSpace}, ExitIntervention},
		{"stale lock and reboot pending are warnings", func(t *testing.T, _ *Sources, fsys *system.FakeFS, _ *system.FakeDisk, _ *system.FakeFetcher) {
			mountBoot(t, fsys)
			_ = fsys.WriteFile("/var/lib/pacman/db.lck", nil, 0o644)
			_ = fsys.WriteFile("/proc/sys/kernel/osrelease", []byte("6.16.9-arch1-1\n"), 0o644)
		}, []preflight.ID{preflight.CheckLock, preflight.CheckReboot}, ExitAttention},
		{"mirror behind", func(t *testing.T, _ *Sources, fsys *system.FakeFS, _ *system.FakeDisk, f *system.FakeFetcher) {
			mountBoot(t, fsys)
			f.Set(fixtures.MirrorLastUpdateURL, system.FakeResponse{Body: []byte("1792700000\n")})
		}, []preflight.ID{preflight.CheckMirror}, ExitAttention},
		{"mirror within the limit", func(t *testing.T, src *Sources, fsys *system.FakeFS, _ *system.FakeDisk, f *system.FakeFetcher) {
			mountBoot(t, fsys)
			f.Set(fixtures.MirrorLastUpdateURL, system.FakeResponse{Body: []byte("1792700000\n")})
			src.Config.MirrorLagHours = 24
		}, nil, ExitAttention},
		{"mirror down is an error, not a problem", func(t *testing.T, _ *Sources, fsys *system.FakeFS, _ *system.FakeDisk, f *system.FakeFetcher) {
			mountBoot(t, fsys)
			f.Set(fixtures.MirrorLastUpdateURL, system.FakeResponse{Status: 503})
		}, nil, ExitError},
		{"critical and warning together, most severe first", func(_ *testing.T, _ *Sources, fsys *system.FakeFS, _ *system.FakeDisk, _ *system.FakeFetcher) {
			_ = fsys.WriteFile("/var/lib/pacman/db.lck", nil, 0o644)
		}, []preflight.ID{preflight.CheckBootUnmounted, preflight.CheckLock}, ExitIntervention},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, fsys, disk, f := preflightSources()
			tt.setup(t, &src, fsys, disk, f)
			s := Gather(context.Background(), src)
			got := checks(s.Preflight)
			if strings.Join(idStrings(got), ",") != strings.Join(idStrings(tt.want), ",") {
				t.Errorf("checks = %v, want %v\n%+v", got, tt.want, s.Preflight)
			}
			if code := s.ExitCode(); code != tt.code {
				t.Errorf("exit code %d, want %d (errors %v)", code, tt.code, s.Errors)
			}
		})
	}
}

func idStrings(ids []preflight.ID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return out
}

func TestPreflightSwitches(t *testing.T) {
	t.Run("preflight off makes no requests and reads nothing", func(t *testing.T) {
		src, _, disk, f := preflightSources()
		src.Config.Preflight = false
		s := Gather(context.Background(), src)
		if len(s.Preflight) != 0 || len(disk.Calls()) != 0 {
			t.Errorf("problems %v, statfs calls %v", s.Preflight, disk.Calls())
		}
		for _, c := range f.Calls() {
			if strings.HasSuffix(c.URL, "/lastupdate") {
				t.Errorf("fetched %s with preflight off", c.URL)
			}
		}
		if s.ExitCode() != ExitAttention {
			t.Errorf("exit code %d", s.ExitCode())
		}
	})
	t.Run("mirror_check off skips only the mirror", func(t *testing.T) {
		src, _, disk, f := preflightSources()
		src.Config.MirrorCheck = false
		s := Gather(context.Background(), src)
		if len(s.Preflight) != 1 || len(disk.Calls()) == 0 {
			t.Errorf("problems %v, statfs calls %v", s.Preflight, disk.Calls())
		}
		for _, c := range f.Calls() {
			if strings.HasSuffix(c.URL, "/lastupdate") {
				t.Errorf("fetched %s with mirror_check off", c.URL)
			}
		}
	})
	t.Run("no disk means no preflight", func(t *testing.T) {
		src, _, _, _ := preflightSources()
		src.Disk = nil
		if s := Gather(context.Background(), src); len(s.Preflight) != 0 || len(s.Errors) != 0 {
			t.Errorf("problems %v, errors %v", s.Preflight, s.Errors)
		}
	})
}

func TestPreflightOutput(t *testing.T) {
	src, _, _, _ := preflightSources()
	s := Gather(context.Background(), src)

	text := s.Text()
	if !strings.Contains(text, "‼ preflight: /boot is in fstab but not mounted") {
		t.Errorf("no preflight line in:\n%s", text)
	}
	// Critical problems come before warnings.
	if strings.Index(text, "‼ preflight") > strings.Index(text, "⚠ ") {
		t.Errorf("preflight problem not ordered by severity:\n%s", text)
	}

	var got struct {
		Critical      int  `json:"critical"`
		RebootPending bool `json:"reboot_pending"`
		Preflight     []struct{ Severity, Message string }
	}
	if err := json.Unmarshal(s.JSON(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Preflight) != 1 || got.Preflight[0].Severity != risk.Critical.String() {
		t.Errorf("preflight JSON = %+v", got.Preflight)
	}
	if got.Critical != 0 {
		t.Errorf("critical = %d: it keeps counting news items only", got.Critical)
	}
	if s.Class() != "critical" {
		t.Errorf("class %q", s.Class())
	}
}

func TestPreflightRebootPendingJSON(t *testing.T) {
	src, fsys, _, _ := preflightSources()
	_ = fsys.WriteFile("/proc/sys/kernel/osrelease", []byte("6.16.9-arch1-1\n"), 0o644)
	var got struct {
		RebootPending bool `json:"reboot_pending"`
	}
	if err := json.Unmarshal(Gather(context.Background(), src).JSON(), &got); err != nil || !got.RebootPending {
		t.Errorf("reboot_pending = %v, err %v", got.RebootPending, err)
	}
}
