package ui

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/ops"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/system"
)

// pendingAction is the write action the confirm modal is about. Nothing
// runs until the user confirms; the dry run comes first (safety invariant
// 2), and a plan whose dry run failed offers no way to proceed.
type pendingAction struct {
	kind  ops.Kind
	names []string
	opts  ops.RemoveOptions
	// seq numbers the dry runs, so the answer of an older one (before a
	// toggle) is dropped.
	seq     int
	loading bool
	plan    ops.Plan
	// typed is what the user has typed towards plan.Token().
	typed string

	// The downgrade modal first lets the user pick a cached version
	// (candidates, pick), then dry-runs the chosen file (target). from is
	// the installed version and cacheDirs the directories -U files may
	// come from.
	picking    bool
	candidates []pacman.CachedPackage
	pick       int
	target     pacman.CachedPackage
	from       string
	cacheDirs  []string
	dateErr    error
}

type dryRunMsg struct {
	seq    int
	out    []byte
	stderr []byte
	err    error
	// backups and backupsErr are the lazy `pacman -Qii` read, only when the
	// dry run succeeded and -n is off.
	backups    map[string][]pacman.BackupFile
	backupsErr error
}

// actionDoneMsg reports that the handed-off command exited.
type actionDoneMsg struct {
	kind  ops.Kind
	names []string
	freed int64
	// version is what a downgrade installed.
	version string
	err     error
}

// dryRun runs the dry run as the user, through the runner.
// Without -n it then reads, read-only, the backup files of just the packages
// the dry run printed (plain `pacman -Qi` does not print them).
func dryRun(ctx context.Context, r system.Runner, seq int, argv []string, readBackups bool) tea.Cmd {
	return func() tea.Msg {
		out, _, err := r.Run(ctx, argv[0], argv[1:]...)
		msg := dryRunMsg{seq: seq, out: out, err: err}
		var exitErr *system.ExitError
		if errors.As(err, &exitErr) {
			msg.stderr = exitErr.Stderr
		}
		if err == nil && readBackups {
			msg.backups, msg.backupsErr = readBackupFiles(ctx, r, out)
		}
		return msg
	}
}

// readBackupFiles runs `pacman -Qii -- <printed names>`.
func readBackupFiles(ctx context.Context, r system.Runner, printed []byte) (map[string][]pacman.BackupFile, error) {
	list, err := ops.ParsePrint(printed)
	if err != nil || len(list) == 0 {
		return nil, nil // BuildRemovePlan reports these
	}
	names := make([]string, len(list))
	for i, pr := range list {
		names[i] = pr.Name
	}
	argv, err := ops.BackupArgv(names)
	if err != nil {
		return nil, err
	}
	out, _, err := r.Run(ctx, argv[0], argv[1:]...)
	if err != nil {
		return nil, err
	}
	return pacman.ParseBackupFiles(out)
}

// startRemove opens the removal modal for names and starts the dry run.
func (m *Model) startRemove(kind ops.Kind, names []string) tea.Cmd {
	if m.opts.Runner == nil {
		m.flash("removing isn't available here", true)
		return nil
	}
	if err := ops.CheckNames(names); err != nil {
		m.flash(err.Error(), true)
		return nil
	}
	m.act = &pendingAction{kind: kind, names: names, opts: ops.DefaultRemoveOptions()}
	m.actionReturn = m.screen
	m.screen, m.focus = screenConfirm, focusList
	return m.rerunDry()
}

// rerunDry (re)starts the dry run with the current options.
func (m *Model) rerunDry() tea.Cmd {
	a := m.act
	if a.kind == ops.KindDowngrade {
		return m.rerunDowngradeDry()
	}
	a.seq++
	a.loading, a.typed = true, ""
	argv, err := ops.RemoveDryArgv(a.opts, a.names)
	if err != nil {
		a.loading = false
		a.plan = ops.Plan{Kind: a.kind, Failure: err.Error()}
		return nil
	}
	return dryRun(m.ctx(), m.opts.Runner, a.seq, argv, !a.opts.NoSave)
}

func (m *Model) handleDryRun(msg dryRunMsg) {
	a := m.act
	if a == nil || msg.seq != a.seq {
		return
	}
	a.loading = false
	if a.kind == ops.KindDowngrade {
		m.buildDowngradePlan(msg)
		return
	}
	plan, err := ops.BuildRemovePlan(ops.RemoveInput{
		Kind: a.kind, Options: a.opts, Names: a.names,
		Priv: m.cfg.PrivilegeArgv(), Protected: m.cfg.EffectiveProtected(), DB: m.localDB,
		Out: msg.out, Stderr: msg.stderr, Failed: msg.err != nil, RunErr: msg.err,
		Backups: msg.backups, BackupsErr: msg.backupsErr,
	})
	if err != nil {
		plan = ops.Plan{Kind: a.kind, Options: a.opts, Failure: err.Error()}
	}
	a.plan = plan
}

