package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/snapshot"
	"github.com/leNITHISH/cronen/internal/system"
)

// snapExec records every hand-off, refuses (as a test failure) any argv the
// snapshot Guard or the model of the real tools would reject, and exits
// with the error set for the command: snapErr for the snapshot create,
// delErr for a delete, 1-based by call number in failDelete.
type snapExec struct {
	t          *testing.T
	argv       [][]string
	snapErr    error
	upgradeErr error
	failDelete int // fail the Nth delete, 0 = none
	deletes    int
	guard      snapshot.Guard
}

func (e *snapExec) exec(argv []string, done tea.ExecCallback) tea.Cmd {
	e.t.Helper()
	e.argv = append(e.argv, argv)
	if msg := system.InvalidCall(argv[0], argv[1:]); msg != "" {
		e.t.Errorf("%q: the real tool would refuse it: %s", argv, msg)
	}
	var err error
	switch {
	case isSnapshotCmd(argv):
		if gerr := e.guard.Verify(argv); gerr != nil {
			e.t.Errorf("guard refused a snapshot command: %v", gerr)
		}
		switch {
		case contains(argv, "--create") || contains(argv, "create"):
			err = e.snapErr
		case contains(argv, "--delete") || contains(argv, "delete"):
			e.deletes++
			if e.failDelete == e.deletes {
				err = errors.New("exit status 1")
			}
		}
	default:
		err = e.upgradeErr
	}
	return func() tea.Msg { return done(err) }
}

func isSnapshotCmd(argv []string) bool {
	for _, a := range argv {
		if a == "timeshift" || a == "snapper" {
			return true
		}
	}
	return false
}

func contains(argv []string, s string) bool {
	for _, a := range argv {
		if a == s {
			return true
		}
	}
	return false
}

func (e *snapExec) lines() []string {
	var out []string
	for _, a := range e.argv {
		out = append(out, strings.Join(a, " "))
	}
	return out
}

func snapOptions(t *testing.T, kit fixtures.SnapshotKit, mut func(*config.Config)) (Options, *system.FakeRunner, *system.FakeFS, *snapExec) {
	t.Helper()
	opts, r, fsys := realOptions()
	fixtures.WithSnapshots(r, kit)
	fixtures.WithTimeshiftConfig(fsys)
	cfg := config.Default()
	if mut != nil {
		mut(&cfg)
	}
	opts.Config = &cfg
	ex := &snapExec{t: t, guard: snapshot.Guard{Config: cfg.SnapperConfig}}
	opts.Exec = ex.exec
	return opts, r, fsys, ex
}

const (
	chainSnapshot = "sudo timeshift --create --comments cronen: before -Syu (18 packages)"
	upgradeLine   = "sudo pacman -Syu"
)

func TestNoSnapshotToolHidesTheSnapshotUI(t *testing.T) {
	opts, r, _, ex := snapOptions(t, fixtures.SnapshotKit{}, nil)
	m := loaded(t, opts, 100, 40)
	if m.snap.loaded && m.snap.info.Found() {
		t.Fatal("found a tool")
	}
	m = send(t, m, press("U"))
	view := plain(m)
	if strings.Contains(view, "snapshot") || strings.Contains(view, "Snapshot") {
		t.Errorf("the upgrade modal mentions snapshots:\n%s", view)
	}
	// t says why it does nothing; y upgrades as before.
	m = send(t, m, press("t"))
	if !strings.Contains(m.flashMsg, "no snapshot tool") || m.screen != screenConfirm {
		t.Errorf("t: flash %q screen %v", m.flashMsg, m.screen)
	}
	m = send(t, m, press("y"))
	if got := ex.lines(); len(got) != 1 || got[0] != upgradeLine {
		t.Errorf("ran %q", got)
	}
	// Housekeeping shows no Snapshots section and no error; S and R explain.
	m = send(t, m, press("5"))
	if strings.Contains(plain(m), "Snapshots") {
		t.Errorf("housekeeping shows a section:\n%s", plain(m))
	}
	m = send(t, m, press("S"))
	if m.snapAct != nil || !strings.Contains(m.flashMsg, "no snapshot tool") {
		t.Errorf("S: %q", m.flashMsg)
	}
	for _, c := range r.Calls() {
		if strings.Contains(c, "sudo") {
			t.Errorf("ran %q", c)
		}
	}
}

