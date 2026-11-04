package ui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/ops"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/system"
)

var errPermission = errors.New("permission denied")

const cache = "/var/cache/pacman/pkg/"

// downgrade selects name on the Packages screen and presses D.
func downgrade(t *testing.T, m Model, name string) Model {
	t.Helper()
	m.pkgs.selName = name
	m.rebuildPackages()
	return send(t, m, press("D"))
}

func upCalls(r *system.FakeRunner) (n int) {
	for _, c := range r.Calls() {
		if strings.HasPrefix(c, "pacman -Up") {
			n++
		}
	}
	return
}

func TestDowngradeModalSnapshots(t *testing.T) {
	opts, runner, _ := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	base := packagesScreen2(t, opts)

	// The picker: older versions only, newest first, with the build date.
	m := downgrade(t, base, "linux")
	if m.screen != screenConfirm || m.act == nil || !m.act.picking {
		t.Fatalf("screen %v act %+v", m.screen, m.act)
	}
	var vs []string
	for _, c := range m.act.candidates {
		vs = append(vs, c.Version)
	}
	if strings.Join(vs, " ") != "6.17.3.arch1-1 6.17.2.arch1-1" {
		t.Errorf("candidates = %q", vs)
	}
	if upCalls(runner) != 0 {
		t.Error("a dry run ran before a version was chosen")
	}
	golden(t, "downgrade-pick", plain(m))
	checkSize(t, m, 100, 30)

	// A protected package with reverse dependencies: its name must be typed.
	m = send(t, m, press("enter"))
	if m.act.loading || m.act.plan.Blocked() {
		t.Fatalf("plan: %+v", m.act.plan)
	}
	w := strings.Join(m.act.plan.Warnings, "\n")
	if !strings.Contains(w, "required by linux-headers, nvidia") || !strings.Contains(w, "IgnorePkg") {
		t.Errorf("warnings:\n%s", w)
	}
	golden(t, "downgrade-protected", plain(m))
	checkSize(t, m, 100, 30)
	if !slicesContains(runner.Calls(), "pacman -Up --print-format %n %v -- "+cache+"linux-6.17.3.arch1-1-x86_64.pkg.tar.zst") {
		t.Fatalf("dry run missing: %q", runner.Calls())
	}

	// A file with no .sig.
	m = downgrade(t, base, "firefox")
	m = send(t, m, press("enter"))
	if len(m.act.plan.Unsigned) != 1 {
		t.Fatalf("unsigned = %v", m.act.plan.Unsigned)
	}
	if !strings.Contains(plain(m), "no .sig file beside firefox-143.0-1-x86_64.pkg.tar.zst") {
		t.Errorf("missing-sig warning absent:\n%s", plain(m))
	}
	golden(t, "downgrade-nosig", plain(m))

	// pacman refuses: its error, and no way forward.
	m = downgrade(t, base, "linux")
	m = send(t, m, press("j"))
	m = send(t, m, press("enter"))
	if !m.act.plan.Blocked() {
		t.Fatalf("plan not blocked: %+v", m.act.plan)
	}
	golden(t, "downgrade-breaks", plain(m))
	m = send(t, m, press("y"))
	m = typeText(t, m, "linux")
	m = send(t, m, press("enter"))
	if len(ex.argv) != 0 {
		t.Fatalf("ran %q after a failed dry run", ex.argv)
	}
}

func TestDowngradeHandoff(t *testing.T) {
	opts, runner, _ := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	base := packagesScreen2(t, opts)

	// Unprotected, no typing: y hands off, with -- before the path.
	m := downgrade(t, base, "firefox")
	m = send(t, m, press("enter"))
	for _, k := range []string{"j", "enter", "x", "r", "n"} {
		m = send(t, m, press(k))
	}
	if len(ex.argv) != 0 || m.screen != screenConfirm {
		t.Fatalf("ran %q, screen %v", ex.argv, m.screen)
	}
	m = send(t, m, press("y"))
	want := []string{"sudo", "pacman", "-U", "--", cache + "firefox-143.0-1-x86_64.pkg.tar.zst"}
	if len(ex.argv) != 1 || !reflect.DeepEqual(ex.argv[0], want) {
		t.Fatalf("ran %q, want %q", ex.argv, want)
	}
	if !strings.Contains(m.noticeText(), "downgraded firefox to 143.0-1") {
		t.Errorf("notice = %q", m.noticeText())
	}
	if m.gen != 2 {
		t.Errorf("gen = %d: the data wasn't reloaded", m.gen)
	}
	// The dry run was a pacman -Up as the user; -U itself ran only in the hand-off.
	for _, c := range runner.Calls() {
		if strings.HasPrefix(c, "pacman -U ") || strings.HasPrefix(c, "sudo") {
			t.Errorf("cronen ran %q itself", c)
		}
	}

	// A protected package needs its name; the right name hands off.
	ex.argv = nil
	m = downgrade(t, packagesScreen2(t, opts), "linux")
	m = send(t, m, press("enter"))
	m = send(t, m, press("enter"))
	m = typeText(t, m, "linu")
	m = send(t, m, press("enter"))
	if len(ex.argv) != 0 {
		t.Fatalf("partial name ran %q", ex.argv)
	}
	m = typeText(t, m, "x")
	m = send(t, m, press("enter"))
	want = []string{"sudo", "pacman", "-U", "--", cache + "linux-6.17.3.arch1-1-x86_64.pkg.tar.zst"}
	if len(ex.argv) != 1 || !reflect.DeepEqual(ex.argv[0], want) {
		t.Fatalf("ran %q, want %q", ex.argv, want)
	}
}

