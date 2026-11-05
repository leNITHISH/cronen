package ui

import (
	"errors"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/leNITHISH/cronen/internal/archive"
	"github.com/leNITHISH/cronen/internal/bandwidth"
	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/ops"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/summary"
)

// The install helper ("I need X now"). It offers two ways to get a package
// on a system that is behind, with their costs side by side, and never
// `pacman -Sy X` or `pacman -S X` against the real database:
//
//	A  the full upgrade plus X (recommended, the default)
//	B  X from the Arch Linux Archive as of your last full upgrade
//	   (experimental, archive_install = true)
//
// Both run only after a dry run as the user succeeded, and B only after a
// last look at the database lock and the generated config.

type installStage int

const (
	instInput installStage = iota
	instResolving
	instReady
	instFailed
)

type archiveStage int

const (
	archIdle archiveStage = iota
	archPreparing
	archReady
	archFailed
)

// wikiPartial is the page that explains partial upgrades.
const wikiPartial = "https://wiki.archlinux.org/title/System_maintenance#Partial_upgrades_are_unsupported"

type installFlow struct {
	stage installStage
	input textinput.Model
	// seq numbers the resolve and prepare runs, so a stale answer is
	// dropped.
	seq   int
	names []string
	err   error
	res   summary.Resolution

	// Option A.
	a ops.InstallPlan
	// Option B: why it is unavailable (nil when it can be prepared), the
	// preparation, and its plan.
	bAvail error
	bStage archiveStage
	b      archive.Prepared
	bPlan  ops.InstallPlan
	bErr   error

	lastUp time.Time
	realDB string
	sel    ops.InstallOption
	scroll int
}

type installResolvedMsg struct {
	seq    int
	res    summary.Resolution
	a      ops.InstallPlan
	err    error
	lastUp time.Time
	realDB string
	bAvail error
}

type installPreparedMsg struct {
	seq  int
	p    archive.Prepared
	plan ops.InstallPlan
	err  error
}

// installDoneMsg reports that option B's hand-off exited.
type installDoneMsg struct {
	names  []string
	day    string
	differ int
	err    error
}

// archiveBase is where archive directories go, "" if unknown.
func (m Model) archiveBase() string { return m.opts.ArchiveBase }

func (m Model) installGuard(realDB string) ops.Guard {
	if realDB == "" {
		realDB = pacman.DefaultDBPath
	}
	return ops.Guard{RealDB: realDB, ArchiveBase: m.archiveBase()}
}

func (m Model) newInstallInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Placeholder = "package names, separated by spaces"
	st := textinput.DefaultStyles(m.isDark)
	st.Cursor.Blink = false
	ti.SetStyles(st)
	return ti
}

// openInstall opens the helper's name prompt (the I key).
func (m *Model) openInstall() tea.Cmd {
	if m.opts.Runner == nil || m.opts.FS == nil {
		m.flash("the install helper isn't available here", true)
		return nil
	}
	f := &installFlow{stage: instInput, input: m.newInstallInput()}
	m.inst = f
	m.screen, m.focus = screenInstall, focusList
	return f.input.Focus()
}

// startInstall resolves names (cronen install <pkg>..., or the prompt).
func (m *Model) startInstall(names []string) tea.Cmd {
	f := m.inst
	if f == nil {
		f = &installFlow{input: m.newInstallInput()}
		m.inst = f
	}
	m.screen, m.focus = screenInstall, focusList
	f.stage, f.err, f.names = instResolving, nil, names
	f.seq++
	if err := ops.CheckInstallNames(names); err != nil {
		f.stage, f.err = instFailed, err
		return nil
	}
	return tea.Batch(m.resolveInstallCmd(f.seq, names), m.spinner.Tick)
}

func (m Model) resolveInstallCmd(seq int, names []string) tea.Cmd {
	ctx, r, fetcher, fsys, cfg := m.ctx(), m.opts.Runner, m.opts.Fetcher, m.opts.FS, m.cfg
	db, loc, base := m.opts.SyncDB, m.location(), m.archiveBase()
	return func() tea.Msg {
		msg := installResolvedMsg{seq: seq}
		conf, _ := fsys.ReadFile(pacman.ConfPath)
		msg.realDB = pacman.DBPath(conf)
		res, err := summary.ResolveInstall(ctx, r, fetcher, fsys, cfg, db, names)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.res = res
		g := ops.Guard{RealDB: msg.realDB, ArchiveBase: base}
		if msg.a, msg.err = summary.PlanUpgradeInstall(ctx, r, cfg, g, db, res); msg.err != nil {
			return msg
		}
		// Option B reads the last full upgrade from pacman.log only.
		msg.lastUp, _ = pacman.ReadLastFullUpgrade(fsys, pacman.LogPath, loc)
		switch {
		case len(res.AUR) > 0:
			msg.bAvail = errors.New("repository packages only: " + strings.Join(res.AUR, ", ") + " is AUR")
		case base == "" || m.opts.Nonce == nil:
			msg.bAvail = errors.New("no cache directory to build the archive directory in")
		default:
			msg.bAvail = archive.Check(ctx, r, cfg.ArchiveInstall, msg.lastUp)
		}
		return msg
	}
}