func TestUpgradeChainsSnapshotThenUpgrade(t *testing.T) {
	opts, r, fsys, ex := snapOptions(t, fixtures.SnapshotKit{Timeshift: true}, nil)
	m := loaded(t, opts, 110, 40)
	m = send(t, m, press("U"))
	if !m.snap.take {
		t.Fatal("the box does not start ticked with a tool and no hook")
	}
	golden(t, "confirm-snapshot", plain(m))
	checkSize(t, m, 110, 40)
	if len(ex.argv) != 0 {
		t.Fatalf("ran %q before y", ex.lines())
	}
	m = send(t, m, press("y"))
	got := ex.lines()
	if len(got) != 2 || got[0] != chainSnapshot || got[1] != upgradeLine {
		t.Fatalf("ran %q", got)
	}
	// The comment is one argv element after the flag, never joined.
	if a := ex.argv[0]; len(a) != 5 || a[3] != "--comments" || a[4] != "cronen: before -Syu (18 packages)" {
		t.Errorf("argv %q", a)
	}
	// The snapshot is recorded, then the upgrade finishes as usual.
	recs := m.opts.State.Snapshots()
	if len(recs) != 1 || recs[0].Tool != "timeshift" || recs[0].Comment != "cronen: before -Syu (18 packages)" || !recs[0].At.Equal(fixtures.Now) {
		t.Errorf("records %+v", recs)
	}
	if !m.opts.State.LastUpgrade.Equal(fixtures.Now) || m.upgrade.err != nil || !m.upgrade.done {
		t.Errorf("upgrade %+v last %v", m.upgrade, m.opts.State.LastUpgrade)
	}
	if st := savedState(t, fsys); len(st.Snapshots()) != 1 {
		t.Errorf("saved state has %d snapshot records", len(st.Snapshots()))
	}
	for _, c := range r.Calls() {
		if strings.HasPrefix(c, "sudo") {
			t.Errorf("the read-only runner ran %q", c)
		}
	}
}

func TestFailedSnapshotStopsTheUpgrade(t *testing.T) {
	opts, _, _, ex := snapOptions(t, fixtures.SnapshotKit{Timeshift: true}, nil)
	ex.snapErr = errors.New("exit status 1")
	m := loaded(t, opts, 110, 40)
	m = send(t, m, press("U"))
	m = send(t, m, press("y"))

	// The snapshot ran and failed; the upgrade argv was never run.
	if got := ex.lines(); len(got) != 1 || got[0] != chainSnapshot {
		t.Fatalf("ran %q", got)
	}
	for _, l := range ex.lines() {
		if l == upgradeLine {
			t.Fatal("the upgrade ran after a failed snapshot")
		}
	}
	// The modal is back with the error and the separate choice.
	if m.screen != screenConfirm || m.snap.failure == nil || m.snap.take {
		t.Fatalf("screen %v failure %v take %v", m.screen, m.snap.failure, m.snap.take)
	}
	if m.upgrade.done || !m.opts.State.LastUpgrade.Equal(m.opts.State.LastUpgrade) {
		t.Errorf("upgrade %+v", m.upgrade)
	}
	if len(m.opts.State.Snapshots()) != 0 {
		t.Error("a failed snapshot was recorded")
	}
	view := plain(m)
	for _, w := range []string{"the snapshot failed, so the upgrade did not start", "exit status 1", "to upgrade without a snapshot"} {
		if !strings.Contains(view, w) {
			t.Errorf("modal lacks %q:\n%s", w, view)
		}
	}
	golden(t, "confirm-snapshot-failed", view)
	checkSize(t, m, 110, 40)

	// Nothing but a fresh y upgrades; other keys and the old y state don't.
	for _, k := range []string{"j", "r", "enter", "/"} {
		m = send(t, m, press(k))
	}
	if len(ex.argv) != 1 {
		t.Fatalf("ran %q", ex.lines())
	}
	m = send(t, m, press("y"))
	if got := ex.lines(); len(got) != 2 || got[1] != upgradeLine {
		t.Fatalf("ran %q", got)
	}
	if len(m.opts.State.Snapshots()) != 0 || !m.upgrade.done {
		t.Errorf("records %v upgrade %+v", m.opts.State.Snapshots(), m.upgrade)
	}
}

