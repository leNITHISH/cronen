package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/system"
)

// markAndRemove marks the named packages on the Packages screen and
// presses X.
func markAndRemove(t *testing.T, m Model, names ...string) Model {
	t.Helper()
	for _, n := range names {
		m.pkgs.selName = n
		m.rebuildPackages()
		m = send(t, m, press("space"))
	}
	return send(t, m, press("X"))
}

func removeCalls(r *system.FakeRunner) (dry int) {
	for _, c := range r.Calls() {
		if strings.HasPrefix(c, "pacman -R") {
			dry++
		}
	}
	return
}

func TestRemoveModalSnapshots(t *testing.T) {
	opts, _, _ := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	base := packagesScreen2(t, opts)

	// A plain removal: -s pulls in a dependency.
	m := markAndRemove(t, base, "spotify")
	if m.screen != screenConfirm || m.act == nil || m.act.loading || m.act.plan.Blocked() {
		t.Fatalf("screen %v act %+v", m.screen, m.act)
	}
	golden(t, "remove-plain", plain(m))
	checkSize(t, m, 100, 30)

	// A protected package: the name must be typed.
	m = markAndRemove(t, base, "sudo")
	golden(t, "remove-protected", plain(m))

	// Removing something others need: pacman's error, no way forward.
	m = markAndRemove(t, base, "libx11")
	if !m.act.plan.Blocked() {
		t.Fatalf("plan not blocked: %+v", m.act.plan)
	}
	golden(t, "remove-breaks", plain(m))
	send(t, m, press("y"))
	if len(ex.argv) != 0 {
		t.Fatalf("ran %q after a failed dry run", ex.argv)
	}
}

func packagesScreen2(t *testing.T, opts Options) Model {
	t.Helper()
	m := loaded(t, opts, 100, 30)
	return send(t, m, press("3"))
}

func TestRemoveHandoff(t *testing.T) {
	opts, runner, _ := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	m := markAndRemove(t, packagesScreen2(t, opts), "spotify")

	// The dry run came first, as the user, with -- before the names.
	want := "pacman -Rsp --print-format %n %v -- spotify"
	if !slicesContains(runner.Calls(), want) {
		t.Fatalf("calls %q lack %q", runner.Calls(), want)
	}
	// Other keys run nothing.
	for _, k := range []string{"j", "enter", "x", "r"} {
		m = send(t, m, press(k))
	}
	if len(ex.argv) != 0 || m.screen != screenConfirm {
		t.Fatalf("ran %q, screen %v", ex.argv, m.screen)
	}
	m = send(t, m, press("y"))
	if len(ex.argv) != 1 || !reflect.DeepEqual(ex.argv[0], []string{"sudo", "pacman", "-Rns", "--", "spotify"}) {
		t.Fatalf("ran %q", ex.argv)
	}
	if !strings.Contains(m.noticeText(), "removed 1 package") {
		t.Errorf("notice = %q", m.noticeText())
	}
}

func slicesContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestRemoveToggleRerunsDryRun(t *testing.T) {
	opts, runner, _ := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	m := markAndRemove(t, packagesScreen2(t, opts), "spotify")
	// n: keep .pacsave backups. s: no recursive.
	m = send(t, m, press("n"))
	m = send(t, m, press("s"))
	if !slicesContains(runner.Calls(), "pacman -Rp --print-format %n %v -- spotify") {
		t.Fatalf("calls %q", runner.Calls())
	}
	if !slicesContains(runner.Calls(), "pacman -Qii -- spotify") {
		t.Fatalf("backup files not read lazily: %q", runner.Calls())
	}
	send(t, m, press("y"))
	if len(ex.argv) != 1 || !reflect.DeepEqual(ex.argv[0], []string{"sudo", "pacman", "-R", "--", "spotify"}) {
		t.Fatalf("ran %q", ex.argv)
	}
}

func TestProtectedNeedsTheName(t *testing.T) {
	opts, _, _ := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	m := markAndRemove(t, packagesScreen2(t, opts), "sudo")
	m = send(t, m, press("enter"))
	if len(ex.argv) != 0 {
		t.Fatalf("enter removed a protected package: %q", ex.argv)
	}
	// "sud" is not enough; the letters s and n are typed, not toggled.
	m = typeText(t, m, "sud")
	m = send(t, m, press("enter"))
	if len(ex.argv) != 0 {
		t.Fatalf("partial name ran %q", ex.argv)
	}
	m = typeText(t, m, "o")
	if m.act.opts.Recursive != true || m.act.opts.NoSave != true {
		t.Fatalf("typing toggled options: %+v", m.act.opts)
	}
	m = send(t, m, press("enter"))
	if len(ex.argv) != 1 || !reflect.DeepEqual(ex.argv[0], []string{"sudo", "pacman", "-Rns", "--", "sudo"}) {
		t.Fatalf("ran %q", ex.argv)
	}
}

func TestPrivilegeCommandInHandoff(t *testing.T) {
	opts, _, _ := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	cfg := config.Default()
	cfg.PrivilegeCommand = "doas"
	opts.Config = &cfg
	m := markAndRemove(t, packagesScreen2(t, opts), "spotify")
	send(t, m, press("y"))
	if len(ex.argv) != 1 || ex.argv[0][0] != "doas" {
		t.Fatalf("ran %q", ex.argv)
	}
}

