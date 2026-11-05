package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/ops"
	"github.com/leNITHISH/cronen/internal/system"
)

func installOptions(archive bool) (Options, *system.FakeRunner, *system.FakeFS, *fakeExec) {
	opts, r, fsys := realOptions()
	cfg := config.Default()
	cfg.ArchiveInstall = archive
	opts.Config = &cfg
	ex := &fakeExec{}
	opts.Exec = ex.exec
	opts.ArchiveBase = fixtures.ArchiveBase
	n := 0
	opts.Nonce = func() string { n++; return fmt.Sprintf("test%04d", n) }
	return opts, r, fsys, ex
}

// openInstallWith opens the helper with I, types names and presses enter.
func openInstallWith(t *testing.T, opts Options, names string) Model {
	t.Helper()
	m := loaded(t, opts, 110, 60)
	m = send(t, m, press("I"))
	if m.screen != screenInstall || m.inst == nil || m.inst.stage != instInput {
		t.Fatalf("I did not open the prompt: screen %v", m.screen)
	}
	m = typeText(t, m, names)
	return send(t, m, press("enter"))
}

// neverPartial fails the test if any command run (through the runner or the
// hand-off) is a partial upgrade.
func neverPartial(t *testing.T, r *system.FakeRunner, ex *fakeExec) {
	t.Helper()
	g := ops.Guard{RealDB: fixtures.RealDBPath, ArchiveBase: fixtures.ArchiveBase}
	for _, c := range r.Calls() {
		if strings.Contains(c, " -Sy") && !strings.HasPrefix(c, "fakeroot -- pacman -Sy --dbpath "+fixtures.ArchiveBase+"/") {
			t.Errorf("ran %q", c)
		}
		if strings.HasPrefix(c, "pacman -S ") || strings.HasPrefix(c, "sudo") {
			t.Errorf("ran %q", c)
		}
	}
	for _, argv := range ex.argv {
		if err := g.Verify(argv); err != nil {
			t.Errorf("handed off %q: %v", argv, err)
		}
	}
}

func TestInstallOptionAIsTheDefault(t *testing.T) {
	opts, r, _, ex := installOptions(false)
	m := openInstallWith(t, opts, "htop")
	if m.inst.stage != instReady || m.inst.sel != ops.OptionUpgrade || m.inst.a.Blocked() {
		t.Fatalf("flow %+v", m.inst)
	}
	golden(t, "install-a-only", plain(m))
	checkSize(t, m, 110, 60)
	if len(ex.argv) != 0 {
		t.Fatalf("ran %q before y", ex.argv)
	}
	// Option B is off: b says so and runs nothing.
	m = send(t, m, press("b"))
	if m.flashMsg == "" || !strings.Contains(m.flashMsg, "archive_install") || m.inst.sel != ops.OptionUpgrade || len(ex.argv) != 0 {
		t.Fatalf("b: flash %q sel %v", m.flashMsg, m.inst.sel)
	}
	// y runs the full upgrade plus htop after the dry run, as the user.
	m = send(t, m, press("y"))
	want := []string{"sudo", "pacman", "-Syu", "--", "htop"}
	if len(ex.argv) != 1 || strings.Join(ex.argv[0], " ") != strings.Join(want, " ") {
		t.Fatalf("ran %q", ex.argv)
	}
	if !slicesContains(r.Calls(), "pacman -Sup --dbpath "+fixtures.SyncDBPath+" --logfile /dev/null --noconfirm --print-format %n %v %r %s -- htop") {
		t.Errorf("calls %q lack the dry run", r.Calls())
	}
	if m.screen != screenUpdates || m.inst != nil {
		t.Errorf("screen %v", m.screen)
	}
	if !m.upgrade.done || m.upgrade.err != nil {
		t.Errorf("an option A hand-off is recorded as an upgrade: %+v", m.upgrade)
	}
	neverPartial(t, r, ex)
}