func TestFailedSnapshotCanBeRetriedOrCancelled(t *testing.T) {
	opts, _, _, ex := snapOptions(t, fixtures.SnapshotKit{Timeshift: true}, nil)
	ex.snapErr = errors.New("exit status 1")
	m := loaded(t, opts, 110, 40)
	m = send(t, m, press("U"))
	m = send(t, m, press("y"))
	// t ticks the snapshot again; y retries it (and it fails again).
	m = send(t, m, press("t"))
	if !m.snap.take || m.snap.failure != nil {
		t.Fatalf("take %v failure %v", m.snap.take, m.snap.failure)
	}
	m = send(t, m, press("y"))
	if got := ex.lines(); len(got) != 2 || got[0] != chainSnapshot || got[1] != chainSnapshot {
		t.Fatalf("ran %q", got)
	}
	m = send(t, m, press("esc"))
	if m.screen != screenUpdates || len(ex.argv) != 2 {
		t.Errorf("screen %v, ran %q", m.screen, ex.lines())
	}
}

func TestHookSkipsTheExtraSnapshot(t *testing.T) {
	for _, hook := range snapshot.Hooks {
		opts, _, _, ex := snapOptions(t, fixtures.SnapshotKit{Timeshift: true, Hook: hook}, nil)
		m := loaded(t, opts, 110, 40)
		m = send(t, m, press("U"))
		if m.snap.take {
			t.Fatalf("%s: the box is ticked", hook)
		}
		view := plain(m)
		if !strings.Contains(strings.Join(strings.Fields(view), " "), hook+" already takes a snapshot on every pacman transaction, so cronen adds none") {
			t.Errorf("%s: no hook note:\n%s", hook, view)
		}
		if hook == "timeshift-autosnap" {
			golden(t, "confirm-snapshot-hook", view)
		}
		m = send(t, m, press("y"))
		if got := ex.lines(); len(got) != 1 || got[0] != upgradeLine {
			t.Errorf("%s: ran %q", hook, got)
		}
	}
	// The user can still tick it, and is told it makes two.
	opts, _, _, ex := snapOptions(t, fixtures.SnapshotKit{Timeshift: true, Hook: "snap-pac"}, nil)
	m := loaded(t, opts, 110, 40)
	m = send(t, m, press("U"))
	m = send(t, m, press("t"))
	if !strings.Contains(strings.Join(strings.Fields(plain(m)), " "), "there will be two") {
		t.Errorf("no warning about two snapshots:\n%s", plain(m))
	}
	m = send(t, m, press("y"))
	if got := ex.lines(); len(got) != 2 || got[0] != chainSnapshot {
		t.Errorf("ran %q", got)
	}
}

func TestSnapshotBeforeUpgradeModes(t *testing.T) {
	always, _, _, ex := snapOptions(t, fixtures.SnapshotKit{Timeshift: true, Hook: "snap-pac"}, func(c *config.Config) { c.SnapshotBeforeUpgrade = config.SnapshotAlways })
	m := loaded(t, always, 110, 40)
	m = send(t, m, press("U"))
	if !m.snap.take {
		t.Error("always: not ticked")
	}
	m = send(t, m, press("y"))
	if got := ex.lines(); len(got) != 2 || got[0] != chainSnapshot {
		t.Errorf("always: ran %q", got)
	}

	never, _, _, ex := snapOptions(t, fixtures.SnapshotKit{Timeshift: true}, func(c *config.Config) { c.SnapshotBeforeUpgrade = config.SnapshotNever })
	m = loaded(t, never, 110, 40)
	m = send(t, m, press("U"))
	if m.snap.take || strings.Contains(plain(m), "take a snapshot first") {
		t.Errorf("never: offered:\n%s", plain(m))
	}
	m = send(t, m, press("t"))
	if m.snap.take || !strings.Contains(m.flashMsg, "never") {
		t.Errorf("never: t ticked it (%q)", m.flashMsg)
	}
	m = send(t, m, press("y"))
	if got := ex.lines(); len(got) != 1 || got[0] != upgradeLine {
		t.Errorf("never: ran %q", got)
	}
}

