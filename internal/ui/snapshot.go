package ui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/snapshot"
	"github.com/leNITHISH/cronen/internal/summary"
)

// The snapshot helper. Before an upgrade (the upgrade modal and the install
// helper's option A) cronen can chain two hand-offs: the snapshot, then,
// only if it exited 0, the upgrade. A failed snapshot brings the modal back
// with the error and a separate "upgrade without a snapshot" choice that
// needs y again. In housekeeping, S takes a snapshot now, X deletes the
// marked ones and R restores the one under the cursor. Every command is
// built by internal/snapshot and checked by its Guard right before the
// hand-off.

type snapState struct {
	sec    section
	info   summary.SnapshotInfo
	loaded bool
	// take is the box of the open upgrade or install modal.
	take bool
	// failure is the error of the snapshot the last attempt started with;
	// the modal shows it until the user picks again.
	failure error
	// marked are the snapshots X would delete, by name.
	marked map[string]bool
}

type snapshotsMsg struct {
	gen  int
	info summary.SnapshotInfo
	err  error
}

func loadSnapshots(ctx context.Context, src summary.SnapshotSources, gen int) tea.Cmd {
	return func() tea.Msg {
		info, err := summary.LoadSnapshots(ctx, src)
		return snapshotsMsg{gen: gen, info: info, err: err}
	}
}

func (m Model) snapshotSources() summary.SnapshotSources {
	return summary.SnapshotSources{
		Runner: m.opts.Runner, FS: m.opts.FS, Disk: m.opts.Disk,
		Config: m.cfg, State: m.opts.State, Location: m.location(),
	}
}

// snapshotCmds are the loads of the snapshot helper.
func (m Model) snapshotCmds() []tea.Cmd {
	if !m.cfg.Snapshots || m.opts.Runner == nil {
		return nil
	}
	return []tea.Cmd{loadSnapshots(m.ctx(), m.snapshotSources(), m.gen)}
}

// reloadSnapshots rereads just the snapshots, after an action changed them.
func (m *Model) reloadSnapshots() tea.Cmd {
	if !m.cfg.Snapshots || m.opts.Runner == nil {
		return nil
	}
	m.snap.sec = section{loading: true}
	return loadSnapshots(m.ctx(), m.snapshotSources(), m.gen)
}

func (m *Model) handleSnapshots(msg snapshotsMsg) tea.Cmd {
	if msg.gen != m.gen {
		return nil
	}
	m.snap.sec = section{err: msg.err}
	m.snap.info, m.snap.loaded = msg.info, true
	for n := range m.snap.marked {
		found := false
		for _, s := range msg.info.Snapshots {
			found = found || s.Name == n
		}
		if !found {
			delete(m.snap.marked, n)
		}
	}
	return m.changed()
}

// snapOffered reports whether the snapshot line shows in the upgrade modal.
func (m Model) snapOffered() bool {
	return m.snap.loaded && m.snap.info.Offered(m.cfg.SnapshotBeforeUpgrade)
}

// wantSnapshot reports a ticked box that will chain a snapshot first.
func (m Model) wantSnapshot() bool { return m.snapOffered() && m.snap.take }

// resetSnapshotChoice sets the box for a modal that is opening.
func (m *Model) resetSnapshotChoice() {
	m.snap.failure = nil
	m.snap.take = m.snap.loaded && m.snap.info.DefaultTake(m.cfg.SnapshotBeforeUpgrade)
}

// toggleSnapshot is the t key of the upgrade modal and option A.
func (m *Model) toggleSnapshot() {
	switch {
	case !m.snap.loaded || !m.snap.info.Found():
		m.flash("no snapshot tool found (Timeshift or snapper)", false)
	case m.cfg.SnapshotBeforeUpgrade == config.SnapshotNever:
		m.flash("snapshot_before_upgrade = \"never\"", false)
	default:
		m.snap.take = !m.snap.take
		m.snap.failure = nil
	}
}

