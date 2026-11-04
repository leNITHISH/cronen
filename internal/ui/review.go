package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/leNITHISH/cronen/internal/aur"
	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/summary"
)

// The AUR review: the commit log in the detail pane of a pending AUR
// update, and the PKGBUILD diff screen (d).

// reviewState is what is known about one pending AUR update's history.
type reviewState struct {
	commits []aur.Commit
	logDone bool
	logErr  error
	// review is set once the diff is loaded.
	review      *aur.Review
	diffLoading bool
	diffErr     error
}

type aurLogMsg struct {
	gen     int
	name    string
	commits []aur.Commit
	err     error
}

type aurDiffMsg struct {
	gen    int
	name   string
	review aur.Review
	err    error
}

func loadAURLog(m Model, p aur.Package) tea.Cmd {
	ctx, f, gen, fresh := m.ctx(), m.opts.Fetcher, m.gen, m.gen == m.freshGen
	return func() tea.Msg {
		commits, err := summary.LoadAURLog(ctx, f, p, fresh)
		return aurLogMsg{gen: gen, name: p.Name, commits: commits, err: err}
	}
}

func loadAURDiff(m Model, p aur.Package, commits []aur.Commit) tea.Cmd {
	ctx, r, f, loc, gen, fresh := m.ctx(), m.opts.Runner, m.opts.Fetcher, m.location(), m.gen, m.gen == m.freshGen
	return func() tea.Msg {
		rev, err := summary.LoadAURReview(ctx, r, f, loc, p, commits, fresh)
		return aurDiffMsg{gen: gen, name: p.Name, review: rev, err: err}
	}
}

// reviewable reports whether p is a pending AUR update with a record to
// look the history up by.
func reviewable(p aur.Package) bool { return p.Pending && p.Info != nil }

// aurLoaded runs when the AUR packages arrive. It records the maintainers
// seen for the first time as the baseline (see rule 12) and starts fetching
// the history of each pending update: just the log in lazy mode, the diff
// too in eager mode.
func (m *Model) aurLoaded() tea.Cmd {
	var cmds []tea.Cmd
	st := m.opts.State.Clone()
	if summary.RecordMaintainers(&st, m.aurPkgs, false) {
		m.opts.State = st
		cmds = append(cmds, m.saveState())
	}
	if m.opts.Fetcher == nil {
		return tea.Batch(cmds...)
	}
	eager := m.cfg.AURReview == config.AURReviewEager
	for _, p := range m.aurPkgs {
		if !reviewable(p) {
			continue
		}
		if _, seen := m.reviews[p.Name]; seen {
			continue
		}
		m.reviews[p.Name] = reviewState{}
		if eager {
			cmds = append(cmds, loadAURDiff(*m, p, nil))
		} else {
			cmds = append(cmds, loadAURLog(*m, p))
		}
	}
	return tea.Batch(cmds...)
}

// reviewMap is the loaded reviews by package name, for rule 13.
func (m Model) reviewMap() map[string]aur.Review {
	out := map[string]aur.Review{}
	for name, r := range m.reviews {
		if r.review != nil {
			out[name] = *r.review
		}
	}
	return out
}

func (m *Model) handleAURLog(msg aurLogMsg) tea.Cmd {
	if msg.gen != m.gen {
		return nil
	}
	r := m.reviews[msg.name]
	r.logDone, r.logErr = true, msg.err
	if msg.err == nil {
		r.commits = msg.commits
	}
	m.reviews[msg.name] = r
	return m.refreshDetail()
}

func (m *Model) handleAURDiff(msg aurDiffMsg) tea.Cmd {
	if msg.gen != m.gen {
		return nil
	}
	r := m.reviews[msg.name]
	r.diffLoading, r.diffErr = false, msg.err
	if msg.err == nil {
		rev := msg.review
		r.review, r.commits, r.logDone, r.logErr = &rev, rev.Commits, true, nil
	}
	m.reviews[msg.name] = r
	if m.screen == screenDiff && m.diffName == msg.name {
		m.renderDiff()
	}
	// Rule 13 may have raised flags now.
	return m.changed()
}