func TestSnapperChain(t *testing.T) {
	opts, _, _, ex := snapOptions(t, fixtures.SnapshotKit{Snapper: true}, func(c *config.Config) { c.PrivilegeCommand = "doas" })
	m := loaded(t, opts, 110, 40)
	m = send(t, m, press("U"))
	m = send(t, m, press("y"))
	got := ex.lines()
	want := "doas snapper -c root create --description cronen: before -Syu (18 packages) --cleanup-algorithm number"
	if len(got) != 2 || got[0] != want || got[1] != "doas pacman -Syu" {
		t.Errorf("ran %q", got)
	}
	if recs := m.opts.State.Snapshots(); len(recs) != 1 || recs[0].Tool != "snapper" {
		t.Errorf("records %+v", recs)
	}
}

func installWith(t *testing.T, m Model, names string) Model {
	t.Helper()
	m = send(t, m, press("I"))
	m = typeText(t, m, names)
	return send(t, m, press("enter"))
}

func TestInstallOptionAChainsSnapshot(t *testing.T) {
	opts, _, _, ex := snapOptions(t, fixtures.SnapshotKit{Timeshift: true}, nil)
	m := installWith(t, loaded(t, opts, 110, 70), "htop")
	if m.inst == nil || m.inst.stage != instReady || !m.snap.take {
		t.Fatalf("flow %+v take %v", m.inst, m.snap.take)
	}
	view := plain(m)
	if !strings.Contains(view, "take a snapshot first (Timeshift)") {
		t.Errorf("option A lacks the snapshot line:\n%s", view)
	}
	golden(t, "install-a-snapshot", view)
	m = send(t, m, press("y"))
	got := ex.lines()
	if len(got) != 2 || !strings.HasPrefix(got[0], "sudo timeshift --create --comments cronen: before -Syu (") || got[1] != "sudo pacman -Syu -- htop" {
		t.Fatalf("ran %q", got)
	}
	if m.inst != nil || len(m.opts.State.Snapshots()) != 1 {
		t.Errorf("inst %v records %v", m.inst, m.opts.State.Snapshots())
	}
}

func TestInstallOptionAFailedSnapshotStopsIt(t *testing.T) {
	opts, _, _, ex := snapOptions(t, fixtures.SnapshotKit{Timeshift: true}, nil)
	ex.snapErr = errors.New("exit status 1")
	m := installWith(t, loaded(t, opts, 110, 70), "htop")
	m = send(t, m, press("y"))
	if got := ex.lines(); len(got) != 1 || !strings.Contains(got[0], "--create") {
		t.Fatalf("ran %q", got)
	}
	if m.inst == nil || m.screen != screenInstall || m.snap.failure == nil {
		t.Fatalf("inst %v screen %v failure %v", m.inst, m.screen, m.snap.failure)
	}
	view := plain(m)
	if !strings.Contains(view, "the snapshot failed, so the upgrade did not start") {
		t.Errorf("no error shown:\n%s", view)
	}
	golden(t, "install-a-snapshot-failed", view)
	m = send(t, m, press("y"))
	if got := ex.lines(); len(got) != 2 || got[1] != "sudo pacman -Syu -- htop" {
		t.Fatalf("ran %q", got)
	}
}

func TestInstallOptionBHasNoSnapshot(t *testing.T) {
	// Option B isn't an upgrade, and t does nothing there.
	opts, _, _, _ := snapOptions(t, fixtures.SnapshotKit{Timeshift: true}, func(c *config.Config) { c.ArchiveInstall = true })
	opts.ArchiveBase = fixtures.ArchiveBase
	n := 0
	opts.Nonce = func() string { n++; return "snap" + string(rune('a'+n)) }
	m := installWith(t, loaded(t, opts, 110, 70), "htop")
	m = send(t, m, press("b"))
	before := m.snap.take
	m = send(t, m, press("t"))
	if m.snap.take != before {
		t.Error("t changed the box while option B is selected")
	}
}

// ---- housekeeping ----

func hkModel(t *testing.T, kit fixtures.SnapshotKit, mut func(*config.Config)) (Model, *snapExec, *system.FakeRunner) {
	t.Helper()
	opts, r, _, ex := snapOptions(t, kit, mut)
	m := loaded(t, opts, 110, 60)
	return send(t, m, press("5")), ex, r
}