func (m *Model) handleInstallResolved(msg installResolvedMsg) {
	f := m.inst
	if f == nil || msg.seq != f.seq {
		return
	}
	if msg.err != nil {
		f.stage, f.err = instFailed, msg.err
		return
	}
	f.stage, f.res, f.a = instReady, msg.res, msg.a
	f.lastUp, f.realDB, f.bAvail = msg.lastUp, msg.realDB, msg.bAvail
	f.sel = ops.OptionUpgrade
}

// prepareArchive runs option B's preparation: the archive directories, the
// sync and the dry run. It is only started by the user (b), because it
// downloads the archive's repository databases.
func (m *Model) prepareArchive() tea.Cmd {
	f := m.inst
	f.bStage, f.bErr = archPreparing, nil
	f.seq++
	seq := f.seq
	ctx, r, fsys, cfg := m.ctx(), m.opts.Runner, m.opts.FS, m.cfg
	in := archive.Input{
		Base: m.archiveBase(), RealDB: f.realDB, LastUpgrade: f.lastUp, Now: m.now(),
		Nonce: m.opts.Nonce(), Names: f.res.Repo,
	}
	priv := cfg.PrivilegeArgv()
	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		conf, err := fsys.ReadFile(pacman.ConfPath)
		if err != nil {
			return installPreparedMsg{seq: seq, err: err}
		}
		in.RealConf = conf
		in.RealDB = pacman.DBPath(conf)
		p, err := archive.Prepare(ctx, r, fsys, in)
		if err != nil {
			return installPreparedMsg{seq: seq, p: p, err: err}
		}
		plan := ops.InstallPlanInput{
			Option: ops.OptionArchive, Names: in.Names, DryRun: p.DryRun, Guard: p.Guard(),
			Out: p.DryOut, Stderr: p.DryStderr, Failed: p.DryFailed, RunErr: p.DryRunErr,
		}
		if plan.Command, err = p.InstallArgv(priv); err != nil {
			return installPreparedMsg{seq: seq, p: p, err: err}
		}
		return installPreparedMsg{seq: seq, p: p, plan: ops.BuildInstallPlan(plan)}
	})
}

func (m *Model) handleInstallPrepared(msg installPreparedMsg) {
	f := m.inst
	if f == nil || msg.seq != f.seq {
		return
	}
	if msg.err != nil {
		f.bStage, f.bErr = archFailed, msg.err
		return
	}
	f.bStage, f.b, f.bPlan = archReady, msg.p, msg.plan
}

func (m *Model) closeInstall() {
	m.inst = nil
	m.screen = screenUpdates
}

// handleInstallKey handles every key while the helper is open.
func (m Model) handleInstallKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	f := m.inst
	s := msg.String()
	if s == "ctrl+c" {
		return m, tea.Quit
	}
	if s == "esc" {
		m.closeInstall()
		return m, nil
	}
	switch f.stage {
	case instInput:
		if s == "enter" {
			names := strings.Fields(f.input.Value())
			if len(names) == 0 {
				m.flash("type the package names first", false)
				return m, nil
			}
			return m, m.startInstall(names)
		}
		var cmd tea.Cmd
		f.input, cmd = f.input.Update(msg)
		return m, cmd
	case instReady:
	default:
		return m, nil
	}
	switch {
	case s == "a":
		f.sel = ops.OptionUpgrade
	case s == "b":
		return m, m.selectArchive()
	case s == "y":
		return m, m.runInstall()
	case key.Matches(msg, m.keys.Down):
		f.scroll++
	case key.Matches(msg, m.keys.Up):
		f.scroll = max(0, f.scroll-1)
	case key.Matches(msg, m.keys.Top):
		f.scroll = 0
	case key.Matches(msg, m.keys.Bottom):
		f.scroll = 1 << 20
	case key.Matches(msg, m.keys.HalfDown):
		f.scroll += max(1, m.bodyHeight()/2)
	case key.Matches(msg, m.keys.HalfUp):
		f.scroll = max(0, f.scroll-max(1, m.bodyHeight()/2))
	}
	return m, nil
}

