package ui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/linkage"
	"github.com/leNITHISH/cronen/internal/ops"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/partial"
	"github.com/leNITHISH/cronen/internal/summary"
	"github.com/leNITHISH/cronen/internal/system"
)

// partialState is the partial-upgrade detector's share of the model: the
// real-database check and the history marks (info), and the broken-linkage
// scan (run while it works, scan when it finished this session).
type partialState struct {
	sec  section
	info summary.PartialInfo
	// loaded reports that info holds a result.
	loaded bool

	// run is the scan in progress, nil when none.
	run *linkageRun
	// scan is the result of a scan finished in this session.
	scan    *linkage.Result
	scanErr error
}

// linkageRun is a scan in progress. Its counters are written by the scan's
// goroutines and read by the UI's ticks.
type linkageRun struct {
	ctx         context.Context
	cancel      context.CancelFunc
	done, total atomic.Int64
}

type partialMsg struct {
	gen  int
	info summary.PartialInfo
	err  error
}

// linkageMsg reports a finished (or cancelled) scan.
type linkageMsg struct {
	run *linkageRun
	res linkage.Result
	err error
}

// linkageTickMsg refreshes the progress shown while a scan runs.
type linkageTickMsg struct{ run *linkageRun }

const linkageTick = 250 * time.Millisecond

func loadPartial(ctx context.Context, r system.Runner, fsys system.FS, cacheDir string, loc *time.Location, gen int) tea.Cmd {
	return func() tea.Msg {
		info, err := summary.LoadPartial(ctx, summary.PartialSources{Runner: r, FS: fsys, CacheDir: cacheDir, Location: loc})
		return partialMsg{gen: gen, info: info, err: err}
	}
}

// handlePartial stores the detector's result. A scan finished in this
// session stays valid when the cache couldn't be read back (no cache
// directory), as the installed packages are the same until the next reload
// shows otherwise.
func (m *Model) handlePartial(msg partialMsg) tea.Cmd {
	if msg.gen != m.gen {
		return nil
	}
	p := &m.part
	p.sec = section{err: msg.err}
	p.info, p.loaded = msg.info, msg.err == nil
	if p.loaded && !p.info.LinkageKnown && p.scan != nil && m.opts.CacheDir == "" {
		p.info.Linkage, p.info.LinkageKnown = linkage.Saved{Broken: p.scan.Broken}, true
	}
	return m.changed()
}

// scanning reports a linkage scan in progress.
func (m Model) scanning() bool { return m.part.run != nil }

// startScan begins the broken-linkage scan (B on the housekeeping screen).
func (m *Model) startScan() tea.Cmd {
	if m.scanning() {
		m.flash("a scan is already running (esc cancels it)", false)
		return nil
	}
	if m.opts.Opener == nil || m.opts.Runner == nil {
		m.flash("the linkage scan isn't available here", true)
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx())
	run := &linkageRun{ctx: ctx, cancel: cancel}
	m.part.run, m.part.scanErr = run, nil
	src := summary.LinkageSources{
		Runner: m.opts.Runner, Opener: m.opts.Opener, FS: m.opts.FS, CacheDir: m.opts.CacheDir,
		Skip: m.cfg.LinkageSkip, Fresh: m.freshLoad, Now: m.opts.Now,
		OnProgress: func(p linkage.Progress) {
			run.total.Store(int64(p.Total))
			run.done.Store(int64(p.Done))
		},
	}
	scan := func() tea.Msg {
		res, err := summary.ScanLinkage(ctx, src)
		cancel()
		return linkageMsg{run: run, res: res, err: err}
	}
	return tea.Batch(scan, tea.Tick(linkageTick, func(time.Time) tea.Msg { return linkageTickMsg{run: run} }))
}

// cancelScan stops a running scan.
func (m *Model) cancelScan() {
	if m.part.run != nil {
		m.part.run.cancel()
	}
}

func (m *Model) handleLinkage(msg linkageMsg) tea.Cmd {
	p := &m.part
	if p.run != msg.run {
		return nil
	}
	p.run = nil
	switch {
	case errors.Is(msg.err, context.Canceled):
		m.flash("linkage scan cancelled", false)
		return nil
	case msg.err != nil:
		p.scanErr = msg.err
		return nil
	}
	res := msg.res
	p.scan = &res
	p.info.Linkage, p.info.LinkageKnown = linkage.Saved{Broken: res.Broken}, true
	return m.changed()
}

func (m *Model) handleLinkageTick(msg linkageTickMsg) tea.Cmd {
	if m.part.run != msg.run || msg.run == nil {
		return nil
	}
	return tea.Tick(linkageTick, func(time.Time) tea.Msg { return linkageTickMsg{run: msg.run} })
}

// partialSince are the partial upgrades since the last full upgrade.
func (m Model) partialSince() []partial.Transaction {
	if !m.cfg.PartialCheck || !m.part.loaded {
		return nil
	}
	return m.part.info.PartialSince(m.lastFullUpgrade())
}

// partialLine is the line under the header: "2 partial upgrades since your
// last full upgrade (Oct 3: pacman -Sy discord)".
func (m Model) partialLine(w int) string {
	line := summary.PartialLine(m.partialSince(), m.location())
	if line == "" {
		return ""
	}
	return fitLine(" "+m.styles.Warn.Render("~ "+line), w)
}

// partialPackages are the packages the partial upgrades since the last full
// upgrade installed or upgraded: they get the ~ badge until the next full
// upgrade.
func (m Model) partialPackages() map[string]bool {
	return partial.Packages(m.partialSince())
}

