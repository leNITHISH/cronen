package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/risk"
)

const vscode = "visual-studio-code-bin"

func TestDetailShowsMaintainerAndCommits(t *testing.T) {
	m := loaded(t, fixtureOptions(), 110, 40)
	m = selectPackage(t, m, vscode)
	out := plain(m)
	for _, want := range []string{
		"AUR review:",
		"maintainer: dcelasun · co-maintainers: dallas",
		"2025-10-21  dcelasun  Update to 1.105.1",
		"d: review the PKGBUILD diff",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("detail lacks %q:\n%s", want, out)
		}
	}
}

func TestDiffScreenSuspicious(t *testing.T) {
	m := loaded(t, fixtureOptions(), 110, 30)
	m = selectPackage(t, m, vscode)
	m = send(t, m, press("d"))
	if m.screen != screenDiff {
		t.Fatalf("screen = %v after d", m.screen)
	}
	out := plain(m)
	for _, want := range []string{
		"visual-studio-code-bin  PKGBUILD diff",
		"Suspicious changes:",
		"changes the host of a source URL (update.code.visualstudio.com → vscode.download.prss.microsoft.com)",
		"⚠ +source=",
		"-source=",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("diff screen lacks %q:\n%s", want, out)
		}
	}
	golden(t, "diff", out)

	// The loaded diff raised rule 13 on the package.
	var flagged bool
	for _, p := range m.result.Packages {
		for _, f := range p.Flags {
			flagged = flagged || (p.Name == vscode && f.Rule == risk.RuleSuspicious)
		}
	}
	if !flagged {
		t.Error("no suspicious flag after opening the diff")
	}

	m = send(t, m, press("esc"))
	if m.screen != screenUpdates {
		t.Errorf("screen = %v after esc", m.screen)
	}
	if !strings.Contains(plain(m), "(1 suspicious line)") {
		t.Errorf("detail doesn't count the suspicious lines:\n%s", plain(m))
	}
}

func TestDiffScreenClean(t *testing.T) {
	m := loaded(t, fixtureOptions(), 110, 30)
	m = selectPackage(t, m, "paru")
	m = send(t, m, press("d"))
	out := plain(m)
	if !strings.Contains(out, "+pkgrel=2") || strings.Contains(out, "Suspicious") || strings.Contains(out, "⚠ +") {
		t.Errorf("clean diff screen:\n%s", out)
	}
	m = send(t, m, press("d")) // d closes it again
	if m.screen != screenUpdates || !strings.Contains(plain(m), "nothing suspicious found") {
		t.Errorf("after closing:\n%s", plain(m))
	}
	for _, p := range m.result.Packages {
		if p.Name == "paru" && len(p.Flags) != 0 {
			t.Errorf("clean diff raised flags: %+v", p.Flags)
		}
	}
}

func TestDiffKeyOnRepoPackage(t *testing.T) {
	m := loaded(t, fixtureOptions(), 110, 30)
	m = selectPackage(t, m, "linux")
	m = send(t, m, press("d"))
	if m.screen != screenUpdates || !strings.Contains(plain(m), "d reviews the PKGBUILD diff") {
		t.Errorf("screen %v:\n%s", m.screen, plain(m))
	}
}

func TestDiffScrollAndQuit(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 12)
	m = selectPackage(t, m, vscode)
	m = send(t, m, press("d"))
	before := m.diffView.YOffset()
	m = send(t, m, press("j"))
	m = send(t, m, press("j"))
	if m.diffView.YOffset() != before+2 {
		t.Errorf("offset %d after two j, from %d", m.diffView.YOffset(), before)
	}
	m = send(t, m, press("g"))
	if m.diffView.YOffset() != 0 {
		t.Errorf("offset %d after g", m.diffView.YOffset())
	}
	checkSize(t, m, 100, 12)
}

