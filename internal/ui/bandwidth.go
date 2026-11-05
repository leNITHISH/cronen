package ui

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/leNITHISH/cronen/internal/bandwidth"
	"github.com/leNITHISH/cronen/internal/ops"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/preflight"
	"github.com/leNITHISH/cronen/internal/summary"
	"github.com/leNITHISH/cronen/internal/system"
)

// netState is the network side of the bandwidth helper: whether the
// connection is metered, and the ParallelDownloads hint.
type netState struct {
	metered summary.MeteredInfo
	hint    string
}

type netMsg struct {
	gen     int
	metered summary.MeteredInfo
	hint    string
}

func loadNet(ctx context.Context, r system.Runner, fsys system.FS, check bool, gen int) tea.Cmd {
	return func() tea.Msg {
		msg := netMsg{gen: gen, hint: summary.ParallelDownloadsHint(fsys)}
		if check {
			msg.metered = summary.LoadMetered(ctx, r, fsys)
		}
		return msg
	}
}

// lastUsedResult is the "last used" lookup of one package.
type lastUsedResult struct {
	done bool
	at   time.Time
	ok   bool
}

type lastUsedMsg struct {
	gen  int
	name string
	at   time.Time
	ok   bool
}

func loadLastUsed(ctx context.Context, r system.Runner, fsys system.FS, name string, gen int) tea.Cmd {
	return func() tea.Msg {
		at, ok := summary.LoadLastUsed(ctx, r, fsys, name)
		return lastUsedMsg{gen: gen, name: name, at: at, ok: ok}
	}
}

// handoffTiming remembers when a hand-off began, and the preview it began
// with, so its duration can be recorded when it ends.
type handoffTiming struct {
	started   time.Time
	tx        pacman.Transaction
	previewed bool
}

// handleNet stores the network facts and re-assesses the preflight checks.
func (m *Model) handleNet(msg netMsg) tea.Cmd {
	if msg.gen != m.gen {
		return nil
	}
	m.net = netState{metered: msg.metered, hint: msg.hint}
	return m.changed()
}

// handleLastUsed stores a lookup and redraws the detail pane.
func (m *Model) handleLastUsed(msg lastUsedMsg) tea.Cmd {
	if msg.gen != m.gen {
		return nil
	}
	m.lastUsed[msg.name] = lastUsedResult{done: true, at: msg.at, ok: msg.ok}
	return m.refreshDetail()
}

// lastUsedCmd starts the "last used" lookup of the selected package, once.
// Only installed packages have anything to look up.
func (m *Model) lastUsedCmd(e entry) tea.Cmd {
	if (e.kind != entryUpdate && e.kind != entryHeld) || m.opts.Runner == nil || m.opts.FS == nil {
		return nil
	}
	if _, seen := m.lastUsed[e.pkg.Name]; seen {
		return nil
	}
	m.lastUsed[e.pkg.Name] = lastUsedResult{}
	return loadLastUsed(m.ctx(), m.opts.Runner, m.opts.FS, e.pkg.Name, m.gen)
}

// lastUsedLine is "last used ~4 months ago", or "" while unknown or when the
// filesystem can't say (noatime).
func (m Model) lastUsedLine(e entry) string {
	r := m.lastUsed[e.pkg.Name]
	if !r.done || !r.ok {
		return ""
	}
	return "last used " + bandwidth.Ago(r.at, m.now())
}

// bandwidthLine is the line under the header: how far behind the system
// is, what the upgrade costs in bytes and time. Parts that don't fit are
// dropped from the end.
func (m Model) bandwidthLine(w int) string {
	parts := m.bw.Parts()
	if len(parts) == 0 {
		return ""
	}
	for len(parts) > 2 && ansi.StringWidth(" "+strings.Join(parts, " · ")) > w {
		parts = parts[:len(parts)-1]
	}
	return fitLine(" "+m.styles.Subtle.Render(strings.Join(parts, " · ")), w)
}

// entryDownload is the bytes an entry still has to download.
func (m Model) entryDownload(e entry) int64 {
	switch e.kind {
	case entryUpdate:
		return m.txUpdates[e.pkg.Name].DownloadSize
	case entryNew:
		return e.newPkg.DownloadSize
	}
	return 0
}

// sortBySize orders entries biggest download first, keeping the order of
// equal ones.
func (m Model) sortBySize(es []entry) {
	slices.SortStableFunc(es, func(a, b entry) int {
		return cmp.Compare(m.entryDownload(b), m.entryDownload(a))
	})
}

// toggleSizeSort switches the updates list between its normal order and
// biggest download first.
func (m *Model) toggleSizeSort() tea.Cmd {
	m.updates.bySize = !m.updates.bySize
	switch {
	case !m.previewed:
		m.flash("download sizes need the transaction preview", true)
	case m.updates.bySize:
		m.flash("sorted by download size", false)
	default:
		m.flash("sorted by severity", false)
	}
	m.updates.moved = false
	return m.layout()
}

// markUpdate marks the selected installed package for removal, on the
// updates screen: dropping a big package you don't use is the honest way to
// cut the download.
func (m *Model) markUpdate() tea.Cmd {
	e, ok := m.updates.selected()
	if !ok || (e.kind != entryUpdate && e.kind != entryHeld) {
		m.flash("only installed packages can be marked", false)
		return nil
	}
	if m.pkgs.marked == nil {
		m.pkgs.marked = map[string]bool{}
	}
	if m.pkgs.marked[e.pkg.Name] {
		delete(m.pkgs.marked, e.pkg.Name)
	} else {
		m.pkgs.marked[e.pkg.Name] = true
	}
	return m.refreshDetail()
}