func TestHousekeepingTimeshiftWithoutListSudo(t *testing.T) {
	opts, r, _, ex := snapOptions(t, fixtures.SnapshotKit{Timeshift: true, SudoList: true}, nil)
	opts.State.AddSnapshot(config.SnapshotRecord{At: fixtures.Now.Add(-26 * 3600e9), Tool: "timeshift", Comment: "cronen: before -Syu (3 packages)"})
	m := loaded(t, opts, 110, 60)
	m = send(t, m, press("5"))
	view := plain(m)
	flat := strings.Join(strings.Fields(view), " ")
	for _, w := range []string{"Snapshots (Timeshift, rsync mode)", "snapshot_list_sudo = true", "cronen: before -Syu (3 packages)", "their names are unknown"} {
		if !strings.Contains(flat, w) {
			t.Errorf("lacks %q:\n%s", w, view)
		}
	}
	golden(t, "housekeeping-snapshots-timeshift", view)
	checkSize(t, m, 110, 60)
	for _, c := range r.Calls() {
		if strings.HasPrefix(c, "sudo") {
			t.Errorf("listing without snapshot_list_sudo ran %q", c)
		}
	}
	// Nothing to delete or restore: the names are unknown.
	m = send(t, m, press("R"))
	if m.snapAct != nil {
		t.Error("R opened a restore for a recorded snapshot")
	}
	// S takes one now, after a confirmation.
	m = send(t, m, press("S"))
	if m.snapAct == nil || m.snapAct.kind != snapCreate || m.screen != screenConfirm {
		t.Fatalf("S: %+v", m.snapAct)
	}
	golden(t, "snapshot-create", plain(m))
	m = send(t, m, press("n"))
	if len(ex.argv) != 0 {
		t.Fatalf("ran %q without y", ex.lines())
	}
	m = send(t, m, press("y"))
	if got := ex.lines(); len(got) != 1 || got[0] != "sudo timeshift --create --comments cronen: manual snapshot" {
		t.Fatalf("ran %q", got)
	}
	if m.screen != screenHousekeeping || len(m.opts.State.Snapshots()) != 2 || !strings.Contains(m.actionNotice, "snapshot taken") {
		t.Errorf("screen %v records %d notice %q", m.screen, len(m.opts.State.Snapshots()), m.actionNotice)
	}
}

func TestHousekeepingTimeshiftWithListSudo(t *testing.T) {
	m, ex, r := hkModel(t, fixtures.SnapshotKit{Timeshift: true, SudoList: true}, func(c *config.Config) { c.SnapshotListSudo = true })
	view := plain(m)
	if !strings.Contains(view, "2026-10-20_19-02-30") || strings.Contains(view, "snapshot_list_sudo = true allows") {
		t.Errorf("not listed:\n%s", view)
	}
	golden(t, "housekeeping-snapshots-sudo-list", view)
	var sudo []string
	for _, c := range r.Calls() {
		if strings.HasPrefix(c, "sudo") {
			sudo = append(sudo, c)
		}
	}
	if len(sudo) == 0 || sudo[0] != "sudo -n timeshift --list" {
		t.Errorf("sudo calls %q", sudo)
	}
	for _, c := range sudo {
		if c != "sudo -n timeshift --list" {
			t.Errorf("the runner ran %q", c)
		}
	}

	// Cursor to the snapshots, mark the middle one and delete it.
	m = send(t, m, press("G"))
	m = send(t, m, press("k"))
	m = send(t, m, press("space"))
	if len(m.markedSnapshots()) != 1 || m.markedSnapshots()[0].Name != "2026-10-20_19-02-30" {
		t.Fatalf("marked %+v", m.markedSnapshots())
	}
	m = send(t, m, press("X"))
	if m.snapAct == nil || m.snapAct.kind != snapDelete {
		t.Fatalf("X: %+v (flash %q)", m.snapAct, m.flashMsg)
	}
	golden(t, "snapshot-delete", plain(m))
	m = send(t, m, press("y"))
	if got := ex.lines(); len(got) != 1 || got[0] != "sudo timeshift --delete --snapshot 2026-10-20_19-02-30" {
		t.Fatalf("ran %q", got)
	}
	if len(m.snap.marked) != 0 || !strings.Contains(m.actionNotice, "deleted 1 snapshot") {
		t.Errorf("marked %v notice %q", m.snap.marked, m.actionNotice)
	}
}

