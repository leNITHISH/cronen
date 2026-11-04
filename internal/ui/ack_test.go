package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/risk"
)

// firstNonCritical returns a flagged package none of whose flags is critical.
func firstNonCritical(t *testing.T, m Model) risk.Package {
	t.Helper()
	for _, p := range m.result.Flagged() {
		if p.Severity() < risk.Critical {
			return p
		}
	}
	t.Fatal("no non-critical flagged package in the fixtures")
	return risk.Package{}
}

func TestAcknowledgeMovesPackageOut(t *testing.T) {
	opts, _, fsys := realOptions()
	m := loaded(t, opts, 200, 50)
	p := firstNonCritical(t, m)
	flagged := len(m.result.Flagged())
	m = selectPackage(t, m, p.Name)

	m = send(t, m, press("a"))

	got := m.result.Packages[slices.IndexFunc(m.result.Packages, func(q risk.Package) bool { return q.Name == p.Name })]
	if len(got.Flags) != 0 || len(got.Acknowledged) != len(p.Flags) {
		t.Fatalf("flags %v, acknowledged %v", got.Flags, got.Acknowledged)
	}
	if n := len(m.result.Flagged()); n != flagged-1 {
		t.Errorf("flagged = %d, want %d", n, flagged-1)
	}
	// The package is out of NEEDS ATTENTION and in its normal group, ✓.
	e := entryNames(m)[p.Name]
	if len(e.pkg.Flags) != 0 || e.pkg.Name != p.Name {
		t.Fatalf("entry %+v", e)
	}
	line := m.entryLine(e, false, 60)
	if !strings.Contains(line, "✓") {
		t.Errorf("no ✓ in %q", line)
	}
	m = selectPackage(t, m, p.Name) // key presses clear the flash
	if d := plain(m); !strings.Contains(d, "1 acknowledged") {
		t.Errorf("status bar lacks the count:\n%s", d)
	}
	if d := plain(m); !strings.Contains(d, "Acknowledged for "+p.NewVersion) || !strings.Contains(d, p.Flags[0].Reason[:20]) {
		t.Errorf("detail lacks the dimmed flags:\n%s", d)
	}

	// Saved, for this exact version and the acknowledged rules only.
	st := savedState(t, fsys)
	if rules := st.Acknowledged(p.Name, p.NewVersion); !slices.Equal(rules, risk.AcknowledgeableRules(p)) {
		t.Errorf("saved rules = %v", rules)
	}
	if st.Acknowledged(p.Name, p.NewVersion+"1") != nil {
		t.Error("another version is acknowledged")
	}
}

func TestAcknowledgeLeavesCriticalFlags(t *testing.T) {
	opts, _, fsys := realOptions()
	m := loaded(t, opts, 120, 50)
	m = selectPackage(t, m, "linux-firmware") // manual intervention news
	before := len(m.result.Flagged())
	m = send(t, m, press("a"))
	e, _ := m.updates.selected()
	if e.pkg.Name != "linux-firmware" || len(e.pkg.Flags) == 0 {
		t.Fatalf("linux-firmware left NEEDS ATTENTION: %+v", e.pkg)
	}
	for _, f := range e.pkg.Flags {
		if f.Severity != risk.Critical {
			t.Errorf("non-critical flag left: %+v", f)
		}
	}
	if len(m.result.Flagged()) != before || len(m.result.AcknowledgedPackages()) != 0 {
		t.Error("a package with a critical flag moved out of NEEDS ATTENTION")
	}
	if len(m.result.Interventions) != 1 {
		t.Error("the intervention banner went away")
	}
	for _, rule := range savedState(t, fsys).Acknowledged("linux-firmware", e.pkg.NewVersion) {
		if rule == risk.RuleNews.String() {
			t.Error("the critical news rule was recorded")
		}
	}

	// With only critical flags left, there is nothing to acknowledge.
	m = send(t, m, press("a"))
	if !strings.Contains(m.flashMsg, "can't be acknowledged") {
		t.Errorf("flash = %q", m.flashMsg)
	}
}

func TestShowAcknowledgedToggle(t *testing.T) {
	opts, _, _ := realOptions()
	m := loaded(t, opts, 120, 50)
	p := firstNonCritical(t, m)
	m = selectPackage(t, m, p.Name)
	m = send(t, m, press("a"))
	if _, ok := entryNames(m)[p.Name]; !ok {
		t.Fatal("acknowledged package missing by default")
	}
	m = send(t, m, press("A"))
	if _, ok := entryNames(m)[p.Name]; ok {
		t.Error("acknowledged package still listed after hiding")
	}
	m = send(t, m, press("A"))
	if _, ok := entryNames(m)[p.Name]; !ok {
		t.Error("acknowledged package not back after showing")
	}
}

func TestAcknowledgementInHelp(t *testing.T) {
	m := loaded(t, fixtureOptions(), 120, 50)
	m = send(t, m, press("?"))
	out := plain(m)
	for _, want := range []string{"acknowledge flags", "show/hide acknowledged"} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %q:\n%s", want, out)
		}
	}
}

// The state file written before an upgrade or a restart is read back.
func TestAcknowledgementSurvivesRestart(t *testing.T) {
	opts, _, fsys := realOptions()
	m := loaded(t, opts, 120, 50)
	p := firstNonCritical(t, m)
	m = selectPackage(t, m, p.Name)
	m = send(t, m, press("a"))
	_ = m

	opts2, _, _ := realOptions()
	opts2.FS, opts2.State = fsys, savedState(t, fsys)
	m2 := loaded(t, opts2, 120, 50)
	if n := len(m2.result.AcknowledgedPackages()); n != 1 {
		t.Errorf("acknowledged after restart = %d, want 1", n)
	}
}