func TestInstallOptionBBothOptionsSideBySide(t *testing.T) {
	opts, r, fsys, ex := installOptions(true)
	m := openInstallWith(t, opts, "htop")
	// B is available but not prepared: nothing was downloaded yet.
	if m.inst.bAvail != nil || m.inst.bStage != archIdle {
		t.Fatalf("bAvail %v stage %v", m.inst.bAvail, m.inst.bStage)
	}
	for _, c := range r.Calls() {
		if strings.HasPrefix(c, "fakeroot -- pacman -Sy") {
			t.Fatalf("synced the archive before b: %q", c)
		}
	}
	golden(t, "install-b-idle", plain(m))

	m = send(t, m, press("b"))
	if m.inst.bStage != archReady || m.inst.sel != ops.OptionArchive || m.inst.bPlan.Blocked() {
		t.Fatalf("flow %+v", m.inst)
	}
	if m.inst.b.Day != fixtures.ArchiveDayB || len(m.inst.b.Candidate.Differ) != 1 {
		t.Fatalf("day %s differ %d", m.inst.b.Day, len(m.inst.b.Candidate.Differ))
	}
	golden(t, "install-both", plain(m))
	checkSize(t, m, 110, 60)

	// Back to A and to B again: no second preparation.
	m = send(t, m, press("a"))
	m = send(t, m, press("b"))
	n := 0
	for _, c := range r.Calls() {
		if strings.HasPrefix(c, "fakeroot -- pacman -Sy") {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d syncs, want 2 (one per day, once)", n)
	}

	m = send(t, m, press("y"))
	dir := m.opts.ArchiveBase + "/20261023T120000Z-test0001/" + fixtures.ArchiveDayB
	want := "sudo pacman -S --dbpath " + dir + " --config " + dir + "/pacman.conf -- htop"
	if len(ex.argv) != 1 || strings.Join(ex.argv[0], " ") != want {
		t.Fatalf("ran %q, want %q", ex.argv, want)
	}
	// Demo is off: the install is recorded, and the notice names the day.
	got := m.opts.State.ArchiveInstalls()
	if len(got) != 1 || got[0].Day != fixtures.ArchiveDayB || got[0].Packages[0] != "htop" || got[0].Differ != 1 {
		t.Fatalf("state %+v", got)
	}
	if !strings.Contains(m.actionNotice, "installed from the archive (2026-10-21)") {
		t.Errorf("notice %q", m.actionNotice)
	}
	if mode, ok := fsys.DirMode(dir); !ok || mode != 0o700 {
		t.Errorf("dir mode %v", mode)
	}
	neverPartial(t, r, ex)
}

func TestInstallOptionBRefusesWithTheLock(t *testing.T) {
	opts, r, fsys, ex := installOptions(true)
	m := openInstallWith(t, opts, "htop")
	m = send(t, m, press("b"))
	_ = fsys.WriteFile("/var/lib/pacman/db.lck", nil, 0o644)
	m = send(t, m, press("y"))
	if len(ex.argv) != 0 || m.screen != screenInstall || !strings.Contains(m.flashMsg, "db.lck") {
		t.Fatalf("ran %q, screen %v, flash %q", ex.argv, m.screen, m.flashMsg)
	}
	// Option A is unaffected by that check and still available.
	m = send(t, m, press("a"))
	m = send(t, m, press("y"))
	if len(ex.argv) != 1 || ex.argv[0][2] != "-Syu" {
		t.Fatalf("ran %q", ex.argv)
	}
	neverPartial(t, r, ex)
}

func TestInstallOptionBRefusesAChangedConfig(t *testing.T) {
	opts, _, fsys, ex := installOptions(true)
	m := openInstallWith(t, opts, "htop")
	m = send(t, m, press("b"))
	_ = fsys.WriteFile(m.inst.b.Dir+"/pacman.conf", []byte("[options]\nXferCommand = /bin/evil\n"), 0o600)
	m = send(t, m, press("y"))
	if len(ex.argv) != 0 || !strings.Contains(m.flashMsg, "changed") {
		t.Fatalf("ran %q, flash %q", ex.argv, m.flashMsg)
	}
}

func TestInstallOptionBUnavailable(t *testing.T) {
	cases := map[string]struct {
		mod  func(*Options, *system.FakeRunner, *system.FakeFS)
		want string
	}{
		"no fakeroot": {func(_ *Options, r *system.FakeRunner, _ *system.FakeFS) {
			r.Set("fakeroot --version", system.FakeResult{Err: system.ErrCommandNotFound})
		}, "fakeroot is not installed"},
		"no last upgrade": {func(_ *Options, _ *system.FakeRunner, f *system.FakeFS) {
			_ = f.WriteFile("/var/log/pacman.log", []byte("[2026-10-01T10:00:00+0000] [PACMAN] Running 'pacman -Qi'\n"), 0o644)
		}, "last full upgrade is unknown"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			opts, r, fsys, ex := installOptions(true)
			c.mod(&opts, r, fsys)
			m := openInstallWith(t, opts, "htop")
			if m.inst.bAvail == nil || !strings.Contains(m.inst.bAvail.Error(), c.want) {
				t.Fatalf("bAvail %v", m.inst.bAvail)
			}
			golden(t, "install-b-unavailable-"+strings.ReplaceAll(name, " ", "-"), plain(m))
			m = send(t, m, press("b"))
			if m.inst.sel != ops.OptionUpgrade || !strings.Contains(m.flashMsg, "unavailable") {
				t.Errorf("b: sel %v flash %q", m.inst.sel, m.flashMsg)
			}
			for _, call := range r.Calls() {
				if strings.HasPrefix(call, "fakeroot -- ") {
					t.Errorf("ran %q", call)
				}
			}
			if len(m.opts.FS.(*system.FakeFS).Dirs()) != 0 || len(ex.argv) != 0 {
				t.Errorf("dirs %v, ran %q", m.opts.FS.(*system.FakeFS).Dirs(), ex.argv)
			}
		})
	}
}