// snapshotStep builds the create command for the chain.
func (m Model) snapshotStep(packages int) (argv []string, comment string, err error) {
	info := m.snap.info
	comment = snapshot.Comment(packages)
	argv, err = snapshot.CreateArgv(info.Tool(), m.cfg.PrivilegeArgv(), m.cfg.SnapperConfig, comment)
	if err == nil {
		err = m.snapGuard().Verify(argv)
	}
	return argv, comment, err
}

func (m Model) snapGuard() snapshot.Guard { return snapshot.Guard{Config: m.cfg.SnapperConfig} }

type snapFlow int

const (
	flowUpgrade snapFlow = iota
	flowInstall
)

// snapshotStepMsg reports that the snapshot of a chain exited. The upgrade
// is only started from a nil err.
type snapshotStepMsg struct {
	flow    snapFlow
	tool    snapshot.Tool
	comment string
	err     error
}

// startSnapshotFirst hands off the snapshot, the first half of a chain.
func (m *Model) startSnapshotFirst(flow snapFlow, packages int) tea.Cmd {
	argv, comment, err := m.snapshotStep(packages)
	if err != nil {
		m.flash(err.Error(), true)
		return nil
	}
	m.snap.failure = nil
	tool := m.snap.info.Tool()
	return m.opts.Exec(argv, func(err error) tea.Msg {
		return snapshotStepMsg{flow: flow, tool: tool, comment: comment, err: err}
	})
}

// handleSnapshotStep is the second half of the chain: only a snapshot that
// exited 0 lets the upgrade start.
func (m *Model) handleSnapshotStep(msg snapshotStepMsg) tea.Cmd {
	if msg.err != nil {
		// The upgrade does not start. The modal is still open (nothing
		// closed it); it shows the error and, with the box now unticked,
		// the choice to upgrade without a snapshot.
		m.snap.failure, m.snap.take = msg.err, false
		return m.reloadSnapshots()
	}
	m.opts.State = m.opts.State.Clone()
	m.opts.State.AddSnapshot(summary.SnapshotRecord(msg.tool, msg.comment, m.now()))
	save := m.saveState()
	switch msg.flow {
	case flowInstall:
		f := m.inst
		if f == nil || f.a.Blocked() || len(f.a.Command) == 0 {
			return save
		}
		return tea.Batch(save, m.handoffInstallA(f.a.Command))
	}
	m.screen = screenUpdates
	return tea.Batch(save, m.handoffUpgrade(m.cfg.UpgradeArgv()))
}

// addSnapshotOption adds the snapshot line, the hook note, the chain and a
// failure to a modal. then is the command that follows the snapshot.
func (m Model) addSnapshotOption(add func(string), indent string, packages int, then []string) {
	if !m.snapOffered() {
		return
	}
	s, info := m.styles, m.snap.info
	box := "☐"
	if m.snap.take {
		box = "☑"
	}
	add(indent + box + " take a snapshot first (" + info.Tool().Title() + ")   " + s.Subtle.Render("t toggles"))
	if hook := info.Hook(); hook != "" {
		note := hook + " already takes a snapshot on every pacman transaction, so cronen adds none"
		if m.snap.take {
			note = hook + " already takes a snapshot on every pacman transaction; you ticked this anyway, so there will be two"
		}
		add(indent + "  " + s.Info.Render("ℹ "+note))
	}
	if m.snap.take {
		if argv, _, err := m.snapshotStep(packages); err == nil {
			add(indent + "  Runs first: " + s.Bold.Render(strings.Join(quoteFormat(argv), " ")))
			add(indent + "  Then, only if that exits 0: " + s.Bold.Render(strings.Join(then, " ")))
		} else {
			add(indent + "  " + s.Error.Render("✗ "+err.Error()))
		}
	}
	if err := m.snap.failure; err != nil {
		add(indent + s.Error.Render("✗ the snapshot failed, so the upgrade did not start: "+err.Error()))
	}
}

// ---- housekeeping: the Snapshots section ----

// snapshotRows are the listed snapshots the cursor can stand on, newest
// last as the tool lists them.
func (m Model) snapshotRows() []snapshot.Snapshot {
	if !m.snap.loaded || !m.snap.info.Deletable() {
		return nil
	}
	return m.snap.info.Snapshots
}

