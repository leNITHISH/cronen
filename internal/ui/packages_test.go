package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/system"
)

func packagesScreen(t *testing.T, w, h int) Model {
	t.Helper()
	m := loaded(t, fixtureOptions(), w, h)
	return send(t, m, press("3"))
}

func TestPackagesScreenSnapshots(t *testing.T) {
	m := packagesScreen(t, 100, 30)
	golden(t, "packages", plain(m))
	checkSize(t, m, 100, 30)

	// A dependency in the middle of a chain, with its explanation.
	m = send(t, m, press("/"))
	for _, r := range "libx11" {
		m = send(t, m, press(string(r)))
	}
	m = send(t, m, press("enter"))
	golden(t, "packages-why", plain(m))

	narrow := packagesScreen(t, 60, 20)
	golden(t, "packages-narrow", plain(narrow))
	checkSize(t, narrow, 60, 20)
}

func TestPackagesFilterAndSort(t *testing.T) {
	m := packagesScreen(t, 100, 30)
	total := len(m.pkgs.rows)
	if total < 40 {
		t.Fatalf("only %d packages listed", total)
	}
	names := func() []string {
		var out []string
		for _, r := range m.pkgs.rows {
			out = append(out, m.localDB.Packages[r].Name)
		}
		return out
	}
	has := func(n string) bool {
		for _, x := range names() {
			if x == n {
				return true
			}
		}
		return false
	}

	// f cycles all → explicit → dependencies → foreign → orphans → all.
	want := []struct {
		filter pkgFilter
		in     string
		out    string
	}{
		{pkgExplicit, "vim", "glibc"},
		{pkgDeps, "glibc", "vim"},
		{pkgForeign, "paru", "glibc"},
		{pkgOrphans, "old-codec-lib", "paru"},
		{pkgAll, "paru", ""},
	}
	for _, w := range want {
		m = send(t, m, press("f"))
		if m.pkgs.filter != w.filter {
			t.Fatalf("filter = %v, want %v", m.pkgs.filter, w.filter)
		}
		if !has(w.in) || (w.out != "" && has(w.out)) {
			t.Errorf("filter %v: has %s = %v, has %s = %v", w.filter, w.in, has(w.in), w.out, has(w.out))
		}
	}
	if len(m.pkgs.rows) != total {
		t.Errorf("back to all: %d rows, want %d", len(m.pkgs.rows), total)
	}

	// s cycles name → size → install date.
	for i := 1; i < len(names()); i++ {
		if names()[i-1] >= names()[i] {
			t.Fatalf("name order broken at %s, %s", names()[i-1], names()[i])
		}
	}
	m = send(t, m, press("s"))
	if m.pkgs.sort != sortSize || names()[0] != "linux-firmware" {
		t.Errorf("size sort: %v first %s", m.pkgs.sort, names()[0])
	}
	m = send(t, m, press("s"))
	if m.pkgs.sort != sortDate {
		t.Errorf("sort = %v", m.pkgs.sort)
	}
	m = send(t, m, press("s"))
	if m.pkgs.sort != sortName {
		t.Errorf("sort = %v", m.pkgs.sort)
	}
}

func TestPackagesSelectionSurvivesRefilter(t *testing.T) {
	m := packagesScreen(t, 100, 30)
	m = send(t, m, press("G"))
	last, _ := m.pkgs.selected(m.localDB)
	m = send(t, m, press("s")) // by size: the selection stays on the same package
	if cur, _ := m.pkgs.selected(m.localDB); cur.Name != last.Name {
		t.Errorf("selection moved from %s to %s", last.Name, cur.Name)
	}
	m = send(t, m, press("g"))
	if m.pkgs.sel != 0 {
		t.Errorf("sel = %d after g", m.pkgs.sel)
	}
}