// selectArchive selects option B, preparing it first if that hasn't been
// done. Unavailable means a flash with the reason.
func (m *Model) selectArchive() tea.Cmd {
	f := m.inst
	if f.bAvail != nil {
		m.flash("option B unavailable: "+f.bAvail.Error(), true)
		return nil
	}
	f.sel = ops.OptionArchive
	if f.bStage == archIdle || f.bStage == archFailed {
		return m.prepareArchive()
	}
	return nil
}

// runInstall hands the terminal to the selected option's command. A plan
// whose dry run failed, or that is still being prepared, runs nothing.
func (m *Model) runInstall() tea.Cmd {
	f := m.inst
	plan, g := f.a, m.installGuard(f.realDB)
	if f.sel == ops.OptionArchive {
		if f.bStage != archReady {
			m.flash("option B isn't ready: press b to prepare it", true)
			return nil
		}
		plan, g = f.bPlan, f.b.Guard()
	}
	if plan.Blocked() || len(plan.Command) == 0 {
		m.flash("the dry run failed; nothing will be run", true)
		return nil
	}
	if err := g.Verify(plan.Command); err != nil {
		m.flash(err.Error(), true)
		return nil
	}
	if m.opts.Demo {
		m.flash("demo: nothing is run: "+strings.Join(plan.Command, " "), false)
		m.closeInstall()
		return nil
	}
	if m.opts.Exec == nil {
		m.flash("installing isn't available here", true)
		return nil
	}
	if f.sel == ops.OptionArchive {
		// The lock lives in the temporary directory for this option, so
		// look at the real one now, and at the directory root will read.
		if err := archive.Recheck(m.opts.FS, f.b); err != nil {
			m.flash(err.Error(), true)
			return nil
		}
		done := installDoneMsg{names: f.res.Repo, day: f.b.Day, differ: len(f.b.Candidate.Differ)}
		m.closeInstall()
		return m.opts.Exec(plan.Command, func(err error) tea.Msg {
			done.err = err
			return done
		})
	}
	// Option A is a full upgrade: it is recorded like one.
	m.closeInstall()
	m.upgrade = upgradeOutcome{reboot: m.result.RebootRequired}
	m.timing = handoffTiming{}
	return m.opts.Exec(plan.Command, func(err error) tea.Msg { return upgradedMsg{err: err} })
}

// handleInstallDone records an archive install and reloads everything.
func (m *Model) handleInstallDone(msg installDoneMsg) tea.Cmd {
	names := strings.Join(msg.names, ", ")
	m.actionErr = msg.err != nil
	if msg.err != nil {
		m.actionNotice = "✗ installing " + names + " from the archive failed (" + msg.err.Error() + ")"
		return m.refresh()
	}
	m.actionNotice = "✓ " + names + " installed from the archive (" + msg.day + ") · the next -Syu upgrades it normally"
	st := m.opts.State.Clone()
	st.AddArchiveInstall(config.ArchiveInstall{Packages: msg.names, Day: msg.day, Differ: msg.differ, At: m.now()})
	m.opts.State = st
	return tea.Batch(m.saveState(), m.refresh())
}

// installTitle is the status bar's left side.
func (m Model) installTitle() string {
	if m.inst != nil && len(m.inst.names) > 0 {
		return " Install " + strings.Join(m.inst.names, ", ")
	}
	return " Install a package now"
}

// installHints are the keys of the helper's current stage.
func (m Model) installHints() string {
	switch {
	case m.inst == nil:
		return "esc cancel"
	case m.inst.stage == instInput:
		return "enter look up  esc cancel"
	case m.inst.stage == instReady:
		return "a/b option  y run  j/k scroll  esc cancel"
	}
	return "esc cancel"
}

