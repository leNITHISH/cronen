package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/system"
)

const partialWarning = "Your package database was synced without upgrading"

func TestPartialBannerFromRealDatabase(t *testing.T) {
	opts := fixtureOptions()
	r := fixtures.Runner()
	r.Set("pacman -Qu", system.FakeResult{Stdout: []byte("zlib 1.3-1 -> 1.3.1-1\n")})
	opts.Runner = r
	m := loaded(t, opts, 140, 40)
	if out := plain(m); !strings.Contains(out, partialWarning) {
		t.Errorf("no partial-upgrade banner:\n%s", out)
	}

	// Nothing pending in the real database: no banner.
	m = loaded(t, fixtureOptions(), 140, 40)
	if out := plain(m); strings.Contains(out, partialWarning) {
		t.Errorf("banner without a synced database:\n%s", out)
	}

	// Switched off: pacman -Qu is never run.
	cfg := config.Default()
	cfg.PartialCheck = false
	opts.Config = &cfg
	r2 := fixtures.Runner()
	r2.Set("pacman -Qu", system.FakeResult{Stdout: []byte("zlib 1.3-1 -> 1.3.1-1\n")})
	opts.Runner = r2
	m = loaded(t, opts, 140, 40)
	for _, c := range r2.Calls() {
		if c == "pacman -Qu" {
			t.Error("ran pacman -Qu with partial_check = false")
		}
	}
	if strings.Contains(plain(m), partialWarning) {
		t.Error("banner with partial_check = false")
	}
}

// hk is the housekeeping screen on a tall terminal, so the whole of the
// partial-upgrade section shows.
func hk(t *testing.T, opts Options) Model {
	t.Helper()
	return send(t, loaded(t, opts, 140, 100), press("5"))
}

func TestLinkageScanScreen(t *testing.T) {
	opts, runner, _ := realOptions()
	opts.Disk = nil
	runner.Set("pacman -Qu", system.FakeResult{Stdout: []byte("zlib 1.3-1 -> 1.3.1-1\n")})
	m := hk(t, opts)
	if !strings.Contains(plain(m), "not scanned") {
		t.Fatalf("before the scan:\n%s", plain(m))
	}
	m = send(t, m, press("B"))
	if m.scanning() {
		t.Fatal("scan still running")
	}
	out := plain(m)
	for _, want := range []string{
		"/usr/bin/paru (paru 2.1.0-1) needs libalpm.so.15, which no installed package provides",
		"upgrade first, then rebuild", // 16 repo updates are pending
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// The damage makes the synced-database warning critical.
	if !strings.Contains(out, "‼ "+partialWarning) || m.part.info.Broken() != 1 {
		t.Errorf("banner not critical:\n%s", out)
	}
	// With repo updates pending, b offers nothing.
	m = send(t, m, press("b"))
	if m.act != nil {
		t.Fatal("rebuild offered while repo updates are pending")
	}
}

func TestRebuildHandOff(t *testing.T) {
	opts, runner, _ := realOptions()
	runner.Set("checkupdates", system.FakeResult{Code: 2})
	cfg := config.Default()
	cfg.AURHelper = "paru"
	opts.Config = &cfg
	ex := &fakeExec{}
	opts.Exec = ex.exec

	m := hk(t, opts)
	m = send(t, m, press("B"))
	m = send(t, m, press("b"))
	if m.act == nil {
		t.Fatalf("no rebuild modal:\n%s", plain(m))
	}
	if out := plain(m); !strings.Contains(out, "paru -S -- paru") {
		t.Errorf("modal lacks the exact command:\n%s", out)
	}
	if len(ex.argv) != 0 {
		t.Fatalf("ran before confirming: %q", ex.argv)
	}
	m = send(t, m, press("y"))
	if want := [][]string{{"paru", "-S", "--", "paru"}}; !reflect.DeepEqual(ex.argv, want) {
		t.Errorf("ran %q, want %q", ex.argv, want)
	}
	_ = m

	// esc instead of y runs nothing.
	ex.argv = nil
	m = hk(t, opts)
	m = send(t, m, press("B"))
	m = send(t, m, press("b"))
	m = send(t, m, press("esc"))
	if len(ex.argv) != 0 || m.act != nil {
		t.Errorf("esc: ran %q, act %v", ex.argv, m.act)
	}
}

func TestRebuildNotOfferedWhenPendingUnknown(t *testing.T) {
	opts, runner, _ := realOptions()
	runner.Set("checkupdates", system.FakeResult{Code: 1, Stderr: []byte("boom")})
	cfg := config.Default()
	cfg.AURHelper = "paru"
	opts.Config = &cfg
	m := hk(t, opts)
	m = send(t, m, press("B"))
	m = send(t, m, press("b"))
	if m.act != nil {
		t.Error("rebuild offered although the pending updates are unknown")
	}
}

func TestEscCancelsScan(t *testing.T) {
	m := hk(t, fixtureOptions())
	cmd := m.startScan()
	if !m.scanning() || cmd == nil {
		t.Fatal("scan not started")
	}
	run := m.part.run
	next, _ := m.Update(press("esc"))
	m = next.(Model)
	if run.cancelCtxErr() == nil {
		t.Error("esc did not cancel the scan's context")
	}
}

func TestPartialHistoryAndBadge(t *testing.T) {
	opts := fixtureOptions()
	log := string(fixtures.Bytes(fixtures.PacmanLog)) +
		"[2026-10-23T08:00:00+0000] [PACMAN] Running 'pacman -Sy vim'\n" +
		"[2026-10-23T08:00:01+0000] [ALPM] transaction started\n" +
		"[2026-10-23T08:00:02+0000] [ALPM] upgraded vim (9.1.1800-1 -> 9.1.1850-1)\n" +
		"[2026-10-23T08:00:03+0000] [ALPM] transaction completed\n"
	fsys := fixtures.FS()
	_ = fsys.WriteFile("/var/log/pacman.log", []byte(log), 0o644)
	opts.FS = fsys
	m := loaded(t, opts, 140, 100)

	if out := plain(m); !strings.Contains(out, "1 partial upgrade since your last full upgrade (Oct 23: pacman -Sy vim)") {
		t.Errorf("updates header lacks the partial-upgrade line:\n%s", out)
	}
	h := send(t, m, press("4"))
	out := plain(h)
	if !strings.Contains(out, "~ ") || !strings.Contains(out, "pacman -Sy vim") {
		t.Errorf("history doesn't mark the transaction:\n%s", out)
	}
	if !strings.Contains(out, "partial upgrade: this refreshed the sync databases") {
		t.Errorf("history detail lacks the explanation:\n%s", out)
	}
	p := send(t, m, press("3"))
	var vimLine string
	for _, l := range strings.Split(plain(p), "\n") {
		if strings.Contains(l, " vim ") {
			vimLine = l
		}
	}
	if !strings.Contains(vimLine, "~") {
		t.Errorf("vim has no ~ badge: %q", vimLine)
	}
}
