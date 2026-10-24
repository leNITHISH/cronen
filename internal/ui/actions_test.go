package ui

import (
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/system"
)

const (
	testStatePath = "/home/u/.local/state/cronen/state.json"
	firmwareGUID  = "tag:archlinux.org,2026-10-22:/news/linux-firmware-20251021-1-upgrade-requires-manual-intervention/"
	nvidiaGUID    = "tag:archlinux.org,2026-10-21:/news/nvidia-590-driver-drops-pascal-support/"
)

// realOptions is fixtureOptions outside demo mode: links are opened and
// state is saved, through fakes.
func realOptions() (Options, *system.FakeRunner, *system.FakeFS) {
	opts := fixtureOptions()
	opts.Demo = false
	r := fixtures.Runner()
	fsys := fixtures.FS()
	opts.Runner, opts.FS, opts.StatePath = r, fsys, testStatePath
	return opts, r, fsys
}

func typeText(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		m = send(t, m, press(string(r)))
	}
	return m
}

func selectPackage(t *testing.T, m Model, name string) Model {
	t.Helper()
	m = send(t, m, press("g"))
	for e, _ := m.updates.selected(); e.pkg.Name != name; e, _ = m.updates.selected() {
		before := m.updates.sel
		m = send(t, m, press("j"))
		if m.updates.sel == before {
			t.Fatalf("%s not in the list", name)
		}
	}
	return m
}