// selectedReviewable returns the selected entry's AUR package if it is a
// pending update.
func (m Model) selectedReviewable() (aur.Package, bool) {
	e, ok := m.updates.selected()
	if !ok || e.aur == nil || !reviewable(*e.aur) {
		return aur.Package{}, false
	}
	return *e.aur, true
}

// openDiff opens the PKGBUILD diff of the selected pending AUR update,
// fetching it first if needed.
func (m *Model) openDiff() tea.Cmd {
	p, ok := m.selectedReviewable()
	if !ok {
		m.flash("d reviews the PKGBUILD diff of a pending AUR update", false)
		return nil
	}
	if m.opts.Fetcher == nil {
		return nil
	}
	m.diffName = p.Name
	m.screen, m.focus = screenDiff, focusList
	r := m.reviews[p.Name]
	if r.review == nil && !r.diffLoading {
		r.diffLoading, r.diffErr = true, nil
		m.reviews[p.Name] = r
		m.renderDiff()
		var commits []aur.Commit
		if r.logDone && r.logErr == nil {
			commits = r.commits
		}
		return loadAURDiff(*m, p, commits)
	}
	m.renderDiff()
	return nil
}

// handleDiffKey handles keys on the diff screen.
func (m Model) handleDiffKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	vp := &m.diffView
	switch {
	case key.Matches(msg, k.Quit):
		return m, tea.Quit
	case key.Matches(msg, k.Back), key.Matches(msg, k.Diff):
		m.screen = screenUpdates
	case key.Matches(msg, k.Down):
		vp.ScrollDown(1)
	case key.Matches(msg, k.Up):
		vp.ScrollUp(1)
	case key.Matches(msg, k.Top):
		vp.GotoTop()
	case key.Matches(msg, k.Bottom):
		vp.GotoBottom()
	case key.Matches(msg, k.HalfDown):
		vp.HalfPageDown()
	case key.Matches(msg, k.HalfUp):
		vp.HalfPageUp()
	}
	return m, nil
}