// startPacdiff opens the modal for the pacdiff hand-off. It has no dry run.
func (m *Model) startPacdiff() {
	var files []string
	files = append(files, m.pacnew...)
	plan, err := ops.PacdiffPlan(m.cfg.PacdiffArgv(), files)
	if err != nil {
		m.flash(err.Error(), true)
		return
	}
	m.act = &pendingAction{kind: ops.KindPacdiff, plan: plan}
	m.actionReturn = m.screen
	m.screen, m.focus = screenConfirm, focusList
}

func (m *Model) closeAction() {
	m.act = nil
	m.screen = m.actionReturn
}

// handleActionKey handles the keys of the action modal.
func (m Model) handleActionKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	a := m.act
	s := msg.String()
	switch s {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.closeAction()
		return m, nil
	}
	if a.kind == ops.KindDowngrade {
		return m.handleDowngradeKey(msg)
	}
	if a.kind == ops.KindPacdiff {
		if s == "y" {
			return m, m.runAction()
		}
		return m, nil
	}

	// s and n toggle -s and -n. While the package name is being typed they
	// are letters, so the toggles are ctrl+s and ctrl+n then.
	typing := a.plan.Token() != ""
	switch {
	case s == "ctrl+s" || (!typing && s == "s"):
		a.opts.Recursive = !a.opts.Recursive
		return m, m.rerunDry()
	case s == "ctrl+n" || (!typing && s == "n"):
		a.opts.NoSave = !a.opts.NoSave
		return m, m.rerunDry()
	}
	if a.loading || a.plan.Blocked() {
		return m, nil
	}
	if !typing {
		if s == "y" {
			return m, m.runAction()
		}
		return m, nil
	}
	switch {
	case s == "enter":
		if a.typed == a.plan.Token() {
			return m, m.runAction()
		}
		m.flash("type "+a.plan.Token()+" exactly to confirm", true)
	case s == "backspace":
		if a.typed != "" {
			a.typed = a.typed[:len(a.typed)-1]
		}
	case msg.Text != "":
		a.typed += msg.Text
	}
	return m, nil
}

// runAction hands the terminal to the planned command.
func (m *Model) runAction() tea.Cmd {
	a := m.act
	plan := a.plan
	if plan.Blocked() || a.loading || len(plan.Command) == 0 {
		return nil
	}
	if m.opts.Demo {
		m.flash("demo: nothing is run: "+strings.Join(plan.Command, " "), false)
		m.closeAction()
		return nil
	}
	if m.opts.Exec == nil {
		m.flash("removing isn't available here", true)
		m.closeAction()
		return nil
	}
	done := actionDoneMsg{kind: plan.Kind, names: plan.Selected, freed: plan.Freed}
	if a.kind == ops.KindDowngrade {
		if err := m.recheckDowngrade(); err != nil {
			m.flash(err.Error(), true)
			m.closeAction()
			return nil
		}
		done.version = a.target.Version
	}
	m.closeAction()
	return m.opts.Exec(plan.Command, func(err error) tea.Msg {
		done.err = err
		return done
	})
}

// handleActionDone shows what the action did and reloads everything.
func (m *Model) handleActionDone(msg actionDoneMsg) tea.Cmd {
	m.actionErr = msg.err != nil
	switch {
	case msg.err != nil:
		m.actionNotice = "✗ " + actionVerb(msg.kind) + " failed (" + msg.err.Error() + ")"
	case msg.kind == ops.KindPacdiff:
		m.actionNotice = "✓ pacdiff finished"
	case msg.kind == ops.KindDowngrade:
		m.actionNotice = "✓ downgraded " + strings.Join(msg.names, ", ") + " to " + msg.version +
			" · the next -Syu upgrades it again unless it's in IgnorePkg"
	default:
		m.actionNotice = "✓ removed " + plural(len(msg.names), "package", "packages") + " (" + strings.Join(msg.names, ", ") + ")"
		if msg.freed > 0 {
			m.actionNotice += " · freed " + pacman.FormatSize(msg.freed)
		}
		for _, n := range msg.names {
			delete(m.pkgs.marked, n)
			delete(m.hk.unmarked, n)
		}
	}
	return m.refresh()
}

func actionVerb(k ops.Kind) string {
	switch k {
	case ops.KindPacdiff:
		return "pacdiff"
	case ops.KindDowngrade:
		return "downgrade"
	}
	return "removal"
}

// actionTitle is the question at the top of the modal.
func actionTitle(a *pendingAction) string {
	switch a.kind {
	case ops.KindOrphans:
		return "Remove " + plural(len(a.names), "orphan", "orphans") + "?"
	case ops.KindPacdiff:
		return "Merge .pacnew and .pacsave files with pacdiff?"
	case ops.KindDowngrade:
		return "Downgrade " + strings.Join(a.names, ", ") + " from the package cache?"
	}
	return "Remove " + plural(len(a.names), "package", "packages") + "?"
}