func TestSnapshotDeleteStopsAtTheFirstFailure(t *testing.T) {
	m, ex, _ := hkModel(t, fixtures.SnapshotKit{Timeshift: true, SudoList: true}, func(c *config.Config) { c.SnapshotListSudo = true })
	ex.failDelete = 2
	m = send(t, m, press("G"))
	m = send(t, m, press("space"))
	m = send(t, m, press("k"))
	m = send(t, m, press("space"))
	m = send(t, m, press("k"))
	m = send(t, m, press("space"))
	if n := len(m.markedSnapshots()); n != 3 {
		t.Fatalf("marked %d", n)
	}
	m = send(t, m, press("X"))
	m = send(t, m, press("y"))
	got := ex.lines()
	if len(got) != 2 {
		t.Fatalf("ran %q (want two: the third must not run after the second fails)", got)
	}
	if !strings.Contains(m.actionNotice, "stopped after 1 of 3") || !m.actionErr {
		t.Errorf("notice %q", m.actionNotice)
	}
}

func TestSnapshotMarksNeedTheCursorOnTheList(t *testing.T) {
	m, ex, _ := hkModel(t, fixtures.SnapshotKit{Timeshift: true, SudoList: true}, func(c *config.Config) { c.SnapshotListSudo = true })
	// The cursor starts on the first orphan: X is the orphan removal, and
	// R has no snapshot to restore.
	m = send(t, m, press("R"))
	if m.snapAct != nil || !strings.Contains(m.flashMsg, "Snapshots list") {
		t.Errorf("R: %+v %q", m.snapAct, m.flashMsg)
	}
	m = send(t, m, press("G"))
	m = send(t, m, press("X"))
	if m.snapAct != nil || !strings.Contains(m.flashMsg, "no snapshots marked") {
		t.Errorf("X with nothing marked: %+v %q", m.snapAct, m.flashMsg)
	}
	if len(ex.argv) != 0 {
		t.Errorf("ran %q", ex.lines())
	}
}

func TestRestoreRequiresTypingRestore(t *testing.T) {
	m, ex, _ := hkModel(t, fixtures.SnapshotKit{Timeshift: true, SudoList: true}, func(c *config.Config) { c.SnapshotListSudo = true })
	m = send(t, m, press("G"))
	m = send(t, m, press("k"))
	m = send(t, m, press("R"))
	if m.snapAct == nil || m.snapAct.kind != snapRestore {
		t.Fatalf("R: %+v (%q)", m.snapAct, m.flashMsg)
	}
	view := plain(m)
	for _, w := range []string{"2026-10-20_19-02-30", "reboot", "lost", "Type restore"} {
		if !strings.Contains(view, w) {
			t.Errorf("restore modal lacks %q:\n%s", w, view)
		}
	}
	golden(t, "snapshot-restore", view)

	// y, enter and almost-right words do nothing.
	for _, k := range []string{"y", "enter"} {
		m = send(t, m, press(k))
	}
	m = typeText(t, m, "restor")
	m = send(t, m, press("enter"))
	m = send(t, m, press("backspace"))
	m = typeText(t, m, "e")
	if len(ex.argv) != 0 {
		t.Fatalf("ran %q before restore was typed", ex.lines())
	}
	// "y" was typed into the box, so the text is wrong now.
	if m.snapAct.typed == restoreWord {
		t.Fatalf("typed %q", m.snapAct.typed)
	}
	m.snapAct.typed = ""
	m = typeText(t, m, "restore")
	if len(ex.argv) != 0 {
		t.Fatalf("ran %q before enter", ex.lines())
	}
	m = send(t, m, press("enter"))
	if got := ex.lines(); len(got) != 1 || got[0] != "sudo timeshift --restore --snapshot 2026-10-20_19-02-30" {
		t.Fatalf("ran %q", got)
	}
	if !strings.Contains(m.actionNotice, "reboot") {
		t.Errorf("notice %q", m.actionNotice)
	}
}

func TestRestoreEscCancels(t *testing.T) {
	m, ex, _ := hkModel(t, fixtures.SnapshotKit{Timeshift: true, SudoList: true}, func(c *config.Config) { c.SnapshotListSudo = true })
	m = send(t, m, press("G"))
	m = send(t, m, press("R"))
	m = typeText(t, m, "restore")
	m = send(t, m, press("esc"))
	if m.snapAct != nil || m.screen != screenHousekeeping || len(ex.argv) != 0 {
		t.Errorf("act %v screen %v ran %q", m.snapAct, m.screen, ex.lines())
	}
}