func TestInstallOptionBOffWithoutNonceOrBase(t *testing.T) {
	for _, mod := range []func(*Options){func(o *Options) { o.Nonce = nil }, func(o *Options) { o.ArchiveBase = "" }} {
		opts, _, _, _ := installOptions(true)
		mod(&opts)
		m := openInstallWith(t, opts, "htop")
		if m.inst.bAvail == nil {
			t.Fatal("option B should be unavailable")
		}
	}
}

func TestInstallFailuresRunNothing(t *testing.T) {
	for name, names := range map[string]string{"unknown package": "nosuchpkg", "bad name": "-Syu", "uppercase": "Htop", "semicolon": "htop;id"} {
		t.Run(name, func(t *testing.T) {
			opts, r, _, ex := installOptions(true)
			m := openInstallWith(t, opts, names)
			if m.inst.stage != instFailed || m.inst.err == nil {
				t.Fatalf("stage %v", m.inst.stage)
			}
			golden(t, "install-failed-"+strings.ReplaceAll(name, " ", "-"), plain(m))
			for _, k := range []string{"y", "a", "b", "enter"} {
				m = send(t, m, press(k))
			}
			if len(ex.argv) != 0 {
				t.Fatalf("ran %q", ex.argv)
			}
			neverPartial(t, r, ex)
		})
	}
}

func TestInstallDryRunFailureBlocksOption(t *testing.T) {
	opts, r, _, ex := installOptions(false)
	prev := r.Handler
	r.Handler = func(l string) (system.FakeResult, bool) {
		if strings.HasPrefix(l, "pacman -Sup ") && strings.HasSuffix(l, " -- htop") {
			return system.FakeResult{Stderr: []byte("error: failed to prepare transaction (could not satisfy dependencies)\n:: installing a (2-1) breaks dependency 'b' required by c\n"), Code: 1}, true
		}
		return prev(l)
	}
	m := openInstallWith(t, opts, "htop")
	if !m.inst.a.Blocked() {
		t.Fatalf("plan %+v", m.inst.a)
	}
	golden(t, "install-a-blocked", plain(m))
	m = send(t, m, press("y"))
	if len(ex.argv) != 0 {
		t.Fatalf("ran %q after a failed dry run", ex.argv)
	}
}