func (m Model) markedSnapshots() []snapshot.Snapshot {
	var out []snapshot.Snapshot
	for _, s := range m.snapshotRows() {
		if m.snap.marked[s.Name] {
			out = append(out, s)
		}
	}
	return out
}

// snapshotLines adds the Snapshots section. selStart is the index of the
// first snapshot row among the cursor's rows (after the orphans); the
// returned line is the cursor's line or -1.
func (m Model) snapshotLines(w int, add func(string), title func(string), lineNo func() int, selStart int) int {
	s := m.styles
	addWrapped := func(text string) {
		for _, l := range strings.Split(wrapWords(text, w-3, 2), "\n") {
			add("   " + l)
		}
	}
	switch {
	case !m.cfg.Snapshots:
		return -1
	case !m.snap.loaded:
		// Not known yet: a machine with no tool never sees this section,
		// not even while the detection runs.
		return -1
	}
	info := m.snap.info
	if !info.Found() {
		// Nothing installed is nothing to say. A tool named in the config
		// that is missing is worth one line.
		if info.Detect.Missing != "" {
			title("Snapshots")
			add("   " + s.Subtle.Render(info.Detect.Missing))
		}
		return -1
	}
	head := "Snapshots (" + info.Tool().Title()
	if info.Mode != "" {
		head += ", " + info.Mode + " mode"
	}
	title(head + ")")
	sel := -1
	switch {
	case m.snap.sec.loading:
		add("   " + s.Subtle.Render(m.spinner.View()+" listing…"))
	case info.ListErr != nil:
		add("   " + s.Error.Render("✗ could not read the snapshot list: "+strings.TrimPrefix(info.ListErr.Error(), snapshot.ErrUnreadable.Error()+": ")))
	case info.Listed:
		if len(info.Snapshots) == 0 {
			add("   " + s.Subtle.Render("none yet"))
		}
		for i, sn := range info.Snapshots {
			cursor, mark := "  ", "☐ "
			if m.snap.marked[sn.Name] {
				mark = "☑ "
			}
			if m.hk.sel == selStart+i {
				cursor, sel = "▸ ", lineNo()
			}
			row := "  " + cursor + mark + sn.Name
			if sn.Tool == snapshot.Snapper {
				row = "  " + cursor + mark + "#" + sn.Name
			}
			var bits []string
			if !sn.Date.IsZero() && sn.Tool == snapshot.Snapper {
				bits = append(bits, sn.Date.In(m.location()).Format("2006-01-02 15:04"))
			}
			if sn.Type != "" {
				bits = append(bits, sn.Type)
			}
			if sn.Description != "" {
				bits = append(bits, oneLineText(sn.Description))
			}
			add(row + " " + s.Subtle.Render(strings.Join(bits, " · ")))
		}
		add("   " + s.Subtle.Render("space mark · X delete the marked · R restore the one under the cursor · S take one now"))
	default:
		if info.Note != "" {
			addWrapped(s.Subtle.Render("ℹ " + info.Note))
		}
		if len(info.Records) == 0 {
			add("   " + s.Subtle.Render("no snapshot taken by cronen yet · S take one now"))
		}
		for _, r := range info.Records {
			add("   • " + r.At.In(m.location()).Format("2006-01-02 15:04") + " " + s.Subtle.Render(oneLineText(r.Comment)))
		}
		if len(info.Records) > 0 {
			add("   " + s.Subtle.Render("these can't be deleted or restored from here: their names are unknown · S take one now"))
		}
	}
	if m.snap.sec.err != nil && info.ListErr == nil {
		add("   " + s.Error.Render("✗ "+m.snap.sec.err.Error()))
	}
	if !info.Last.IsZero() {
		age := m.now().Sub(info.Last)
		add("   newest known: " + info.Last.In(m.location()).Format("2006-01-02 15:04") + s.Subtle.Render(" ("+ageText(age)+")"))
	}
	if info.Tool() == snapshot.Timeshift && info.Backup.Mounted {
		add("   " + s.Subtle.Render("backup device "+info.Backup.Path+": "+pacman.FormatSize(info.Backup.Free)+" free"))
	}
	if info.ConfigErr != nil {
		add("   " + s.Subtle.Render("ℹ could not read "+summary.TimeshiftConfigPath+": "+info.ConfigErr.Error()))
	}
	if hook := info.Hook(); hook != "" {
		addWrapped(s.Info.Render("ℹ " + hook + " takes a snapshot on every pacman transaction, so cronen adds none before an upgrade (snapshot_before_upgrade = \"always\" overrides)"))
	}
	return sel
}