func TestPackagesMarking(t *testing.T) {
	m := packagesScreen(t, 100, 30)
	first, _ := m.pkgs.selected(m.localDB)
	m = send(t, m, press("space"))
	if !m.pkgs.marked[first.Name] {
		t.Fatalf("%s not marked: %v", first.Name, m.pkgs.marked)
	}
	out := plain(m)
	if !strings.Contains(out, "✓ "+first.Name) || !strings.Contains(out, "1 marked") || !strings.Contains(out, "✓ marked") {
		t.Errorf("mark not shown:\n%s", out)
	}
	m = send(t, m, press("j"))
	m = send(t, m, press("space"))
	if len(m.pkgs.marked) != 2 {
		t.Errorf("marked = %v", m.pkgs.marked)
	}
	m = send(t, m, press("space"))
	m = send(t, m, press("k"))
	m = send(t, m, press("space"))
	if len(m.pkgs.marked) != 0 {
		t.Errorf("unmarking left %v", m.pkgs.marked)
	}
	// Nothing acts on marks yet: X and D do nothing.
	m = send(t, m, press("X"))
	m = send(t, m, press("D"))
	if m.screen != screenPackages {
		t.Errorf("screen = %v", m.screen)
	}
}

func TestPackagesNameFilter(t *testing.T) {
	m := packagesScreen(t, 100, 30)
	m = send(t, m, press("/"))
	for _, r := range "NVIDIA" {
		m = send(t, m, press(string(r)))
	}
	if got := len(m.pkgs.rows); got != 2 {
		t.Errorf("nvidia matches %d packages, want nvidia and nvidia-utils", got)
	}
	m = send(t, m, press("esc"))
	if m.filterQuery() != "" || len(m.pkgs.rows) < 40 {
		t.Errorf("esc left filter %q and %d rows", m.filterQuery(), len(m.pkgs.rows))
	}
	m = send(t, m, press("/"))
	for _, r := range "zzz" {
		m = send(t, m, press(string(r)))
	}
	m = send(t, m, press("enter"))
	if out := plain(m); !strings.Contains(out, "no packages match") || !strings.Contains(out, "No packages match “zzz”") {
		t.Errorf("empty filter not explained:\n%s", out)
	}
}

func TestTabsAndNumberKeys(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 30)
	for _, tt := range []struct {
		key  string
		want screen
	}{
		{"3", screenPackages}, {"2", screenNews}, {"4", screenHistory},
		{"5", screenHousekeeping}, {"1", screenUpdates}, {"3", screenPackages},
	} {
		m = send(t, m, press(tt.key))
		if m.screen != tt.want {
			t.Fatalf("after %s: screen %v, want %v", tt.key, m.screen, tt.want)
		}
		checkSize(t, m, 100, 30)
		if !strings.Contains(strings.Split(plain(m), "\n")[1], "1 Updates") {
			t.Errorf("no tab bar under the header:\n%s", plain(m))
		}
	}
	// v1's n still opens the news, and toggles back.
	m = send(t, m, press("n"))
	if m.screen != screenNews {
		t.Errorf("n: screen %v", m.screen)
	}
	m = send(t, m, press("n"))
	if m.screen != screenUpdates {
		t.Errorf("n again: screen %v", m.screen)
	}
	// esc goes back to Updates from any other screen.
	m = send(t, m, press("3"))
	m = send(t, m, press("esc"))
	if m.screen != screenUpdates {
		t.Errorf("esc: screen %v", m.screen)
	}
	m = send(t, m, press("4"))
	if m.screen != screenHistory || !strings.Contains(plain(m), "pacman -S ripgrep") {
		t.Errorf("history screen:\n%s", plain(m))
	}
	// Digits typed into the filter are text, not screen switches.
	m = send(t, m, press("3"))
	m = send(t, m, press("/"))
	m = send(t, m, press("5"))
	if m.screen != screenPackages || m.filter.Value() != "5" {
		t.Errorf("screen %v, filter %q", m.screen, m.filter.Value())
	}
}

func TestPackagesHelpListsKeys(t *testing.T) {
	m := packagesScreen(t, 100, 30)
	m = send(t, m, press("?"))
	out := plain(m)
	for _, want := range []string{"1-5", "mark", "package filter", "package sort"} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %q", want)
		}
	}
	golden(t, "help", out)
}