func savedState(t *testing.T, fsys system.FS) config.State {
	t.Helper()
	st, err := config.LoadState(fsys, testStatePath)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func xdgOpenCalls(r *system.FakeRunner) []string {
	var out []string
	for _, c := range r.Calls() {
		if strings.HasPrefix(c, "xdg-open ") {
			out = append(out, c)
		}
	}
	return out
}

func TestOpenLinks(t *testing.T) {
	opts, r, _ := realOptions()
	const (
		upstream  = "https://github.com/morganamilo/paru"
		changelog = "https://aur.archlinux.org/cgit/aur.git/log/?h=paru"
		post      = "https://archlinux.org/news/linux-firmware-20251021-1-upgrade-requires-manual-intervention/"
	)
	for _, u := range []string{upstream, changelog, post} {
		r.Set("xdg-open "+u, system.FakeResult{})
	}
	m := selectPackage(t, loaded(t, opts, 100, 30), "paru")
	m = send(t, m, press("o"))
	m = send(t, m, press("c"))
	m = send(t, m, press("n"))
	m = send(t, m, press("o"))
	want := []string{"xdg-open " + upstream, "xdg-open " + changelog, "xdg-open " + post}
	if got := xdgOpenCalls(r); !slices.Equal(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
	if m.flashErr {
		t.Errorf("unexpected error: %s", m.flashMsg)
	}

	m = send(t, m, press("c"))
	if !strings.Contains(plain(m), "news posts have no changelog") {
		t.Errorf("c on the news screen should explain itself:\n%s", plain(m))
	}
	if got := xdgOpenCalls(r); len(got) != 3 {
		t.Errorf("c on the news screen ran %q", got)
	}
}

func TestOpenLinkXdgOpenMissing(t *testing.T) {
	opts, r, _ := realOptions()
	r.Set("xdg-open https://github.com/morganamilo/paru", system.FakeResult{Err: system.ErrCommandNotFound})
	m := selectPackage(t, loaded(t, opts, 100, 30), "paru")
	m = send(t, m, press("o"))
	if !m.flashErr || !strings.Contains(plain(m), "xdg-open not found — install xdg-utils") {
		t.Errorf("want the xdg-utils hint:\n%s", plain(m))
	}
	// The message goes away with the next key.
	m = send(t, m, press("j"))
	if strings.Contains(plain(m), "xdg-utils") {
		t.Errorf("flash message stuck:\n%s", plain(m))
	}
}

func TestOpenLinkDemo(t *testing.T) {
	m := selectPackage(t, loaded(t, fixtureOptions(), 100, 30), "paru")
	r := m.opts.Runner.(*system.FakeRunner)
	m = send(t, m, press("o"))
	if got := xdgOpenCalls(r); len(got) != 0 {
		t.Errorf("demo ran %q", got)
	}
	if !strings.Contains(plain(m), "demo: would open https://github.com/morganamilo/paru") {
		t.Errorf("want a demo message:\n%s", plain(m))
	}
}

func TestOpenLinkArchwebPending(t *testing.T) {
	opts, r, _ := realOptions()
	m := loaded(t, opts, 100, 30)
	e, _ := m.updates.selected()
	m.archweb[e.pkg.Name] = archwebResult{}
	m = send(t, m, press("o"))
	if !strings.Contains(m.flashMsg, "still looking up") || len(xdgOpenCalls(r)) != 0 {
		t.Errorf("flash %q, calls %q", m.flashMsg, xdgOpenCalls(r))
	}
}

func TestSafeURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://example.org/a?b=c", "https://example.org/a?b=c"},
		{"http://example.org", "http://example.org"},
		{" https://example.org/\x1b[31mx\x07 ", "https://example.org/[31mx"},
		{"-https://example.org", ""},
		{"--help", ""},
		{"file:///etc/passwd", ""},
		{"javascript:alert(1)", ""},
		{"https://", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := safeURL(tt.in); got != tt.want {
			t.Errorf("safeURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMarkReadNews(t *testing.T) {
	opts, _, fsys := realOptions()
	m := loaded(t, opts, 100, 30)
	m = send(t, m, press("n"))
	if it := m.newsItems[m.newsPane.sel]; it.GUID != firmwareGUID {
		t.Fatalf("first news item is %q", it.GUID)
	}
	m = send(t, m, press("m"))
	if m.unread[firmwareGUID] || len(m.unreadNews) != 1 {
		t.Errorf("unread = %v after marking read", m.unread)
	}
	if len(m.result.Interventions) != 0 || strings.Contains(plain(m), "requires manual intervention —") {
		t.Errorf("banner still up:\n%s", plain(m))
	}
	checkSize(t, m, 100, 30)
	if st := savedState(t, fsys); !st.IsRead(firmwareGUID) || st.IsRead(nvidiaGUID) {
		t.Errorf("saved read news = %q", st.ReadNews())
	}

	m = send(t, m, press("m"))
	if m.flashMsg != "already read" {
		t.Errorf("flash = %q", m.flashMsg)
	}
}

func TestMarkReadFromPackage(t *testing.T) {
	opts, _, fsys := realOptions()
	m := selectPackage(t, loaded(t, opts, 100, 30), "nvidia-utils")
	m = send(t, m, press("m"))
	if m.unread[nvidiaGUID] || !m.unread[firmwareGUID] {
		t.Errorf("unread = %v", m.unread)
	}
	if st := savedState(t, fsys); !st.IsRead(nvidiaGUID) {
		t.Errorf("saved read news = %q", st.ReadNews())
	}
	// The selection stays on the package as it loses its news flag.
	if e, _ := m.updates.selected(); e.pkg.Name != "nvidia-utils" {
		t.Errorf("selected %q", e.pkg.Name)
	}

	m = selectPackage(t, m, "zstd")
	m = send(t, m, press("m"))
	if !strings.Contains(m.flashMsg, "no unread news") {
		t.Errorf("flash = %q", m.flashMsg)
	}
}

func TestMarkReadDemoWritesNothing(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 30)
	fsys := m.opts.FS.(*system.FakeFS)
	m = send(t, m, press("n"))
	m = send(t, m, press("m"))
	if m.unread[firmwareGUID] {
		t.Error("not marked read in memory")
	}
	if _, err := fsys.ReadFile(testStatePath); err == nil {
		t.Error("demo wrote state")
	}
	if m.flashErr {
		t.Errorf("demo warned: %s", m.flashMsg)
	}
}

func TestMarkReadSaveError(t *testing.T) {
	opts, _, _ := realOptions()
	opts.FS = failingWriteFS{opts.FS}
	m := loaded(t, opts, 100, 30)
	m = send(t, m, press("n"))
	m = send(t, m, press("m"))
	if !m.flashErr || !strings.Contains(m.flashMsg, "couldn't save state") {
		t.Errorf("flash = %q", m.flashMsg)
	}
}

type failingWriteFS struct{ system.FS }

func (failingWriteFS) WriteFile(string, []byte, fs.FileMode) error {
	return errors.New("read-only file system")
}

func TestFilter(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 30)
	m = send(t, m, press("/"))
	if !m.filtering {
		t.Fatal("/ did not start the filter")
	}
	// App keys are typed into the filter, not acted on.
	m = typeText(t, m, "nrqU?")
	if m.screen != screenUpdates || m.gen != 1 || m.filter.Value() != "nrqU?" {
		t.Fatalf("screen %v, gen %d, filter %q", m.screen, m.gen, m.filter.Value())
	}
	if len(m.updates.entries) != 0 || !strings.Contains(plain(m), "No packages match") {
		t.Errorf("want no matches:\n%s", plain(m))
	}
	m = send(t, m, press("esc"))
	if m.filtering || m.filter.Value() != "" || len(m.updates.entries) == 0 {
		t.Fatalf("esc did not clear the filter")
	}

	m = send(t, m, press("/"))
	m = typeText(t, m, "LINUX")
	m = send(t, m, press("enter"))
	if m.filtering {
		t.Fatal("enter did not end filter input")
	}
	var names []string
	for _, e := range m.updates.entries {
		names = append(names, e.pkg.Name)
	}
	want := []string{"linux-firmware", "linux", "linux-api-headers", "linux-headers"}
	if !slices.Equal(names, want) {
		t.Errorf("entries = %q, want %q", names, want)
	}
	golden(t, "filter", plain(m))
	checkSize(t, m, 100, 30)

	// Keys act on the app again, and esc clears the kept filter.
	m = send(t, m, press("j"))
	if e, _ := m.updates.selected(); e.pkg.Name != "linux" {
		t.Errorf("selected %q after j", e.pkg.Name)
	}
	m = send(t, m, press("esc"))
	if m.filter.Value() != "" || len(m.updates.entries) <= len(want) {
		t.Errorf("esc did not clear the kept filter")
	}
}

func TestFilterCtrlCQuits(t *testing.T) {
	m := send(t, loaded(t, fixtureOptions(), 100, 30), press("/"))
	_, cmd := m.Update(press("ctrl+c"))
	if cmd == nil || !isQuit(cmd()) {
		t.Error("ctrl+c did not quit while filtering")
	}
}

// fakeExec records the upgrade command and exits with err.
type fakeExec struct {
	argv [][]string
	err  error
}

func (f *fakeExec) exec(argv []string, done tea.ExecCallback) tea.Cmd {
	f.argv = append(f.argv, argv)
	return func() tea.Msg { return done(f.err) }
}

func TestUpgradeConfirm(t *testing.T) {
	opts, _, _ := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	m := loaded(t, opts, 100, 30)
	m = send(t, m, press("U"))
	if m.screen != screenConfirm {
		t.Fatalf("screen = %v", m.screen)
	}
	golden(t, "confirm", plain(m))
	checkSize(t, m, 100, 30)

	// Only y proceeds; other keys do nothing, n cancels.
	for _, k := range []string{"j", "r", "U", "enter", "/"} {
		m = send(t, m, press(k))
	}
	if m.screen != screenConfirm || len(ex.argv) != 0 || m.gen != 1 {
		t.Fatalf("screen %v, ran %q, gen %d", m.screen, ex.argv, m.gen)
	}
	m = send(t, m, press("n"))
	if m.screen != screenUpdates || len(ex.argv) != 0 {
		t.Fatalf("n: screen %v, ran %q", m.screen, ex.argv)
	}
	m = send(t, m, press("U"))
	m = send(t, m, press("esc"))
	if m.screen != screenUpdates || len(ex.argv) != 0 {
		t.Fatalf("esc: screen %v, ran %q", m.screen, ex.argv)
	}
}

func TestUpgradeSuccess(t *testing.T) {
	opts, _, fsys := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	m := loaded(t, opts, 100, 30)
	m = send(t, m, press("U"))
	m = send(t, m, press("y"))

	if len(ex.argv) != 1 || strings.Join(ex.argv[0], " ") != "sudo pacman -Syu" {
		t.Fatalf("ran %q", ex.argv)
	}
	if m.gen != 2 || m.anyLoading() {
		t.Errorf("gen %d, loading %v: want a finished refresh", m.gen, m.anyLoading())
	}
	if !m.opts.State.LastUpgrade.Equal(fixtures.Now) {
		t.Errorf("last upgrade = %v", m.opts.State.LastUpgrade)
	}
	st := savedState(t, fsys)
	if !st.LastUpgrade.Equal(fixtures.Now) || !st.IsRead(firmwareGUID) || !st.IsRead(nvidiaGUID) {
		t.Errorf("saved state: last %v, read %q", st.LastUpgrade, st.ReadNews())
	}
	if len(m.unreadNews) != 0 || len(m.result.Interventions) != 0 {
		t.Errorf("unread %d, interventions %d after upgrading", len(m.unreadNews), len(m.result.Interventions))
	}
	// The fixture system still lists the kernel afterwards, but the reboot
	// note must not depend on that.
	golden(t, "upgraded", plain(m))
	checkSize(t, m, 100, 30)

	m = send(t, m, press("esc"))
	if strings.Contains(plain(m), "upgrade finished") {
		t.Error("esc did not dismiss the notice")
	}
	checkSize(t, m, 100, 30)
}

func TestUpgradeRebootDecidedBeforehand(t *testing.T) {
	opts, r, _ := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	m := loaded(t, opts, 100, 30)
	// After the upgrade, nothing is pending any more.
	r.Set("checkupdates", fixtures.CheckupdatesNoneResult())
	m = send(t, m, press("U"))
	m = send(t, m, press("y"))
	if m.result.RebootRequired {
		t.Fatal("kernel still pending after the refresh")
	}
	out := plain(m)
	if !strings.Contains(out, "✓ upgrade finished · reboot to run the new kernel · 1 .pacnew file to merge (run pacdiff)") {
		t.Errorf("notice lacks the reboot note or .pacnew count:\n%s", out)
	}
}

func TestUpgradeFailure(t *testing.T) {
	opts, _, fsys := realOptions()
	ex := &fakeExec{err: errors.New("exit status 1")}
	opts.Exec = ex.exec
	m := loaded(t, opts, 100, 30)
	m = send(t, m, press("U"))
	m = send(t, m, press("y"))
	if m.gen != 2 {
		t.Errorf("gen %d: want a refresh after a failed upgrade too", m.gen)
	}
	if !m.opts.State.LastUpgrade.IsZero() || len(m.opts.State.ReadNews()) != 0 {
		t.Errorf("state changed after a failed upgrade: %+v", m.opts.State)
	}
	if _, err := fsys.ReadFile(testStatePath); err == nil {
		t.Error("state written after a failed upgrade")
	}
	if !strings.Contains(plain(m), "✗ upgrade failed (exit status 1); news not marked read") {
		t.Errorf("no failure notice:\n%s", plain(m))
	}
	checkSize(t, m, 100, 30)
}

func TestUpgradeCommandFromConfig(t *testing.T) {
	opts, _, _ := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	cfg := config.Default()
	cfg.AURHelper = "paru"
	opts.Config = &cfg
	m := loaded(t, opts, 100, 30)
	if !strings.Contains(plain(send(t, m, press("U"))), "This runs: paru -Syu") {
		t.Error("confirm modal doesn't show the paru command")
	}
	send(t, send(t, m, press("U")), press("y"))
	if len(ex.argv) != 1 || strings.Join(ex.argv[0], " ") != "paru -Syu" {
		t.Errorf("ran %q", ex.argv)
	}
}

func TestUpgradeUnavailable(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 30)
	m = send(t, m, press("U"))
	m = send(t, m, press("y"))
	if m.screen != screenUpdates || !m.flashErr {
		t.Errorf("screen %v, flash %q", m.screen, m.flashMsg)
	}
}
