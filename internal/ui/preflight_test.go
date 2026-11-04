package ui

import (
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/system"
)

// preflightOptions is the fixture system with the preflight checks on: its
// /boot is in fstab but not mounted, and a kernel is updating.
func preflightOptions() (Options, *system.FakeFS) {
	opts := fixtureOptions()
	fsys := fixtures.FS()
	opts.FS = fsys
	opts.Disk = fixtures.Disk()
	return opts, fsys
}

func TestPreflightBanner(t *testing.T) {
	const w, h = 100, 30
	opts, _ := preflightOptions()
	m := loaded(t, opts, w, h)

	out := plain(m)
	if !strings.Contains(out, "‼ /boot is in fstab but not mounted while linux updates:") {
		t.Errorf("no /boot banner:\n%s", out)
	}
	golden(t, "preflight-boot", out)
	checkSize(t, m, w, h)
	if len(m.pf.problems) != 1 {
		t.Errorf("problems = %+v", m.pf.problems)
	}
}

func TestPreflightSeveralProblemsCollapse(t *testing.T) {
	const w, h = 100, 30
	opts, fsys := preflightOptions()
	_ = fsys.WriteFile("/var/lib/pacman/db.lck", nil, 0o644)
	m := loaded(t, opts, w, h)

	out := plain(m)
	if !strings.Contains(out, "‼ 2 preflight problems (U lists them): /boot is in fstab") {
		t.Errorf("problems didn't collapse into one line:\n%s", out)
	}
	if strings.Count(out, "preflight problem") != 1 {
		t.Errorf("want exactly one banner line:\n%s", out)
	}
	checkSize(t, m, w, h)

	// The upgrade confirmation lists every problem, in full.
	c := send(t, m, press("U"))
	conf := plain(c)
	for _, want := range []string{"2 preflight problems", "‼ /boot is in fstab", "⚠ pacman's database lock", "pgrep -x pacman"} {
		if !strings.Contains(conf, want) {
			t.Errorf("confirm lacks %q:\n%s", want, conf)
		}
	}
	golden(t, "preflight-confirm", conf)
	checkSize(t, c, w, h)
}

func TestPreflightQuietWhenHealthyOrOff(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		opts, fsys := preflightOptions()
		mounts := string(fixtures.Bytes(fixtures.ProcSelfMounts)) + "/dev/nvme0n1p1 /boot vfat rw 0 0\n"
		_ = fsys.WriteFile("/proc/self/mounts", []byte(mounts), 0o644)
		m := loaded(t, opts, 100, 30)
		if len(m.pf.problems) != 0 || strings.Contains(plain(m), "preflight") {
			t.Errorf("problems %+v:\n%s", m.pf.problems, plain(m))
		}
	})
	t.Run("preflight off", func(t *testing.T) {
		opts, _ := preflightOptions()
		cfg := config.Default()
		cfg.Preflight = false
		opts.Config = &cfg
		disk := fixtures.Disk()
		opts.Disk = disk
		m := loaded(t, opts, 100, 30)
		if len(m.pf.problems) != 0 || len(disk.Calls()) != 0 {
			t.Errorf("problems %+v, statfs calls %v", m.pf.problems, disk.Calls())
		}
	})
	t.Run("mirror_check off makes no mirror requests", func(t *testing.T) {
		opts, _ := preflightOptions()
		cfg := config.Default()
		cfg.MirrorCheck = false
		opts.Config = &cfg
		f := fixtures.Fetcher()
		opts.Fetcher = f
		loaded(t, opts, 100, 30)
		for _, c := range f.Calls() {
			if strings.HasSuffix(c.URL, "/lastupdate") {
				t.Errorf("fetched %s", c.URL)
			}
		}
	})
}

func TestPreflightMirrorBehindAndFailing(t *testing.T) {
	t.Run("behind", func(t *testing.T) {
		opts, fsys := preflightOptions()
		mounts := string(fixtures.Bytes(fixtures.ProcSelfMounts)) + "/dev/nvme0n1p1 /boot vfat rw 0 0\n"
		_ = fsys.WriteFile("/proc/self/mounts", []byte(mounts), 0o644)
		f := fixtures.Fetcher()
		f.Set(fixtures.MirrorLastUpdateURL, system.FakeResponse{Body: []byte("1792700000\n")})
		opts.Fetcher = f
		m := loaded(t, opts, 120, 30)
		if !strings.Contains(plain(m), "⚠ Your mirror (https://mirror.example.org/archlinux) is 16 hours behind") {
			t.Errorf("no mirror banner:\n%s", plain(m))
		}
	})
	t.Run("failing source shows its error and blocks nothing", func(t *testing.T) {
		opts, _ := preflightOptions()
		f := fixtures.Fetcher()
		f.Set(fixtures.MirrorLastUpdateURL, system.FakeResponse{Status: 503})
		opts.Fetcher = f
		m := loaded(t, opts, 120, 50)
		out := plain(m)
		if !strings.Contains(out, "✗ mirror:") {
			t.Errorf("no mirror error in the list:\n%s", out)
		}
		if !strings.Contains(out, "/boot is in fstab") || !strings.Contains(out, "REPO (") {
			t.Errorf("the rest of the UI is affected:\n%s", out)
		}
	})
}
