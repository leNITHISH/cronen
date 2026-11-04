package ui

import (
	"context"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/ops"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/summary"
	"github.com/leNITHISH/cronen/internal/system"
)

// housekeepingPane is the Housekeeping screen: preflight results, orphans,
// .pacnew diffs, the package cache and failed units. Each source has its
// own section state and a failed one only fills its own part.
type housekeepingPane struct {
	// unmarked are orphans the user took out of the removal; every orphan
	// starts marked.
	unmarked map[string]bool
	sel      int
	offset   int

	diffs    []summary.PacnewDiff
	cache    summary.CacheInfo
	units    summary.FailedUnits
	diffSec  section
	cacheSec section
	unitsSec section
}

type pacnewDiffMsg struct {
	gen   int
	diffs []summary.PacnewDiff
}

type cacheMsg struct {
	gen  int
	info summary.CacheInfo
	err  error
}

type unitsMsg struct {
	gen   int
	units summary.FailedUnits
	err   error
}

func loadPacnewDiffs(ctx context.Context, r system.Runner, files []string, gen int) tea.Cmd {
	return func() tea.Msg {
		return pacnewDiffMsg{gen: gen, diffs: summary.LoadPacnewDiffs(ctx, r, files)}
	}
}

func loadCache(fsys system.FS, extra []string, home string, gen int) tea.Cmd {
	return func() tea.Msg {
		info, err := summary.LoadCache(fsys, extra, home)
		return cacheMsg{gen: gen, info: info, err: err}
	}
}

func loadUnits(ctx context.Context, r system.Runner, gen int) tea.Cmd {
	return func() tea.Msg {
		u, err := summary.LoadFailedUnits(ctx, r)
		return unitsMsg{gen: gen, units: u, err: err}
	}
}

// housekeepingCmds are the loads of the Housekeeping screen's own sources.
func (m Model) housekeepingCmds() []tea.Cmd {
	if !m.cfg.Housekeeping {
		return nil
	}
	return []tea.Cmd{
		loadCache(m.opts.FS, m.cfg.ExtraCacheDirs, m.opts.Home, m.gen),
		loadUnits(m.ctx(), m.opts.Runner, m.gen),
	}
}

// handleHousekeepingMsg handles the three loads above. It reports whether
// msg was one of them.
func (m *Model) handleHousekeepingMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case pacnewDiffMsg:
		if msg.gen == m.gen {
			m.hk.diffSec, m.hk.diffs = section{}, msg.diffs
		}
	case cacheMsg:
		if msg.gen == m.gen {
			m.hk.cacheSec, m.hk.cache = section{err: msg.err}, msg.info
		}
	case unitsMsg:
		if msg.gen == m.gen {
			m.hk.unitsSec, m.hk.units = section{err: msg.err}, msg.units
		}
	default:
		return nil, false
	}
	return nil, true
}