func TestDowngradeEscCancels(t *testing.T) {
	opts, _, _ := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	m := downgrade(t, packagesScreen2(t, opts), "firefox")
	m = send(t, m, press("esc"))
	if m.screen != screenPackages || m.act != nil || len(ex.argv) != 0 {
		t.Errorf("screen %v act %v ran %q", m.screen, m.act, ex.argv)
	}
}

func TestDowngradeNothingToDowngradeTo(t *testing.T) {
	opts, _, _ := realOptions()
	m := downgrade(t, packagesScreen2(t, opts), "ripgrep")
	if m.screen == screenConfirm || !strings.Contains(plain(m), "no version of ripgrep older than 14.1.1-1 is in the package cache") {
		t.Errorf("screen %v:\n%s", m.screen, plain(m))
	}
}

// -U paths come only from the configured cache directories.
func TestDowngradeRefusesOutsideAndLinks(t *testing.T) {
	opts, runner, fsys := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	base := packagesScreen2(t, opts)

	outside := pacman.CachedPackage{Name: "firefox", Version: "143.0-1", Path: "/tmp/firefox-143.0-1-x86_64.pkg.tar.zst"}
	m := downgrade(t, base, "firefox")
	m.act.candidates = []pacman.CachedPackage{outside}
	m = send(t, m, press("enter"))
	if !m.act.plan.Blocked() || !strings.Contains(m.act.plan.Failure, "not inside a configured cache directory") {
		t.Fatalf("plan: %+v", m.act.plan)
	}
	send(t, m, press("y"))
	if upCalls(runner) != 0 || len(ex.argv) != 0 {
		t.Errorf("ran dry %d times, hand-off %q", upCalls(runner), ex.argv)
	}

	// A symbolic link in the cache is refused too.
	fsys.SetSymlink(cache + "firefox-143.0-1-x86_64.pkg.tar.zst")
	m = downgrade(t, base, "firefox")
	m = send(t, m, press("enter"))
	if !m.act.plan.Blocked() || !strings.Contains(m.act.plan.Failure, "not a regular file") {
		t.Fatalf("plan: %+v", m.act.plan)
	}
	if upCalls(runner) != 0 {
		t.Error("dry run ran for a symlink")
	}

	// The file is looked at again before the hand-off: swapped for a link
	// after the dry run, nothing runs.
	opts2, _, fsys2 := realOptions()
	ex2 := &fakeExec{}
	opts2.Exec = ex2.exec
	m = downgrade(t, packagesScreen2(t, opts2), "firefox")
	m = send(t, m, press("enter"))
	if m.act.plan.Blocked() {
		t.Fatalf("plan: %+v", m.act.plan)
	}
	fsys2.SetSymlink(cache + "firefox-143.0-1-x86_64.pkg.tar.zst")
	m = send(t, m, press("y"))
	if len(ex2.argv) != 0 || !m.flashErr {
		t.Errorf("ran %q after the file became a link (flash %q)", ex2.argv, m.flashMsg)
	}
}

func TestDowngradeFromHistory(t *testing.T) {
	opts, _, _ := realOptions()
	m := historyScreen(t, opts)
	// Load the package list first: D needs the installed version.
	// A transaction that changed several packages asks for a filter.
	m = selectTx(t, m, "pacman -S firefox")
	m = send(t, m, press("D"))
	if m.screen == screenConfirm || !strings.Contains(plain(m), "filter by one with /") {
		t.Errorf("screen %v:\n%s", m.screen, plain(m))
	}
	// A transaction that changed one package downgrades it.
	m = selectTx(t, m, "pacman -S nvidia-utils")
	m = send(t, m, press("D"))
	if m.screen != screenConfirm || m.act == nil || m.act.names[0] != "nvidia-utils" || !m.act.picking {
		t.Fatalf("screen %v act %+v", m.screen, m.act)
	}
	m = send(t, m, press("esc"))
	if m.screen != screenHistory {
		t.Errorf("esc returned to %v", m.screen)
	}
	// With a filter, D acts on the package of the selected change.
	m = send(t, m, press("/"))
	m = typeText(t, m, "firefox")
	m = send(t, m, press("enter"))
	m = send(t, m, press("D"))
	if m.screen != screenConfirm || m.act.names[0] != "firefox" {
		t.Fatalf("screen %v act %+v", m.screen, m.act)
	}
}

func TestDowngradeDemoRunsNothing(t *testing.T) {
	m := downgrade(t, packagesScreen2(t, fixtureOptions()), "firefox")
	m = send(t, m, press("enter"))
	if m.act == nil || m.act.plan.Blocked() {
		t.Fatalf("act %+v", m.act)
	}
	m = send(t, m, press("y"))
	if !strings.Contains(plain(m), "demo: nothing is run: sudo pacman -U -- ") {
		t.Errorf("demo y:\n%s", plain(m))
	}
}

var _ = ops.KindDowngrade
