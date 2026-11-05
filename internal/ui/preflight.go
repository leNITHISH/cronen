package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/mirror"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/preflight"
	"github.com/leNITHISH/cronen/internal/summary"
	"github.com/leNITHISH/cronen/internal/system"
)

// preflightState is the preflight checks' share of the model: the system
// facts, the mirror's freshness, and the problems they add up to.
type preflightState struct {
	factsSec, mirrorSec section
	facts               preflight.Facts
	factsLoaded         bool
	mirror              mirror.Result
	mirrorLoaded        bool
	problems            []preflight.Problem
}

type factsMsg struct {
	gen   int
	facts preflight.Facts
	err   error
}

type mirrorMsg struct {
	gen int
	res mirror.Result
	err error
}

func loadFacts(fsys system.FS, disk system.Disk, repo []pacman.Update, gen int) tea.Cmd {
	return func() tea.Msg {
		facts, err := summary.LoadPreflight(fsys, disk, repo)
		return factsMsg{gen: gen, facts: facts, err: err}
	}
}

func loadMirror(ctx context.Context, fsys system.FS, f system.Fetcher, gen int, fresh bool) tea.Cmd {
	return func() tea.Msg {
		res, err := summary.LoadMirror(ctx, fsys, f, fresh)
		return mirrorMsg{gen: gen, res: res, err: err}
	}
}

// preflightEnabled reports whether the preflight checks run at all.
func (m Model) preflightEnabled() bool {
	return m.cfg.Preflight && m.opts.Disk != nil
}

// markPreflightLoading is part of markLoading.
func (m *Model) markPreflightLoading() {
	m.pf.factsSec = section{loading: m.preflightEnabled()}
	m.pf.mirrorSec = section{loading: m.preflightEnabled() && m.cfg.MirrorCheck}
}

// mirrorCmds returns the command that checks the mirror, if enabled.
func (m Model) mirrorCmds(fresh bool) []tea.Cmd {
	if !m.preflightEnabled() || !m.cfg.MirrorCheck {
		return nil
	}
	return []tea.Cmd{loadMirror(m.ctx(), m.opts.FS, m.opts.Fetcher, m.gen, fresh)}
}

// factsCmd returns the command that reads the system facts once the
// pending repo updates are known (nil when checkupdates failed: the checks
// that don't depend on them still run).
func (m Model) factsCmd(repo []pacman.Update) tea.Cmd {
	if !m.preflightEnabled() {
		return nil
	}
	return loadFacts(m.opts.FS, m.opts.Disk, repo, m.gen)
}

// updatePreflight handles factsMsg and mirrorMsg.
func (m Model) updatePreflight(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case factsMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.pf.factsSec = section{err: msg.err}
		m.pf.facts, m.pf.factsLoaded = msg.facts, true
	case mirrorMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.pf.mirrorSec = section{err: msg.err}
		m.pf.mirror, m.pf.mirrorLoaded = msg.res, msg.err == nil
	}
	return m, m.changed()
}

// assessPreflight re-runs the checks after any data arrived.
func (m *Model) assessPreflight() {
	m.pf.problems = nil
	// The partial-upgrade warning and the snapshot age are their own
	// sources: they show without the system facts.
	defer func() {
		if m.cfg.PartialCheck && m.part.loaded {
			m.pf.problems = summary.AddPartial(m.pf.problems, m.part.info)
		}
		if m.cfg.Preflight && m.snap.loaded {
			m.pf.problems = summary.AddSnapshot(m.pf.problems, m.snap.info, m.cfg, m.now())
		}
	}()
	if !m.preflightEnabled() || !m.pf.factsLoaded {
		return
	}
	var tx *pacman.Transaction
	if m.previewed {
		tx = &m.tx
	}
	var mir *mirror.Result
	if m.pf.mirrorLoaded && m.cfg.MirrorCheck {
		mir = &m.pf.mirror
	}
	facts := m.pf.facts
	facts.Metered, facts.MeteredDevice = m.net.metered.Metered, m.net.metered.Device
	m.pf.problems = summary.AssessPreflight(facts, tx, mir, m.lastFullUpgrade(), m.cfg, m.now())
}

// preflightBanner is the banner line for the preflight problems, most
// severe first. Several collapse into one line; the upgrade confirmation
// lists them all.
func (m Model) preflightBanner(w int) string {
	ps := m.pf.problems
	if len(ps) == 0 {
		return ""
	}
	top := ps[0]
	text := " " + severityIcon(top.Severity) + " "
	if len(ps) > 1 {
		text += itoa(len(ps)) + " preflight problems (U lists them): "
	}
	text += top.Message
	return fitLine(m.styles.Severity(top.Severity).Bold(true).Render(text), w)
}

// addPreflight adds the preflight problems to the upgrade confirmation.
func (m Model) addPreflight(add func(string)) {
	if len(m.pf.problems) == 0 {
		return
	}
	add(" " + m.styles.Bold.Render(plural(len(m.pf.problems), "preflight problem", "preflight problems")))
	for _, p := range m.pf.problems {
		add("   " + m.styles.Severity(p.Severity).Render(severityIcon(p.Severity)) + " " + p.Message)
	}
	add("")
}

// addPreflightRows adds a PREFLIGHT section to the updates list when a
// preflight source failed. Healthy checks show up as the banner only.
func (m *Model) addPreflightRows(p *updatesPane, note func(string, bool, bool)) {
	var failed []error
	for _, s := range []section{m.pf.factsSec, m.pf.mirrorSec} {
		if s.err != nil && !s.loading {
			failed = append(failed, s.err)
		}
	}
	if len(failed) == 0 {
		return
	}
	p.rows = append(p.rows, row{kind: rowSection, text: "PREFLIGHT"})
	for _, err := range failed {
		note("✗ "+err.Error(), false, true)
	}
}