func TestEscCancelsAndNothingMarkedFlashes(t *testing.T) {
	opts, runner, _ := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	m := packagesScreen2(t, opts)
	m = send(t, m, press("X"))
	if m.screen != screenPackages || removeCalls(runner) != 0 || !strings.Contains(m.flashMsg, "mark packages") {
		t.Fatalf("screen %v, flash %q", m.screen, m.flashMsg)
	}
	m = markAndRemove(t, m, "spotify")
	m = send(t, m, press("esc"))
	if m.screen != screenPackages || m.act != nil || len(ex.argv) != 0 {
		t.Fatalf("screen %v act %v ran %q", m.screen, m.act, ex.argv)
	}
}

func TestDemoRunsNothing(t *testing.T) {
	opts := fixtureOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	m := markAndRemove(t, packagesScreen2(t, opts), "spotify")
	m = send(t, m, press("y"))
	if len(ex.argv) != 0 || !strings.Contains(m.flashMsg, "demo: nothing is run") {
		t.Fatalf("ran %q flash %q", ex.argv, m.flashMsg)
	}
}

func housekeepingScreen(t *testing.T, opts Options) Model {
	t.Helper()
	m := loaded(t, opts, 100, 40)
	return send(t, m, press("5"))
}

func TestHousekeepingScreen(t *testing.T) {
	opts := fixtureOptions()
	opts.Disk = fixtures.Disk()
	m := housekeepingScreen(t, opts)
	golden(t, "housekeeping", plain(m))
	checkSize(t, m, 100, 40)
	out := plain(m)
	for _, want := range []string{"Orphans (4)", "libgbm-compat", "old-codec-lib", "mirrorlist.pacnew",
		"+## Generated on 2026-10-01", "6 packages", "bluetooth.service", "paccache -r"} {
		if !strings.Contains(out, want) {
			t.Errorf("screen lacks %q", want)
		}
	}
}

func TestOrphanCleanup(t *testing.T) {
	opts, runner, _ := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	m := housekeepingScreen(t, opts)

	// All four start marked. Unmark the second, check, mark it again.
	m = send(t, m, press("j"))
	m = send(t, m, press("space"))
	if got := m.markedOrphans(); !reflect.DeepEqual(got, []string{"libgbm-compat", "paru-debug", "zstd-debug"}) {
		t.Fatalf("marked = %v", got)
	}
	m = send(t, m, press("space"))
	m = send(t, m, press("X"))
	if m.act == nil || m.act.plan.Blocked() {
		t.Fatalf("act %+v", m.act)
	}
	if !strings.Contains(strings.Join(m.act.plan.Warnings, "|"), "new orphans") {
		t.Errorf("no new-orphans note: %v", m.act.plan.Warnings)
	}
	golden(t, "remove-orphans", plain(m))
	if !slicesContains(runner.Calls(), "pacman -Rsp --print-format %n %v -- libgbm-compat old-codec-lib paru-debug zstd-debug") {
		t.Fatalf("calls %q", runner.Calls())
	}
	m = send(t, m, press("y"))
	if len(ex.argv) != 1 || !reflect.DeepEqual(ex.argv[0], []string{"sudo", "pacman", "-Rns", "--", "libgbm-compat", "old-codec-lib", "paru-debug", "zstd-debug"}) {
		t.Fatalf("ran %q", ex.argv)
	}
	if m.screen != screenHousekeeping {
		t.Errorf("screen = %v", m.screen)
	}
}

func TestNoOrphansMarkedRemovesNothing(t *testing.T) {
	opts, runner, _ := realOptions()
	m := housekeepingScreen(t, opts)
	for range 4 {
		m = send(t, m, press("space"))
		m = send(t, m, press("j"))
	}
	m = send(t, m, press("X"))
	if m.act != nil || removeCalls(runner) != 0 {
		t.Fatalf("act %v, calls %q", m.act, runner.Calls())
	}
}

func TestPacdiffHandoff(t *testing.T) {
	opts, runner, _ := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	m := housekeepingScreen(t, opts)
	m = send(t, m, press("P"))
	if m.act == nil {
		t.Fatal("no modal")
	}
	golden(t, "pacdiff", plain(m))
	for _, c := range runner.Calls() {
		if strings.Contains(c, "pacdiff") && c != "pacdiff -o" {
			t.Fatalf("pacdiff ran in a dry run: %q", c)
		}
	}
	m = send(t, m, press("y"))
	if len(ex.argv) != 1 || !reflect.DeepEqual(ex.argv[0], []string{"sudo", "pacdiff"}) {
		t.Fatalf("ran %q", ex.argv)
	}
}

func TestHousekeepingSourcesFailAlone(t *testing.T) {
	opts, runner, _ := realOptions()
	runner.Set("systemctl --failed --plain --no-legend", system.FakeResult{Code: 1, Stderr: []byte("boom")})
	m := housekeepingScreen(t, opts)
	out := plain(m)
	if !strings.Contains(out, "systemctl --failed") || !strings.Contains(out, "libgbm-compat") {
		t.Errorf("a failed source broke the screen:\n%s", out)
	}
}

func TestHousekeepingOff(t *testing.T) {
	opts, runner, _ := realOptions()
	cfg := config.Default()
	cfg.Housekeeping = false
	opts.Config = &cfg
	m := housekeepingScreen(t, opts)
	for _, c := range runner.Calls() {
		if strings.HasPrefix(c, "systemctl") || strings.HasPrefix(c, "diff") {
			t.Errorf("housekeeping off, but ran %q", c)
		}
	}
	if !strings.Contains(plain(m), "housekeeping is off") {
		t.Error("no note")
	}
}