func TestLazyAndEagerFetching(t *testing.T) {
	lazy := fixtureOptions()
	fl := fixtures.Fetcher()
	lazy.Fetcher = fl
	m := loaded(t, lazy, 110, 30)
	diffs := func() int {
		n := 0
		for _, c := range fl.Calls() {
			if strings.Contains(c.URL, "/rawdiff/") {
				n++
			}
		}
		return n
	}
	if diffs() != 0 {
		t.Fatalf("lazy mode fetched %d diffs before d", diffs())
	}
	logs := 0
	for _, c := range fl.Calls() {
		if strings.Contains(c.URL, "/atom/") {
			logs++
		}
	}
	if logs != 2 {
		t.Errorf("fetched %d logs, want one per pending AUR update (2)", logs)
	}
	m = selectPackage(t, m, "paru")
	m = send(t, m, press("d"))
	if diffs() != 1 {
		t.Errorf("fetched %d diffs after d", diffs())
	}
	_ = m

	eager := fixtureOptions()
	fe := fixtures.Fetcher()
	eager.Fetcher = fe
	cfg := config.Default()
	cfg.AURReview = config.AURReviewEager
	eager.Config = &cfg
	m = loaded(t, eager, 110, 30)
	var flagged bool
	for _, p := range m.result.Packages {
		for _, f := range p.Flags {
			flagged = flagged || (p.Name == vscode && f.Rule == risk.RuleSuspicious)
		}
	}
	if !flagged {
		t.Error("eager mode didn't flag the suspicious diff before it was opened")
	}
}

func TestMaintainerChangeFlagLifecycle(t *testing.T) {
	opts, _, fsys := realOptions()
	var st config.State
	st.SetMaintainer(config.Maintainer{PkgBase: vscode, Maintainer: "old-maintainer", CoMaintainers: []string{"dallas"}})
	opts.State = st
	ex := &fakeExec{}
	opts.Exec = ex.exec

	hasFlag := func(m Model) bool {
		for _, p := range m.result.Packages {
			if p.Name == vscode {
				return slices.ContainsFunc(p.Flags, func(f risk.Flag) bool {
					return f.Rule == risk.RuleMaintainer && strings.Contains(f.Reason, "old-maintainer → dcelasun")
				})
			}
		}
		return false
	}

	m := loaded(t, opts, 110, 30)
	if !hasFlag(m) {
		t.Fatal("maintainer change not flagged")
	}
	m = selectPackage(t, m, vscode)
	if !strings.Contains(plain(m), "old-maintainer → dcelasun") {
		t.Errorf("detail lacks the reason:\n%s", plain(m))
	}

	// A restart (a new model with the saved state) still flags it: the
	// baseline pass must not have overwritten the recorded maintainer.
	saved := savedState(t, fsys)
	if rec, _ := saved.Maintainer(vscode); rec.Maintainer != "old-maintainer" {
		t.Fatalf("saved record = %+v", rec)
	}
	if rec, ok := saved.Maintainer("paru"); !ok || rec.Maintainer != "Morganamilo" {
		t.Errorf("baseline for paru not saved: %+v, %v", rec, ok)
	}
	opts2 := opts
	opts2.State = saved
	if m2 := loaded(t, opts2, 110, 30); !hasFlag(m2) {
		t.Fatal("flag lost across a restart")
	}

	// A successful upgrade accepts the current maintainers.
	m = send(t, m, press("U"))
	m = send(t, m, press("y"))
	if rec, _ := savedState(t, fsys).Maintainer(vscode); rec.Maintainer != "dcelasun" {
		t.Errorf("after the upgrade the record is %+v", rec)
	}
	if hasFlag(m) {
		t.Error("flag survived a successful upgrade")
	}
}

func TestFailedUpgradeKeepsMaintainerFlag(t *testing.T) {
	opts, _, fsys := realOptions()
	var st config.State
	st.SetMaintainer(config.Maintainer{PkgBase: vscode, Maintainer: "old-maintainer", CoMaintainers: []string{"dallas"}})
	opts.State = st
	opts.Exec = (&fakeExec{err: errors.New("exit status 1")}).exec
	m := loaded(t, opts, 110, 30)
	m = send(t, m, press("U"))
	send(t, m, press("y"))
	if rec, _ := savedState(t, fsys).Maintainer(vscode); rec.Maintainer != "old-maintainer" {
		t.Errorf("a failed upgrade changed the record: %+v", rec)
	}
}

func TestCleanLineStripsTerminalControls(t *testing.T) {
	got := cleanLine("+a\x1b[31mred\x1b[0m\x1b]0;title\x07\tb\x00\u009bc")
	if strings.ContainsAny(got, "\x1b\x07\x00\u009b\t") || !strings.HasPrefix(got, "+ared") {
		t.Errorf("cleanLine = %q", got)
	}
}