// ageText is "3 days ago" or "5 hours ago".
func ageText(d time.Duration) string {
	switch {
	case d < 0:
		return "in the future"
	case d >= 48*time.Hour:
		return itoa(int(d/(24*time.Hour))) + " days ago"
	case d >= time.Hour:
		return plural(int(d/time.Hour), "hour", "hours") + " ago"
	}
	return "just now"
}

// oneLineText keeps text from a tool on one line.
func oneLineText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return ' '
		}
		return r
	}, s)
}

// ---- housekeeping: create, delete, restore ----

type snapKind int

const (
	snapCreate snapKind = iota
	snapDelete
	snapRestore
)

// snapAction is the modal of a snapshot action started from housekeeping.
type snapAction struct {
	kind  snapKind
	tool  snapshot.Tool
	snaps []snapshot.Snapshot
	steps []snapshot.Step
	// copyCmd is the snapper rollback command shown for the user to copy.
	// It is never run.
	copyCmd string
	comment string
	// typed is what the user has typed towards "restore".
	typed string
}

// restoreWord is what the restore modal wants typed.
const restoreWord = "restore"

// snapSeq runs the steps of a snapAction one hand-off at a time, stopping
// at the first that fails.
type snapSeq struct {
	kind    snapKind
	tool    snapshot.Tool
	steps   []snapshot.Step
	idx     int
	comment string
	names   []string
}

type snapSeqMsg struct{ err error }

func (m *Model) openSnapAction(a *snapAction) {
	m.snapAct = a
	m.actionReturn = m.screen
	m.screen, m.focus = screenConfirm, focusList
}

func (m *Model) closeSnapAction() {
	m.snapAct = nil
	m.screen = m.actionReturn
}

// snapAvailable checks that an action can start, flashing why not.
func (m *Model) snapAvailable() bool {
	switch {
	case m.opts.Runner == nil || m.opts.Exec == nil && !m.opts.Demo:
		m.flash("snapshots aren't available here", true)
	case !m.snap.loaded || !m.snap.info.Found():
		m.flash("no snapshot tool found (Timeshift or snapper)", false)
	default:
		return true
	}
	return false
}

// startSnapshotNow is S: take a snapshot now.
func (m *Model) startSnapshotNow() {
	if !m.snapAvailable() {
		return
	}
	info := m.snap.info
	comment := "cronen: manual snapshot"
	argv, err := snapshot.CreateArgv(info.Tool(), m.cfg.PrivilegeArgv(), m.cfg.SnapperConfig, comment)
	if err == nil {
		err = m.snapGuard().Verify(argv)
	}
	if err != nil {
		m.flash(err.Error(), true)
		return
	}
	m.openSnapAction(&snapAction{kind: snapCreate, tool: info.Tool(), comment: comment,
		steps: []snapshot.Step{{Argv: argv, Label: "take a snapshot"}}})
}

// startSnapshotDelete is X on a snapshot row.
func (m *Model) startSnapshotDelete() {
	if !m.snapAvailable() {
		return
	}
	snaps := m.markedSnapshots()
	if len(snaps) == 0 {
		m.flash("no snapshots marked (space marks the one under the cursor)", false)
		return
	}
	info := m.snap.info
	a := &snapAction{kind: snapDelete, tool: info.Tool(), snaps: snaps}
	for _, sn := range snaps {
		argv, err := snapshot.DeleteArgv(info.Tool(), m.cfg.PrivilegeArgv(), m.cfg.SnapperConfig, sn.Name)
		if err == nil {
			err = m.snapGuard().Verify(argv)
		}
		if err != nil {
			m.flash(err.Error(), true)
			return
		}
		a.steps = append(a.steps, snapshot.Step{Argv: argv, Label: "delete " + sn.Name})
	}
	m.openSnapAction(a)
}