// orphanNames are the installed orphans, by name.
func (m Model) orphanNames() []string {
	if m.localDB == nil {
		return nil
	}
	var names []string
	for n := range m.localDB.Orphans {
		if _, ok := m.localDB.Lookup(n); ok {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	return names
}

// markedOrphans are the orphans that X would remove.
func (m Model) markedOrphans() []string {
	var out []string
	for _, n := range m.orphanNames() {
		if !m.hk.unmarked[n] {
			out = append(out, n)
		}
	}
	return out
}

// handleHousekeepingKey handles the keys of the Housekeeping screen.
func (m *Model) handleHousekeepingKey(k tea.KeyPressMsg) (tea.Cmd, bool) {
	keys, h := m.keys, &m.hk
	orphans := m.orphanNames()
	switch {
	case key.Matches(k, keys.Down):
		h.sel = clamp(h.sel+1, 0, max(0, len(orphans)-1))
	case key.Matches(k, keys.Up):
		h.sel = clamp(h.sel-1, 0, max(0, len(orphans)-1))
	case key.Matches(k, keys.Top):
		h.sel = 0
	case key.Matches(k, keys.Bottom):
		h.sel = max(0, len(orphans)-1)
	case key.Matches(k, keys.HalfDown):
		h.offset += max(1, m.bodyHeight()/2)
	case key.Matches(k, keys.HalfUp):
		h.offset = max(0, h.offset-max(1, m.bodyHeight()/2))
	case key.Matches(k, keys.Mark):
		if h.sel < len(orphans) {
			if h.unmarked == nil {
				h.unmarked = map[string]bool{}
			}
			n := orphans[h.sel]
			h.unmarked[n] = !h.unmarked[n]
		}
	case key.Matches(k, keys.Remove):
		names := m.markedOrphans()
		if len(names) == 0 {
			m.flash("no orphans marked", false)
			return nil, true
		}
		return m.startRemove(ops.KindOrphans, names), true
	case key.Matches(k, keys.Pacdiff):
		m.startPacdiff()
	default:
		return nil, false
	}
	return nil, true
}

// housekeepingView renders the whole screen in w×h cells, scrolled so the
// selected orphan shows.
func (m *Model) housekeepingView(w, h int) string {
	lines, selLine := m.housekeepingLines(w)
	hk := &m.hk
	if selLine >= 0 {
		if selLine < hk.offset {
			hk.offset = selLine
		}
		if selLine >= hk.offset+h {
			hk.offset = selLine - h + 1
		}
	}
	hk.offset = clamp(hk.offset, 0, max(0, len(lines)-h))
	return strings.Join(lines[hk.offset:], "\n")
}

// housekeepingLines are the screen's lines, and the index of the selected
// orphan's line (-1 if none).
func (m Model) housekeepingLines(w int) ([]string, int) {
	s := m.styles
	var lines []string
	add := func(text string) { lines = append(lines, fitLine(text, w)) }
	title := func(text string) {
		add("")
		add(" " + s.Section.Render(text))
	}
	selLine := -1

	if !m.cfg.Housekeeping {
		add("")
		add(" " + s.Subtle.Render("housekeeping is off (housekeeping = false); preflight and orphans still show"))
	}

	// Preflight.
	title("Preflight")
	switch {
	case !m.preflightEnabled():
		add("   " + s.Subtle.Render("preflight checks are off"))
	case m.pf.factsSec.loading:
		add("   " + s.Subtle.Render(m.spinner.View()+" checking…"))
	case m.pf.factsSec.err != nil:
		add("   " + s.Error.Render("✗ "+m.pf.factsSec.err.Error()))
	case len(m.pf.problems) == 0:
		add("   ✓ no problems")
	}
	for _, p := range m.pf.problems {
		add("   " + s.Severity(p.Severity).Render(severityIcon(p.Severity)) + " " + p.Message)
	}
	if m.pf.factsLoaded && m.pf.facts.RebootPending {
		add("   " + s.Warn.Render("⚠ reboot pending: the running kernel "+m.pf.facts.RunningKernel+" was replaced"))
	}

	// Orphans.
	orphans := m.orphanNames()
	switch {
	case !m.cfg.PackageBrowser:
		title("Orphans")
		add("   " + s.Subtle.Render("needs package_browser = true"))
	case m.localSec.loading && m.localDB == nil:
		title("Orphans")
		add("   " + s.Subtle.Render(m.spinner.View()+" reading installed packages…"))
	case m.localSec.err != nil:
		title("Orphans")
		add("   " + s.Error.Render("✗ "+m.localSec.err.Error()))
	default:
		var total, marked int64
		for _, n := range orphans {
			p, _ := m.localDB.Lookup(n)
			total += p.InstalledSize
			if !m.hk.unmarked[n] {
				marked += p.InstalledSize
			}
		}
		head := "Orphans (" + itoa(len(orphans)) + ")"
		if len(orphans) > 0 {
			head += " · " + pacman.FormatSize(total) + ", " + pacman.FormatSize(marked) + " marked"
		}
		title(head)
		if len(orphans) == 0 {
			add("   ✓ no orphans")
		}
		for i, n := range orphans {
			p, _ := m.localDB.Lookup(n)
			cursor, mark := "  ", "☐ "
			if !m.hk.unmarked[n] {
				mark = "☑ "
			}
			if i == m.hk.sel {
				cursor, selLine = "▸ ", len(lines)
			}
			add("  " + cursor + mark + n + " " + s.Subtle.Render(p.Version+" "+pacman.FormatSize(p.InstalledSize)))
		}
		if len(orphans) > 0 {
			add("   " + s.Subtle.Render("space mark · X remove the marked · removing orphans can create new orphans"))
		}
	}

	// .pacnew files.
	switch {
	case m.pacnewSec.loading:
		title("Config files (.pacnew, .pacsave)")
		add("   " + s.Subtle.Render(m.spinner.View()+" looking…"))
	case m.pacnewSec.err != nil:
		title("Config files (.pacnew, .pacsave)")
		add("   " + s.Error.Render("✗ "+m.pacnewSec.err.Error()))
	default:
		title("Config files (" + itoa(len(m.pacnew)) + ")")
		if len(m.pacnew) == 0 {
			add("   ✓ none to merge")
		} else {
			add("   " + s.Subtle.Render("P merge them with "+strings.Join(m.cfg.PacdiffArgv(), " ")))
		}
		for _, f := range m.pacnew {
			add("   " + s.Bold.Render(f))
			for _, d := range m.hk.diffs {
				if d.File != f {
					continue
				}
				m.addDiff(add, d)
			}
		}
	}

	// Package cache.
	title("Package cache")
	switch {
	case !m.cfg.Housekeeping:
		add("   " + s.Subtle.Render("off"))
	case m.hk.cacheSec.err != nil:
		add("   " + s.Error.Render("✗ "+m.hk.cacheSec.err.Error()))
	default:
		c := m.hk.cache
		add("   " + plural(c.Files, "package", "packages") + ", " + pacman.FormatSize(c.Bytes) + " in " + strings.Join(c.Dirs, ", "))
		add("   " + s.Subtle.Render("to trim it: paccache -r (paccache is not run by cronen)"))
	}

	// Failed units.
	title("Failed units")
	switch {
	case !m.cfg.Housekeeping:
		add("   " + s.Subtle.Render("off"))
	case m.hk.unitsSec.err != nil:
		add("   " + s.Error.Render("✗ "+m.hk.unitsSec.err.Error()))
	case m.hk.units.Count() == 0:
		add("   ✓ none")
	default:
		for _, u := range m.hk.units.System {
			add("   " + s.Warn.Render("✗") + " " + u)
		}
		for _, u := range m.hk.units.User {
			add("   " + s.Warn.Render("✗") + " " + u + s.Subtle.Render(" (user)"))
		}
	}

	// Vulnerable packages with no fix.
	title("Vulnerable, no fix yet (" + itoa(len(m.result.Vulnerable)) + ")")
	if len(m.result.Vulnerable) == 0 {
		add("   ✓ none known")
	}
	for _, v := range m.result.Vulnerable {
		add("   " + s.Warn.Render("⚠") + " " + risk.VulnerableReason(v))
	}
	return lines, selLine
}

// addDiff adds the lines of one .pacnew diff, coloured and capped.
func (m Model) addDiff(add func(string), d summary.PacnewDiff) {
	s := m.styles
	switch {
	case d.NeedsRoot:
		add("     " + s.Subtle.Render("needs root to view"))
		return
	case d.Err != nil:
		add("     " + s.Error.Render("✗ "+d.Err.Error()))
		return
	case d.Diff == "":
		add("     " + s.Subtle.Render("identical to "+d.Original))
		return
	}
	const maxDiff = 12
	n := 0
	for l := range strings.Lines(d.Diff) {
		l = strings.TrimRight(l, "\n")
		if n == maxDiff {
			add("     " + s.Subtle.Render("… (P shows the rest)"))
			break
		}
		n++
		style := s.Subtle
		switch {
		case strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++"):
			style = s.Info
		case strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---"):
			style = s.Warn
		}
		add("     " + style.Render(l))
	}
}

// housekeepingStatus are the status bar parts of the screen.
func (m Model) housekeepingStatus() []string {
	parts := []string{"housekeeping"}
	if n := len(m.orphanNames()); n > 0 {
		parts = append(parts, plural(len(m.markedOrphans()), "orphan", "orphans")+" marked of "+itoa(n))
	}
	if n := len(m.pacnew); n > 0 {
		parts = append(parts, plural(n, ".pacnew", ".pacnew"))
	}
	return parts
}