// removeMarked starts the removal flow for the marked packages.
func (m *Model) removeMarked() tea.Cmd {
	names := slices.Sorted(maps.Keys(m.pkgs.marked))
	if len(names) == 0 {
		m.flash("mark packages with space first, then X", false)
		return nil
	}
	return m.startRemove(ops.KindRemove, names)
}

// requestPrefetch opens the prefetch modal. The transaction preview is its
// dry run: without it, or with nothing left to download, nothing is offered.
func (m *Model) requestPrefetch() {
	if m.opts.FS == nil {
		return
	}
	plan, err := summary.PrefetchPlan(m.opts.FS, m.cfg, m.previewed, m.tx.DownloadBytes)
	if err != nil {
		m.flash(err.Error(), true)
		return
	}
	m.act = &pendingAction{kind: ops.KindPrefetch, plan: plan}
	m.actionReturn = m.screen
	m.screen, m.focus = screenConfirm, focusList
}

// prefetchView is the confirmation of a prefetch.
func (m Model) prefetchView(w int) string {
	s, p := m.styles, m.act.plan
	var lines []string
	add := func(text string) { lines = append(lines, wrapIndent(text, w, 4)) }
	add(" " + s.Bold.Render(actionTitle(m.act)))
	add("")
	add(" This runs: " + s.Bold.Render(strings.Join(p.Command, " ")))
	if m.opts.Demo {
		add(" " + s.Subtle.Render("demo: nothing is run"))
	}
	add("")
	add(" It downloads " + s.Bold.Render(pacman.FormatSize(m.tx.DownloadBytes)) + " (" +
		plural(m.tx.Packages(), "package", "packages") + ") into the package cache and installs nothing.")
	add(" " + s.Subtle.Render("It uses checkupdates' own temporary database, never the real one, so it can't cause a partial upgrade."))
	est := bandwidth.EstimateTime(m.opts.State.PrefetchSamples(), m.opts.State.UpgradeSamples(), m.tx.DownloadBytes, 0)
	add(" Download time: " + est.Text())
	add("")
	for _, pr := range m.pf.problems {
		if pr.Check == preflight.CheckMetered {
			add(" " + s.Severity(pr.Severity).Render(severityIcon(pr.Severity)+" "+pr.Message))
		}
	}
	if m.net.hint != "" {
		add(" " + s.Subtle.Render("ℹ "+m.net.hint))
	}
	add("")
	add(" Press " + s.Bold.Render("y") + " to run it, " + s.Bold.Render("esc") + " to cancel.")
	return strings.Join(lines, "\n")
}

// addBandwidth adds the cost of the upgrade to its confirmation: the
// bytes and time, and the ParallelDownloads hint.
func (m Model) addBandwidth(add func(string)) {
	if m.bw.Shown() {
		add(" Download: " + pacman.FormatSize(m.bw.Remaining) + " · estimated time: " + m.bw.Estimate.Text())
	}
	if m.net.hint != "" {
		add(" " + m.styles.Subtle.Render("ℹ "+m.net.hint))
	}
}

// recordPending keeps the day's pending download size, once a day, for the
// growth trend. Called when a preview arrives.
func (m *Model) recordPending() tea.Cmd {
	if !m.previewed || (m.opts.StatePath == "" && !m.opts.Demo) {
		return nil
	}
	st := m.opts.State.Clone()
	if !summary.RecordPending(&st, m.tx, m.now(), m.location()) {
		return nil
	}
	m.opts.State = st
	return m.saveState()
}

// previewArrived is what a new transaction preview sets off: after a
// removal, it says what the removal saved; and the day's pending size is
// recorded.
func (m *Model) previewArrived(msg previewMsg) tea.Cmd {
	if m.dlBeforeSet {
		m.dlBeforeSet = false
		if msg.err == nil && m.actionNotice != "" {
			if saved := m.dlBefore - msg.tx.DownloadBytes; saved > 0 {
				m.actionNotice += " · the download is " + pacman.FormatSize(msg.tx.DownloadBytes) + " now (saved " + pacman.FormatSize(saved) + ")"
			} else {
				m.actionNotice += " · the download is unchanged (" + pacman.FormatSize(msg.tx.DownloadBytes) + ")"
			}
		}
	}
	return m.recordPending()
}

// usageLines are the detail pane's lines about an installed package: when
// it was last used, and whether it is marked for removal (with what that
// saves in the download).
func (m Model) usageLines(e entry) []string {
	var out []string
	if l := m.lastUsedLine(e); l != "" {
		out = append(out, l)
	}
	if m.pkgs.marked[e.pkg.Name] && (e.kind == entryUpdate || e.kind == entryHeld) {
		l := "marked for removal (X)"
		if d := m.entryDownload(e); d > 0 {
			l += ": removing it first saves a " + pacman.FormatSize(d) + " download"
		}
		out = append(out, l)
	}
	return out
}

// metered reports that the metered preflight problem is raised.
func (m Model) metered() bool {
	for _, p := range m.pf.problems {
		if p.Check == preflight.CheckMetered {
			return true
		}
	}
	return false
}