// startSnapshotRestore is R on a snapshot row.
func (m *Model) startSnapshotRestore() {
	if !m.snapAvailable() {
		return
	}
	rows := m.snapshotRows()
	idx := m.hk.sel - len(m.orphanNames())
	if idx < 0 || idx >= len(rows) {
		m.flash("R restores the snapshot under the cursor: move to the Snapshots list", false)
		return
	}
	sn, info := rows[idx], m.snap.info
	a := &snapAction{kind: snapRestore, tool: info.Tool(), snaps: []snapshot.Snapshot{sn}}
	if info.Tool() == snapshot.Snapper {
		cmd, err := snapshot.RollbackDisplay(m.cfg.PrivilegeArgv(), m.cfg.SnapperConfig, sn.Name)
		if err != nil {
			m.flash(err.Error(), true)
			return
		}
		a.copyCmd = cmd
	} else {
		argv, err := snapshot.RestoreArgv(info.Tool(), m.cfg.PrivilegeArgv(), sn.Name)
		if err == nil {
			err = m.snapGuard().Verify(argv)
		}
		if err != nil {
			m.flash(err.Error(), true)
			return
		}
		a.steps = []snapshot.Step{{Argv: argv, Label: "restore " + sn.Name}}
	}
	m.openSnapAction(a)
}

func (m Model) handleSnapActionKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	a := m.snapAct
	s := msg.String()
	switch s {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.closeSnapAction()
		return m, nil
	}
	switch {
	case a.kind == snapRestore && a.copyCmd != "":
		// snapper: a command to copy; nothing here runs it.
		return m, nil
	case a.kind == snapRestore:
		switch {
		case s == "enter":
			if a.typed == restoreWord {
				return m, m.runSnapAction()
			}
			m.flash("type "+restoreWord+" exactly to confirm", true)
		case s == "backspace":
			if a.typed != "" {
				a.typed = a.typed[:len(a.typed)-1]
			}
		case msg.Text != "":
			a.typed += msg.Text
		}
		return m, nil
	}
	if s == "y" {
		return m, m.runSnapAction()
	}
	return m, nil
}

// runSnapAction hands the terminal to the first step. Every step is
// verified by the Guard again, right before it runs.
func (m *Model) runSnapAction() tea.Cmd {
	a := m.snapAct
	if len(a.steps) == 0 {
		return nil
	}
	for _, st := range a.steps {
		if err := m.snapGuard().Verify(st.Argv); err != nil {
			m.flash(err.Error(), true)
			m.closeSnapAction()
			return nil
		}
	}
	if m.opts.Demo {
		m.flash("demo: nothing is run: "+strings.Join(a.steps[0].Argv, " "), false)
		m.closeSnapAction()
		return nil
	}
	if m.opts.Exec == nil {
		m.flash("snapshots aren't available here", true)
		m.closeSnapAction()
		return nil
	}
	seq := &snapSeq{kind: a.kind, tool: a.tool, steps: a.steps, comment: a.comment}
	for _, sn := range a.snaps {
		seq.names = append(seq.names, sn.Name)
	}
	m.snapSeq = seq
	m.closeSnapAction()
	return m.execSeqStep()
}

func (m *Model) execSeqStep() tea.Cmd {
	step := m.snapSeq.steps[m.snapSeq.idx]
	return m.opts.Exec(step.Argv, func(err error) tea.Msg { return snapSeqMsg{err: err} })
}