func TestSnapperListsAndRestoreIsOnlyShown(t *testing.T) {
	m, ex, r := hkModel(t, fixtures.SnapshotKit{Snapper: true}, nil)
	view := plain(m)
	for _, w := range []string{"Snapshots (snapper)", "#4", "cronen: before -Syu (41 packages)", "pre", "post"} {
		if !strings.Contains(view, w) {
			t.Errorf("lacks %q:\n%s", w, view)
		}
	}
	golden(t, "housekeeping-snapshots-snapper", view)
	for _, c := range r.Calls() {
		if strings.Contains(c, "sudo") {
			t.Errorf("ran %q", c)
		}
	}

	m = send(t, m, press("G"))
	m = send(t, m, press("R"))
	if m.snapAct == nil || m.snapAct.copyCmd != "sudo snapper -c root rollback 4" {
		t.Fatalf("R: %+v", m.snapAct)
	}
	view = plain(m)
	if !strings.Contains(view, "sudo snapper -c root rollback 4") || !strings.Contains(view, "does not run it") {
		t.Errorf("modal:\n%s", view)
	}
	golden(t, "snapper-restore", view)
	// No key and no typed word hands anything off.
	for _, k := range []string{"y", "enter"} {
		m = send(t, m, press(k))
	}
	m = typeText(t, m, "restore")
	m = send(t, m, press("enter"))
	if len(ex.argv) != 0 || m.snapSeq != nil {
		t.Fatalf("a snapper rollback was handed off: %q", ex.lines())
	}
	m = send(t, m, press("esc"))
	if m.snapAct != nil {
		t.Error("esc did not close it")
	}

	// Deleting works, one command per snapshot.
	m = send(t, m, press("space"))
	m = send(t, m, press("X"))
	m = send(t, m, press("y"))
	if got := ex.lines(); len(got) != 1 || got[0] != "sudo snapper -c root delete 4" {
		t.Errorf("ran %q", got)
	}
}

func TestSnapperDeniedAndUnreadable(t *testing.T) {
	m, _, _ := hkModel(t, fixtures.SnapshotKit{Snapper: true, SnapperDenied: true}, nil)
	view := plain(m)
	flat := strings.Join(strings.Fields(view), " ")
	for _, w := range []string{"Snapshots (snapper)", "refused to list", "ALLOW_USERS", "No permissions"} {
		if !strings.Contains(flat, w) {
			t.Errorf("denied lacks %q:\n%s", w, view)
		}
	}
	if strings.Contains(view, "none yet") {
		t.Error("permission denied looked like an empty list")
	}
	golden(t, "housekeeping-snapshots-denied", view)

	opts, r, _, _ := snapOptions(t, fixtures.SnapshotKit{Snapper: true}, nil)
	r.Set("snapper -c root list", system.FakeResult{Stdout: []byte("garbage | in\n")})
	m = send(t, loaded(t, opts, 110, 60), press("5"))
	view = plain(m)
	if !strings.Contains(view, "could not read the snapshot list") || strings.Contains(view, "none yet") {
		t.Errorf("unreadable:\n%s", view)
	}
	golden(t, "housekeeping-snapshots-unreadable", view)
}

func TestSnapshotPreflightBanner(t *testing.T) {
	m, _, _ := hkModel(t, fixtures.SnapshotKit{Timeshift: true}, nil)
	found := false
	for _, p := range m.pf.problems {
		found = found || strings.Contains(p.Message, "No Timeshift snapshot is known")
	}
	if !found {
		t.Errorf("problems %+v", m.pf.problems)
	}
}

func TestDemoSnapshotChainRunsNothing(t *testing.T) {
	// The demo's Exec ends every command with success without running it.
	opts, _, _, ex := snapOptions(t, fixtures.SnapshotKit{Timeshift: true}, nil)
	opts.Demo = true
	m := loaded(t, opts, 110, 40)
	m = send(t, m, press("5"))
	m = send(t, m, press("S"))
	m = send(t, m, press("y"))
	if len(ex.argv) != 0 || !strings.Contains(m.flashMsg, "demo: nothing is run") {
		t.Errorf("ran %q, flash %q", ex.lines(), m.flashMsg)
	}
}