func TestPackagesUpdateOnlyKeysDoNothing(t *testing.T) {
	m := packagesScreen(t, 100, 30)
	for _, k := range []string{"o", "c", "m", "d", "a", "A"} {
		next := send(t, m, press(k))
		if plain(next) != plain(m) {
			t.Errorf("%s changed the Packages screen", k)
		}
	}
}

func TestPackagesFailure(t *testing.T) {
	opts := fixtureOptions()
	r := fixtures.Runner()
	r.Set("pacman -Qi", system.FakeResult{Err: system.ErrCommandNotFound})
	opts.Runner = r
	m := send(t, loaded(t, opts, 100, 30), press("3"))
	out := plain(m)
	if !strings.Contains(out, "✗ reading the package database") {
		t.Errorf("error not shown inline:\n%s", out)
	}
	// The rest of the app is fine.
	m = send(t, m, press("1"))
	if !strings.Contains(plain(m), "NEEDS ATTENTION") {
		t.Errorf("updates broken by the packages failure:\n%s", plain(m))
	}
	if m.anyLoading() {
		t.Error("still loading")
	}
}

func TestPackageBrowserOffRunsNothing(t *testing.T) {
	opts := fixtureOptions()
	r := fixtures.Runner()
	opts.Runner = r
	cfg := config.Default()
	cfg.PackageBrowser = false
	opts.Config = &cfg
	m := send(t, loaded(t, opts, 100, 30), press("3"))
	for _, c := range r.Calls() {
		if c == "pacman -Qi" || c == "pacman -Qdtq" {
			t.Errorf("ran %q with package_browser off", c)
		}
	}
	if m.anyLoading() {
		t.Error("spinner running with the browser off")
	}
	if !strings.Contains(plain(m), "package browser is off") {
		t.Errorf("not explained:\n%s", plain(m))
	}
}

// The first frame of the Packages screen for ~1,500 packages must take under
// 100 ms: parse the dump, build the model and render.
func TestPackagesLargeFirstFrame(t *testing.T) {
	opts := fixtureOptions()
	r := fixtures.Runner()
	r.Set("pacman -Qi", system.FakeResult{Stdout: fixtures.SyntheticQi(1500)})
	opts.Runner = r
	m := sized(t, opts, 120, 40)
	m = send(t, m, press("3"))

	start := time.Now()
	msg := loadLocalDB(m.ctx(), r, time.UTC, m.gen)()
	next, _ := m.Update(msg)
	m = next.(Model)
	out := m.View().Content
	elapsed := time.Since(start)

	if budget := 100 * time.Millisecond * raceSlowdown; elapsed > budget {
		t.Errorf("parsing 1500 packages and rendering the first frame took %v, budget %v", elapsed, budget)
	}
	t.Logf("1500 packages: first frame in %v", elapsed)
	if len(m.pkgs.rows) != 1500 || !strings.Contains(out, "pkg0000") {
		t.Errorf("rows = %d, frame:\n%s", len(m.pkgs.rows), out)
	}
	checkSize(t, m, 120, 40)

	// And moving around stays cheap.
	start = time.Now()
	m = send(t, m, press("G"))
	m = send(t, m, press("s"))
	if d := time.Since(start); d > 100*time.Millisecond*raceSlowdown {
		t.Errorf("G and s took %v", d)
	}
	if sel, _ := m.pkgs.selected(m.localDB); sel.Name == "" {
		t.Error("no selection")
	}
}

func TestPackageDetailWhyInstalled(t *testing.T) {
	m := packagesScreen(t, 120, 40)
	sel := func(name string) {
		for i, r := range m.pkgs.rows {
			if m.localDB.Packages[r].Name == name {
				m.pkgs.sel, m.pkgs.selName = i, name
			}
		}
		m.refreshDetail()
	}
	for name, want := range map[string]string{
		"libx11":         "libx11 ← gtk3 ← firefox (explicit)",
		"vim":            "you installed it",
		"old-codec-lib":  "no explicitly installed package needs it",
		"libfoo-cycle-a": "no explicitly installed package needs it",
	} {
		sel(name)
		if got := m.detail.GetContent(); !strings.Contains(flat(got), want) {
			t.Errorf("%s: detail lacks %q:\n%s", name, want, got)
		}
	}
}

func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

var _ = tea.Quit