// cleanLine removes what could drive the terminal from a line of text that
// came from the network: control characters, escape sequences and tabs.
func cleanLine(s string) string {
	s = strings.ReplaceAll(ansi.Strip(s), "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
}

// renderDiff rebuilds the diff viewport for the package being reviewed.
func (m *Model) renderDiff() {
	w, _ := m.size()
	m.diffView.SetWidth(w)
	m.diffView.SetHeight(m.bodyHeight())
	m.diffView.SetContent(m.diffContent())
	m.diffView.GotoTop()
}

func (m Model) diffContent() string {
	s := m.styles
	r := m.reviews[m.diffName]
	var b strings.Builder
	line := func(text string) { b.WriteString(text + "\n") }

	line(" " + s.Bold.Render(m.diffName) + s.Subtle.Render("  PKGBUILD diff"))
	switch {
	case r.diffLoading:
		line("")
		line(" " + m.spinner.View() + " fetching the diff from aur.archlinux.org…")
		return b.String()
	case r.diffErr != nil:
		line("")
		line(" " + s.Error.Render("✗ "+cleanLine(r.diffErr.Error())))
		return b.String()
	case r.review == nil:
		line("")
		line(" " + s.Subtle.Render("no diff loaded"))
		return b.String()
	}

	rev := *r.review
	loc := m.location()
	if rev.HasBase && len(rev.Commits) > 0 {
		newest := rev.Commits[0]
		line(" " + s.Subtle.Render(fmt.Sprintf("%s (%s) → %s (%s)",
			shortID(rev.Base.ID), rev.Base.Date.In(loc).Format("2006-01-02"),
			shortID(newest.ID), newest.Date.In(loc).Format("2006-01-02"))))
	}
	line("")
	if rev.Note != "" {
		line(" " + s.Warn.Render("No diff: "+cleanLine(rev.Note)+"."))
		if len(rev.Commits) > 0 {
			line("")
			line(" Commits:")
			for _, c := range rev.Commits {
				line("   " + commitLine(c, loc))
			}
		}
		return b.String()
	}
	if len(rev.Findings) > 0 {
		line(" " + s.Warn.Render("Suspicious changes:"))
		for _, f := range rev.Findings {
			line(" " + s.Warn.Render("  ⚠ "+f.Reason))
		}
		line("")
	}

	suspicious := map[int]bool{}
	for _, f := range rev.Findings {
		suspicious[f.Line] = true
	}
	c := lipgloss.LightDark(m.isDark)
	add := lipgloss.NewStyle().Foreground(c(lipgloss.Color("#1B6E2D"), lipgloss.Color("#8FD19E")))
	del := lipgloss.NewStyle().Foreground(c(lipgloss.Color("#B3261E"), lipgloss.Color("#F2B8B5")))
	hunk := s.Info
	for i, raw := range strings.Split(rev.Diff, "\n") {
		text := cleanLine(raw)
		switch {
		case suspicious[i]:
			line(s.Warn.Reverse(true).Render("⚠ " + text))
		case strings.HasPrefix(text, "+++"), strings.HasPrefix(text, "---"), strings.HasPrefix(text, "diff "), strings.HasPrefix(text, "index "):
			line(s.Bold.Render(text))
		case strings.HasPrefix(text, "@@"):
			line(hunk.Render(text))
		case strings.HasPrefix(text, "+"):
			line(add.Render(text))
		case strings.HasPrefix(text, "-"):
			line(del.Render(text))
		default:
			line(text)
		}
	}
	return b.String()
}

func shortID(id string) string {
	if len(id) > 7 {
		return id[:7]
	}
	return id
}

// diffScreenView is the body of the diff screen.
func (m Model) diffScreenView() string {
	return m.diffView.View()
}

// reviewLines are the lines the detail pane adds for an AUR package: its
// maintainers, the latest commits and the hint for the diff. They are
// plain; the caller bullets them.
func (m Model) reviewLines(e entry) []string {
	if e.aur == nil || e.aur.Info == nil {
		return nil
	}
	info := e.aur.Info
	loc := m.location()
	l := "maintainer: none (orphaned)"
	if info.Maintainer != "" {
		l = "maintainer: " + cleanLine(info.Maintainer)
	}
	if len(info.CoMaintainers) > 0 {
		l += " · co-maintainers: " + cleanLine(strings.Join(info.CoMaintainers, ", "))
	}
	lines := []string{l}
	if !reviewable(*e.aur) {
		return lines
	}

	r, ok := m.reviews[e.aur.Name]
	switch {
	case !ok || (!r.logDone && !r.diffLoading && r.review == nil):
		lines = append(lines, "latest commits: loading…")
	case r.logErr != nil:
		lines = append(lines, "latest commits unavailable: "+cleanLine(r.logErr.Error()))
	case len(r.commits) == 0 && r.logDone:
		lines = append(lines, "no commits found on the AUR")
	default:
		for i, c := range r.commits {
			if i == 3 {
				break
			}
			lines = append(lines, commitLine(c, loc))
		}
	}
	hint := "d: review the PKGBUILD diff"
	if r.review != nil {
		switch {
		case len(r.review.Findings) > 0:
			hint += " (" + plural(len(r.review.Findings), "suspicious line", "suspicious lines") + ")"
		case r.review.Note != "":
			hint = "d: no diff (" + cleanLine(r.review.Note) + ")"
		default:
			hint += " (nothing suspicious found)"
		}
	}
	return append(lines, hint)
}

func commitLine(c aur.Commit, loc *time.Location) string {
	return c.Date.In(loc).Format("2006-01-02") + "  " + cleanLine(c.Author) + "  " + cleanLine(c.Title)
}