// wrapWords wraps text at spaces only, indenting continuation lines. Unlike
// wrapIndent it never breaks inside a hyphenated word, so a command line
// ("--config", "-Syu") stays readable as the exact argv. A word wider than
// the screen is cut as a last resort.
func wrapWords(text string, w, indent int) string {
	if w-indent < 8 {
		return text
	}
	limit := w - indent
	lines := strings.Split(ansi.Hardwrap(ansi.Wordwrap(text, limit, ""), limit, true), "\n")
	pad := strings.Repeat(" ", indent)
	for i := 1; i < len(lines); i++ {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

// installView is the helper's screen.
func (m Model) installView(w int) string {
	s, f := m.styles, m.inst
	var lines []string
	add := func(text string) { lines = append(lines, wrapWords(text, w, 6)) }
	switch f.stage {
	case instInput:
		add(" " + s.Bold.Render("Install a package now"))
		add("")
		add(" Type the package names. cronen looks them up, then offers the safe ways to get them.")
		add(" It never runs " + s.Bold.Render("pacman -Sy X") + " (a partial upgrade): see " + wikiPartial)
		add("")
		add(" " + f.input.View())
		return strings.Join(lines, "\n")
	case instResolving:
		add(" " + s.Bold.Render("Install "+strings.Join(f.names, ", ")))
		add("")
		add(" " + s.Subtle.Render(m.spinner.View()+" looking up the packages and running pacman's dry run…"))
		return strings.Join(lines, "\n")
	case instFailed:
		add(" " + s.Bold.Render("Install "+strings.Join(f.names, ", ")))
		add("")
		add(" " + s.Error.Render("✗ "+f.err.Error()))
		add("")
		add(" Nothing was run. " + s.Subtle.Render("esc closes this."))
		return strings.Join(lines, "\n")
	}

	names := strings.Join(f.res.All(), ", ")
	add(" " + s.Bold.Render("Install "+names))
	add("")
	add(" " + s.Warn.Render("Why not `pacman -Sy "+f.names[0]+"`?") + " It syncs the database and installs one package on a system")
	add(" that isn't fully upgraded: a partial upgrade. The package is built against libraries newer")
	add(" than yours, and things break later. cronen never does that. " + s.Subtle.Render(wikiPartial))
	add("")
	m.optionAView(add)
	add("")
	m.optionBView(add)
	add("")
	switch {
	case f.sel == ops.OptionArchive && f.bStage == archReady && !f.bPlan.Blocked():
		add(" Press " + s.Bold.Render("y") + " to run option B, " + s.Bold.Render("a") + " for option A, " + s.Bold.Render("esc") + " to cancel and do nothing.")
	case f.sel == ops.OptionUpgrade && !f.a.Blocked():
		add(" Press " + s.Bold.Render("y") + " to run option A, " + s.Bold.Render("b") + " for option B, " + s.Bold.Render("esc") + " to cancel and do nothing.")
	default:
		add(" Nothing can be run from the selected option. " + s.Subtle.Render("esc cancels."))
	}
	if m.opts.Demo {
		add(" " + s.Subtle.Render("demo: nothing is run"))
	}

	// Scrolling: the screen is longer than most terminals.
	h := m.bodyHeight()
	all := strings.Split(strings.Join(lines, "\n"), "\n")
	f.scroll = clamp(f.scroll, 0, max(0, len(all)-h))
	return strings.Join(all[f.scroll:], "\n")
}

func (m Model) marker(o ops.InstallOption) string {
	if m.inst.sel == o {
		return "▶"
	}
	return " "
}

func (m Model) optionAView(add func(string)) {
	s, f := m.styles, m.inst
	p := f.a
	add(" " + m.marker(ops.OptionUpgrade) + " " + s.Bold.Render("A  Full upgrade, then install "+strings.Join(f.res.All(), ", ")) + " " + s.Info.Render("recommended"))
	add("      Dry run: " + s.Subtle.Render(strings.Join(quoteFormat(p.DryRun), " ")))
	if p.Blocked() {
		add("      " + s.Error.Render("✗ the dry run failed; this option can't run: "+p.Failure))
		return
	}
	add("      This runs: " + s.Bold.Render(strings.Join(p.Command, " ")))
	add("      Download: " + pacman.FormatSize(p.Download) + " (" + plural(p.Packages, "package", "packages") + ") · estimated time: " +
		bandwidth.EstimateTime(m.opts.State.PrefetchSamples(), m.opts.State.UpgradeSamples(), p.Download, p.Packages).Text())
	if len(f.res.AUR) > 0 {
		add("      " + s.Warn.Render("⚠ "+strings.Join(f.res.AUR, ", ")+" comes from the AUR: "+m.cfg.AURHelper+" builds it, and cronen does not preview that. Read its PKGBUILD."))
	}
	m.itemList(add, p)
	for _, wn := range p.Warnings {
		add("      " + s.Warn.Render("⚠ "+wn))
	}
}

func (m Model) itemList(add func(string), p ops.InstallPlan) {
	s := m.styles
	var asked, other []ops.InstallItem
	for _, it := range p.Items {
		if it.Requested {
			asked = append(asked, it)
		} else {
			other = append(other, it)
		}
	}
	for _, it := range asked {
		add("      " + s.Bold.Render(it.Name) + " " + s.Subtle.Render(it.Version+" · "+it.Repo+" · "+pacman.FormatSize(it.Size)))
	}
	if len(other) > 0 {
		var label string
		if p.Option == ops.OptionUpgrade {
			label = "the rest of the upgrade"
		} else {
			label = "pulled in as new dependencies"
		}
		add("      " + s.Subtle.Render(plural(len(other), "package", "packages")+" · "+label))
		for i, it := range other {
			if i == maxListed {
				add("        " + s.Subtle.Render("… and "+itoa(len(other)-maxListed)+" more"))
				break
			}
			add("        " + it.Name + " " + s.Subtle.Render(it.Version))
		}
	}
}

func (m Model) optionBView(add func(string)) {
	s, f := m.styles, m.inst
	head := " " + m.marker(ops.OptionArchive) + " " + s.Bold.Render("B  "+strings.Join(f.names, ", ")+" as of your last full upgrade") + " " + s.Subtle.Render("experimental")
	add(head)
	if f.bAvail != nil {
		msg := "unavailable: " + f.bAvail.Error()
		if errors.Is(f.bAvail, archive.ErrOff) {
			msg = "off: " + f.bAvail.Error()
		}
		add("      " + s.Subtle.Render(msg))
		return
	}
	add("      " + s.Subtle.Render("Installs it from the Arch Linux Archive snapshot of the day of your last full upgrade ("+f.lastUp.UTC().Format(archive.DayFormat)+") or the next, so you download only it and its new dependencies."))
	switch f.bStage {
	case archIdle:
		add("      Press " + s.Bold.Render("b") + " to prepare it. That downloads the archive's repository databases for two days, as you (through fakeroot), into a new 0700 directory under " + m.archiveBase() + ". Nothing is installed.")
		return
	case archPreparing:
		add("      " + s.Subtle.Render(m.spinner.View()+" syncing the archive and running pacman's dry run… (the archive can be slow)"))
		return
	case archFailed:
		add("      " + s.Error.Render("✗ "+f.bErr.Error()))
		add("      " + s.Subtle.Render("press b to try again"))
		return
	}
	p, b := f.bPlan, f.b
	add("      " + s.Severity(confidenceSeverity(len(b.Candidate.Differ))).Render(b.Candidate.Confidence()))
	for i, l := range b.Candidate.DifferList() {
		if i == maxListed {
			add("        " + s.Subtle.Render("… and "+itoa(len(b.Candidate.Differ)-maxListed)+" more"))
			break
		}
		add("        " + s.Subtle.Render(l))
	}
	add("      Dry run: " + s.Subtle.Render(strings.Join(quoteFormat(p.DryRun), " ")))
	if p.Blocked() {
		add("      " + s.Error.Render("✗ the dry run failed; this option can't run: "+p.Failure))
	} else {
		add("      This runs: " + s.Bold.Render(strings.Join(p.Command, " ")))
		add("      Download: " + pacman.FormatSize(p.Download) + " (" + plural(p.Packages, "package", "packages") + ") · estimated time: " +
			bandwidth.EstimateTime(m.opts.State.PrefetchSamples(), m.opts.State.UpgradeSamples(), p.Download, p.Packages).Text())
		m.itemList(add, p)
		for _, wn := range p.Warnings {
			add("      " + s.Warn.Render("⚠ "+wn))
		}
	}
	rep := b.Report
	add("      " + s.Subtle.Render("The temporary pacman.conf keeps your [options] and enables only "+strings.Join(rep.Repos, ", ")+", each with just the archive Server."))
	if len(rep.Dropped) > 0 {
		add("      " + s.Warn.Render("⚠ Custom repositories are dropped: "+strings.Join(rep.Dropped, ", ")+" (so a package from one can't be installed this way)."))
	}
	if rep.Includes > 0 || len(rep.Options) > 0 {
		add("      " + s.Subtle.Render("Left out: "+plural(rep.Includes, "Include line", "Include lines")+optionsLeftOut(rep.Options)+"."))
	}
	add("      " + s.Subtle.Render("The archive is slow. The next -Syu upgrades it normally. Root will read the config from "+b.Dir+", a directory cronen just created (new for every install, 0700, written fresh); cronen compares it with what it wrote, and checks that no db.lck exists, right before the hand-off."))
}

func optionsLeftOut(opts []string) string {
	if len(opts) == 0 {
		return ""
	}
	return ", " + strings.Join(opts, ", ")
}

func confidenceSeverity(differ int) (sev risk.Severity) {
	if differ == 0 {
		return risk.Info
	}
	return risk.Warn
}