func (m *Model) handleSnapSeq(msg snapSeqMsg) tea.Cmd {
	s := m.snapSeq
	if s == nil {
		return nil
	}
	verb := map[snapKind]string{snapCreate: "snapshot", snapDelete: "delete", snapRestore: "restore"}[s.kind]
	if msg.err != nil {
		m.snapSeq = nil
		m.actionErr = true
		m.actionNotice = "✗ " + verb + " failed (" + msg.err.Error() + ")"
		if s.kind == snapDelete && len(s.steps) > 1 {
			m.actionNotice += " · stopped after " + itoa(s.idx) + " of " + itoa(len(s.steps)) + "; the rest was not run"
		}
		return tea.Batch(m.reloadSnapshots(), m.changed())
	}
	s.idx++
	if s.idx < len(s.steps) {
		return m.execSeqStep()
	}
	m.snapSeq = nil
	m.actionErr = false
	var save tea.Cmd
	switch s.kind {
	case snapCreate:
		m.actionNotice = "✓ " + s.tool.Title() + " snapshot taken"
		m.opts.State = m.opts.State.Clone()
		m.opts.State.AddSnapshot(summary.SnapshotRecord(s.tool, s.comment, m.now()))
		save = m.saveState()
	case snapDelete:
		m.actionNotice = "✓ deleted " + plural(len(s.names), "snapshot", "snapshots") + " (" + strings.Join(s.names, ", ") + ")"
		for _, n := range s.names {
			delete(m.snap.marked, n)
		}
	case snapRestore:
		m.actionNotice = "✓ " + s.tool.Title() + " restore finished · reboot to use the restored system"
	}
	return tea.Batch(save, m.reloadSnapshots(), m.changed())
}

// snapActionView is the modal of S, X and R in housekeeping.
func (m Model) snapActionView(w int) string {
	s, a := m.styles, m.snapAct
	var lines []string
	add := func(text string) { lines = append(lines, wrapWords(text, w, 4)) }
	cmd := func(argv []string) string { return s.Bold.Render(strings.Join(quoteFormat(argv), " ")) }
	switch a.kind {
	case snapCreate:
		add(" " + s.Bold.Render("Take a "+a.tool.Title()+" snapshot now?"))
		add("")
		add(" This runs: " + cmd(a.steps[0].Argv))
		add("")
		add(" Press " + s.Bold.Render("y") + " to run it, " + s.Bold.Render("esc") + " to cancel.")
	case snapDelete:
		add(" " + s.Bold.Render("Delete "+plural(len(a.snaps), "snapshot", "snapshots")+"?"))
		add("")
		for i, sn := range a.snaps {
			add("   " + sn.Name + " " + s.Subtle.Render(oneLineText(sn.Description)))
			add("     runs: " + cmd(a.steps[i].Argv))
		}
		add("")
		add(" " + s.Warn.Render("⚠ A deleted snapshot is gone for good."))
		if len(a.steps) > 1 {
			add(" " + s.Subtle.Render("One command per snapshot, run in turn; it stops at the first that fails."))
		}
		add("")
		add(" Press " + s.Bold.Render("y") + " to delete, " + s.Bold.Render("esc") + " to cancel.")
	case snapRestore:
		sn := a.snaps[0]
		add(" " + s.Critical.Render("Restore "+a.tool.Title()+" snapshot "+sn.Name+"?"))
		add("")
		if !sn.Date.IsZero() {
			add(" Taken: " + sn.Date.In(m.location()).Format("2006-01-02 15:04") + " " + s.Subtle.Render(oneLineText(sn.Description)))
		}
		add(" " + s.Critical.Render("⚠ The system will need a reboot afterwards."))
		add(" " + s.Critical.Render("⚠ Everything changed since this snapshot is lost: installed and upgraded packages, files, settings."))
		add("")
		if a.copyCmd != "" {
			add(" snapper's rollback depends on how your system boots, so cronen does not run it. Copy the command and read the snapper documentation first:")
			add("")
			add("   " + s.Bold.Render(a.copyCmd))
			add("")
			add(" " + s.Subtle.Render("esc closes this"))
			break
		}
		add(" This runs: " + cmd(a.steps[0].Argv))
		add(" " + s.Subtle.Render("Timeshift asks its own questions after that."))
		add("")
		add(" Type " + s.Bold.Render(restoreWord) + " and press enter to restore.")
		add("   > " + a.typed + "▏")
	}
	return strings.Join(lines, "\n")
}

func snapActionTitle(a *snapAction) string {
	switch a.kind {
	case snapDelete:
		return "Delete " + plural(len(a.snaps), "snapshot", "snapshots") + "?"
	case snapRestore:
		return "Restore a snapshot?"
	}
	return "Take a snapshot?"
}