// partialTx reports whether a logged transaction is a partial upgrade.
func partialTx(tx pacman.LogTransaction) bool {
	_, ok := partial.Check(tx)
	return ok
}

// fixes works out the suggested fixes for the broken files of the last scan.
func (m Model) fixes() []linkage.Fix {
	if !m.part.info.LinkageKnown || len(m.part.info.Linkage.Broken) == 0 {
		return nil
	}
	in := linkage.FixInput{
		Broken:      m.part.info.Linkage.Broken,
		PendingRepo: len(m.repo),
		// checkupdates failed (or hasn't answered): nobody knows, so no rebuild.
		PendingUnknown: m.repoSec.err != nil || m.repoSec.loading,
		Priv:           m.cfg.PrivilegeArgv(),
		AURHelper:      m.cfg.AURHelper,
		Foreign:        map[string]bool{},
		Cached:         map[string]string{},
	}
	if m.localDB != nil {
		for n := range m.localDB.Foreign {
			in.Foreign[n] = true
		}
	}
	in.CacheDirs = pacman.ResolveCacheDirs(m.opts.FS, m.cfg.ExtraCacheDirs, m.opts.Home)
	for _, b := range in.Broken {
		if in.Foreign[b.Package] {
			continue
		}
		for _, c := range m.hist.cache[b.Package] {
			if pacman.SameVersion(c.Version, b.Version) {
				in.Cached[b.Package] = c.Path
			}
		}
	}
	return linkage.Suggest(in)
}

// startRebuild opens the confirm modal for the rebuild fix (b on the
// housekeeping screen). It is only available when Suggest offered it, which
// it does only with no repo updates pending.
func (m *Model) startRebuild() {
	for _, f := range m.fixes() {
		if f.Kind != linkage.FixRebuild || f.Argv == nil {
			continue
		}
		plan, err := ops.RebuildPlan(m.cfg.AURHelper, f.Packages)
		if err != nil {
			m.flash(err.Error(), true)
			return
		}
		m.act = &pendingAction{kind: ops.KindRebuild, names: f.Packages, plan: plan}
		m.actionReturn = m.screen
		m.screen, m.focus = screenConfirm, focusList
		return
	}
	for _, f := range m.fixes() {
		if f.Kind == linkage.FixUpgradeFirst {
			m.flash("upgrade first, then rebuild: repo updates are pending", false)
			return
		}
	}
	m.flash("nothing to rebuild (or no aur_helper set)", false)
}

// linkageLines renders the "Partial upgrade" section of the housekeeping
// screen.
func (m Model) linkageLines(add func(string), title func(string)) {
	s := m.styles
	title("Partial upgrade")
	switch {
	case !m.cfg.PartialCheck:
		add("   " + s.Subtle.Render("partial-upgrade checks are off (partial_check = false)"))
	case m.part.sec.loading:
		add("   " + s.Subtle.Render(m.spinner.View()+" checking the package database…"))
	case m.part.sec.err != nil:
		add("   " + s.Error.Render("✗ "+m.part.sec.err.Error()))
	default:
		info := m.part.info
		if info.DBSynced() {
			add("   " + s.Warn.Render("⚠ the package database was synced without upgrading: "+plural(len(info.Pending), "package has", "packages have")+" a newer version waiting"))
		} else {
			add("   ✓ the package database matches what is installed")
		}
		if txs := m.partialSince(); len(txs) > 0 {
			add("   " + s.Warn.Render("~ "+summary.PartialLine(txs, m.location())))
		}
	}

	add("")
	switch run := m.part.run; {
	case run != nil:
		done, total := run.done.Load(), run.total.Load()
		prog := "listing package files…"
		if total > 0 {
			prog = "scanning packages: " + itoa(int(done)) + "/" + itoa(int(total))
		}
		add("   " + s.Subtle.Render(m.spinner.View()+" "+prog+" · esc cancels"))
	case m.part.scanErr != nil:
		add("   " + s.Error.Render("✗ linkage scan: "+m.part.scanErr.Error()))
	case !m.part.info.LinkageKnown:
		add("   " + s.Subtle.Render("broken linkage: not scanned. B reads every installed package's ELF files (also: cronen --scan-linkage)"))
	case len(m.part.info.Linkage.Broken) == 0:
		add("   ✓ no broken linkage in the last scan " + s.Subtle.Render("(B scans again)"))
	default:
		broken := m.part.info.Linkage.Broken
		add("   " + s.Critical.Render("✗ "+plural(len(broken), "broken library", "broken libraries")+" in the last scan") + " " + s.Subtle.Render("(B scans again)"))
		const maxShown = 8
		for i, b := range broken {
			if i == maxShown {
				add("     " + s.Subtle.Render("… and "+itoa(len(broken)-maxShown)+" more (cronen --scan-linkage lists all)"))
				break
			}
			add("     " + oneLineUI(b.String()))
		}
		for _, f := range m.fixes() {
			add("   " + s.Info.Render("→ ") + f.Text)
		}
		for _, f := range m.fixes() {
			if f.Kind == linkage.FixRebuild && f.Argv != nil {
				add("   " + s.Subtle.Render("b hands the rebuild off (after a confirmation)"))
				break
			}
		}
	}
}

// oneLineUI keeps control characters out of a line of text from the system.
func oneLineUI(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return ' '
		}
		return r
	}, s)
}

// cancelCtxErr reports whether the scan's context was cancelled: it is
// nil until cancel was called. Tests use it.
func (r *linkageRun) cancelCtxErr() error {
	if r.ctx == nil {
		return nil
	}
	return r.ctx.Err()
}