func TestInstallEscCancelsAndRunsNothing(t *testing.T) {
	opts, r, _, ex := installOptions(true)
	m := openInstallWith(t, opts, "htop")
	m = send(t, m, press("b"))
	m = send(t, m, press("esc"))
	if m.screen != screenUpdates || m.inst != nil || len(ex.argv) != 0 {
		t.Fatalf("screen %v ran %q", m.screen, ex.argv)
	}
	// The prompt too.
	m = send(t, m, press("I"))
	m = send(t, m, press("esc"))
	if m.screen != screenUpdates || len(ex.argv) != 0 {
		t.Fatalf("screen %v", m.screen)
	}
	neverPartial(t, r, ex)
}

func TestInstallWithAURHelper(t *testing.T) {
	opts, r, _, ex := installOptions(true)
	cfg := *opts.Config
	cfg.AURHelper = "paru"
	opts.Config = &cfg
	m := openInstallWith(t, opts, "htop paru")
	if m.inst.stage != instReady || strings.Join(m.inst.res.AUR, ",") != "paru" {
		t.Fatalf("flow %+v", m.inst)
	}
	// An AUR package rules out option B.
	if m.inst.bAvail == nil || !strings.Contains(m.inst.bAvail.Error(), "repository packages only") {
		t.Errorf("bAvail %v", m.inst.bAvail)
	}
	golden(t, "install-aur-helper", plain(m))
	m = send(t, m, press("y"))
	if len(ex.argv) != 1 || strings.Join(ex.argv[0], " ") != "paru -Syu -- htop paru" {
		t.Fatalf("ran %q", ex.argv)
	}
	neverPartial(t, r, ex)
}

func TestInstallFromCommandLine(t *testing.T) {
	opts, r, _, ex := installOptions(false)
	opts.Install = []string{"htop"}
	m := sized(t, opts, 110, 60)
	if m.screen != screenInstall || m.inst.stage != instResolving {
		t.Fatalf("screen %v", m.screen)
	}
	m = run(t, m, m.resolveInstallCmd(m.inst.seq, m.inst.names))
	if m.inst.stage != instReady || m.inst.a.Blocked() {
		t.Fatalf("flow %+v", m.inst)
	}
	m = send(t, m, press("y"))
	if len(ex.argv) != 1 || ex.argv[0][2] != "-Syu" {
		t.Fatalf("ran %q", ex.argv)
	}
	neverPartial(t, r, ex)

	// Invalid names never get as far as a lookup.
	opts.Install = []string{"-Sy", "htop"}
	m = sized(t, opts, 110, 60)
	if m.inst.stage != instFailed {
		t.Fatalf("stage %v", m.inst.stage)
	}
}

func TestInstallDemoRunsNothing(t *testing.T) {
	opts, _, _, ex := installOptions(true)
	opts.Demo = true
	m := openInstallWith(t, opts, "htop")
	m = send(t, m, press("b"))
	m = send(t, m, press("y"))
	if len(ex.argv) != 0 || !strings.Contains(m.flashMsg, "demo: nothing is run") {
		t.Fatalf("ran %q, flash %q", ex.argv, m.flashMsg)
	}
}

func TestInstallPromptKeysAreText(t *testing.T) {
	opts, _, _, _ := installOptions(false)
	m := loaded(t, opts, 100, 30)
	m = send(t, m, press("I"))
	m = typeText(t, m, "qr U")
	if m.screen != screenInstall || m.inst.input.Value() != "qr U" {
		t.Fatalf("screen %v value %q", m.screen, m.inst.input.Value())
	}
	golden(t, "install-prompt", plain(m))
	// Empty input does nothing.
	m = send(t, m, press("esc"))
	m = send(t, m, press("I"))
	m = send(t, m, press("enter"))
	if m.inst.stage != instInput {
		t.Fatalf("stage %v", m.inst.stage)
	}
}

func TestInstallScrolls(t *testing.T) {
	opts, _, _, _ := installOptions(true)
	m := loaded(t, opts, 100, 24)
	m = send(t, m, press("I"))
	m = typeText(t, m, "htop")
	m = send(t, m, press("enter"))
	m = send(t, m, press("b"))
	top := plain(m)
	m = send(t, m, press("G"))
	if plain(m) == top {
		t.Error("G did not scroll")
	}
	checkSize(t, m, 100, 24)
	m = send(t, m, press("g"))
	if plain(m) != top {
		t.Error("g did not return to the top")
	}
}
