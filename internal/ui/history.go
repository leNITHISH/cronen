package ui

import (
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/system"
)

// historyPane is the History screen: pacman.log read into transactions,
// newest first. With a name filter the rows turn into the history of the
// packages that match: one row per change.
type historyPane struct {
	// txs are the transactions, newest first.
	txs []pacman.LogTransaction
	// cache is what the package cache holds, by package name.
	cache map[string][]pacman.CachedPackage

	rows    []histRow
	sel     int
	selKey  string
	offset  int
	cacheEr error
}

// histRow is one row: a transaction, or (with a filter) one change of it.
type histRow struct {
	tx int // index into historyPane.txs
	// change is the index into the transaction's changes, or -1 for a
	// transaction row.
	change int
}

func (r histRow) key() string { return itoa(r.tx) + "/" + itoa(r.change) }

type historyMsg struct {
	gen   int
	txs   []pacman.LogTransaction
	cache []pacman.CachedPackage
	// cacheErr is why the cache couldn't be listed: History still shows.
	cacheErr error
	err      error
}

func loadHistory(fsys system.FS, extra []string, home string, loc *time.Location, gen int) tea.Cmd {
	return func() tea.Msg {
		data, err := fsys.ReadFile(pacman.LogPath)
		if err != nil {
			return historyMsg{gen: gen, err: err}
		}
		msg := historyMsg{gen: gen, txs: pacman.ReverseTransactions(pacman.ParseHistory(data, loc))}
		msg.cache, msg.cacheErr = pacman.ScanCache(fsys, pacman.ResolveCacheDirs(fsys, extra, home))
		return msg
	}
}

func (m *Model) handleHistory(msg historyMsg) tea.Cmd {
	if msg.gen != m.gen {
		return nil
	}
	m.histSec = section{err: msg.err}
	h := &m.hist
	h.txs, h.cache, h.cacheEr = nil, nil, msg.cacheErr
	if msg.err == nil {
		h.txs = msg.txs
		h.cache = map[string][]pacman.CachedPackage{}
		for _, c := range msg.cache {
			h.cache[c.Name] = append(h.cache[c.Name], c)
		}
	}
	m.rebuildHistory()
	return m.refreshDetail()
}

// inCache reports whether the package cache still holds name at version.
func (h historyPane) inCache(name, version string) bool {
	if version == "" {
		return false
	}
	return slices.ContainsFunc(h.cache[name], func(c pacman.CachedPackage) bool {
		return pacman.SameVersion(c.Version, version)
	})
}

// rebuildHistory lists the rows for the current filter and keeps the
// selection on the same row.
func (m *Model) rebuildHistory() {
	h := &m.hist
	h.rows = h.rows[:0]
	q := ""
	if m.screen == screenHistory {
		q = m.filterQuery()
	}
	for i, tx := range h.txs {
		if q == "" {
			h.rows = append(h.rows, histRow{tx: i, change: -1})
			continue
		}
		for j, c := range tx.Changes {
			if strings.Contains(c.Name, q) {
				h.rows = append(h.rows, histRow{tx: i, change: j})
			}
		}
	}
	h.sel = 0
	for i, r := range h.rows {
		if r.key() == h.selKey {
			h.sel = i
			break
		}
	}
	if len(h.rows) > 0 {
		h.selKey = h.rows[h.sel].key()
	} else {
		h.selKey = ""
	}
	h.scroll(m.bodyHeight())
}

func (h *historyPane) selected() (histRow, bool) {
	if h.sel < 0 || h.sel >= len(h.rows) {
		return histRow{}, false
	}
	return h.rows[h.sel], true
}

func (h *historyPane) move(delta int) {
	if len(h.rows) == 0 {
		return
	}
	delta = clamp(delta, -len(h.rows), len(h.rows))
	h.sel = clamp(h.sel+delta, 0, len(h.rows)-1)
	h.selKey = h.rows[h.sel].key()
}

func (h *historyPane) scroll(height int) {
	height-- // the column header
	if height <= 0 {
		return
	}
	if h.sel < h.offset {
		h.offset = h.sel
	}
	if h.sel >= h.offset+height {
		h.offset = h.sel - height + 1
	}
	h.offset = clamp(h.offset, 0, max(0, len(h.rows)-height))
}

// handleHistoryKey handles the keys of the History screen that the shared
// handler doesn't. It reports whether the key was used.
func (m *Model) handleHistoryKey(k tea.KeyPressMsg) (tea.Cmd, bool) {
	keys, h := m.keys, &m.hist
	delta := 0
	switch {
	case key.Matches(k, keys.Downgrade):
		return m.downgradeFromHistory(), true
	case key.Matches(k, keys.Down):
		delta = 1
	case key.Matches(k, keys.Up):
		delta = -1
	case key.Matches(k, keys.Top):
		delta = -1 << 30
	case key.Matches(k, keys.Bottom):
		delta = 1 << 30
	default:
		return nil, false
	}
	h.move(delta)
	h.scroll(m.bodyHeight())
	return m.refreshDetail(), true
}

// downgradeFromHistory starts a downgrade for the package of the selected
// row: with a filter, the package that row changed; without one, the one
// package the transaction changed.
func (m *Model) downgradeFromHistory() tea.Cmd {
	row, ok := m.hist.selected()
	if !ok {
		m.flash("nothing selected", false)
		return nil
	}
	tx := m.hist.txs[row.tx]
	switch {
	case row.change >= 0:
		return m.startDowngrade(tx.Changes[row.change].Name)
	case len(tx.Changes) == 1:
		return m.startDowngrade(tx.Changes[0].Name)
	}
	m.flash("this transaction changed several packages: filter by one with / first, then D", false)
	return nil
}

// txCounts is the compact summary of a transaction's changes.
func txCounts(tx pacman.LogTransaction) string {
	var parts []string
	for _, c := range []struct {
		sym string
		act pacman.Action
	}{
		{"+", pacman.ActionInstalled}, {"↑", pacman.ActionUpgraded}, {"↓", pacman.ActionDowngraded},
		{"⟳", pacman.ActionReinstalled}, {"-", pacman.ActionRemoved},
	} {
		if n := tx.Count(c.act); n > 0 {
			parts = append(parts, c.sym+itoa(n))
		}
	}
	return strings.Join(parts, " ")
}

const histDateFmt = "2006-01-02 15:04"

// historyListView renders the left pane of the History screen.
func (m Model) historyListView(w, h int) string {
	hp := m.hist
	switch {
	case !m.cfg.History:
		return "   " + m.styles.Subtle.Render("history is off (history = false)")
	case m.histSec.loading && hp.txs == nil:
		return "   " + m.styles.Subtle.Render(m.spinner.View()+" reading pacman.log…")
	case m.histSec.err != nil:
		return " " + m.styles.Error.Render("✗ "+m.histSec.err.Error())
	}
	loc := m.location()
	pkgMode := m.filterQuery() != ""
	title := "    when              command"
	if pkgMode {
		title = "    when        package"
	}
	lines := []string{m.styles.Section.Render(fitLine(title, w))}
	if len(hp.rows) == 0 {
		msg := "no transactions in pacman.log"
		if pkgMode {
			msg = "no package matches"
		}
		return strings.Join(append(lines, "   "+m.styles.Subtle.Render(msg)), "\n")
	}
	end := min(len(hp.rows), hp.offset+h-1)
	for i := hp.offset; i < end; i++ {
		row := hp.rows[i]
		tx := hp.txs[row.tx]
		cursor := "  "
		if i == hp.sel {
			cursor = "▸ "
		}
		var line string
		if row.change < 0 {
			counts := txCounts(tx)
			if tx.Interrupted {
				counts = "⚠ " + counts
			}
			if partialTx(tx) {
				// Synced the databases without upgrading.
				counts = "~ " + counts
			}
			cmd := tx.Command
			if cmd == "" {
				cmd = "(no command logged)"
			}
			room := max(0, w-2-len(histDateFmt)-2-len(counts)-1)
			line = cursor + tx.Start.In(loc).Format(histDateFmt) + "  " + fitLine(cmd, room) + " " + counts
		} else {
			c := tx.Changes[row.change]
			detail := actionSymbol(c.Action) + " " + versionChange(c)
			nameW := min(24, max(8, (w-2-10-2)/2))
			line = cursor + tx.Start.In(loc).Format("2006-01-02") + "  " + fitLine(c.Name, nameW) + " " + detail
		}
		if i == hp.sel && m.focus == focusList {
			line = m.styles.Bold.Render(fitLine(line, w))
		}
		lines = append(lines, fitLine(line, w))
	}
	return strings.Join(lines, "\n")
}

// actionSymbol is the one-cell mark of an action, as in the list's counts.
func actionSymbol(a pacman.Action) string {
	switch a {
	case pacman.ActionInstalled:
		return "+"
	case pacman.ActionUpgraded:
		return "↑"
	case pacman.ActionDowngraded:
		return "↓"
	case pacman.ActionReinstalled:
		return "⟳"
	}
	return "-"
}

// versionChange is "old → new", or the one version a change has.
func versionChange(c pacman.Change) string {
	switch {
	case c.Old != "" && c.New != "" && c.Old != c.New:
		return c.Old + " → " + c.New
	case c.New != "":
		return c.New
	}
	return c.Old
}

// historyDetail renders the detail pane for the selected row.
func (m Model) historyDetail(row histRow, w int) string {
	tx := m.hist.txs[row.tx]
	loc := m.location()
	var b strings.Builder
	b.WriteString(" " + m.styles.Title.Render(tx.Start.In(loc).Format("2006-01-02 15:04:05")) + "\n")
	if tx.Command != "" {
		b.WriteString(" " + wrapIndent(m.styles.Bold.Render(tx.Command), w-2, 1) + "\n")
	}
	switch {
	case tx.Interrupted:
		b.WriteString(" " + wrapIndent(m.styles.Warn.Render("⚠ interrupted: pacman.log has no \"transaction completed\" for it"), w-2, 3) + "\n")
	case !tx.End.IsZero():
		b.WriteString(" " + m.styles.Subtle.Render("completed after "+tx.End.Sub(tx.Start).Round(time.Second).String()) + "\n")
	}
	if partialTx(tx) {
		b.WriteString(" " + wrapIndent(m.styles.Warn.Render("~ partial upgrade: this refreshed the sync databases without upgrading (-y without -u) and installed something. Packages built against newer libraries than the ones installed can break."), w-2, 3) + "\n")
	}
	if tx.Implicit {
		b.WriteString(" " + m.styles.Subtle.Render("(this log has no \"transaction started\" line for it)") + "\n")
	}
	b.WriteString("\n " + m.styles.Section.Render("Changes ("+itoa(len(tx.Changes))+")") + "\n")
	if len(tx.Changes) == 0 {
		b.WriteString("   " + m.styles.Subtle.Render("no package changes were logged") + "\n")
	}
	q := m.filterQuery()
	anyCached := false
	for _, c := range tx.Changes {
		line := "   " + actionSymbol(c.Action) + " " + c.Name + " " + m.styles.Subtle.Render(versionChange(c))
		if c.Old != "" && m.hist.inCache(c.Name, c.Old) {
			line += " " + m.styles.Bold.Render("●")
			anyCached = true
		}
		if q != "" && strings.Contains(c.Name, q) {
			line = strings.Replace(line, "   ", " ▸ ", 1)
		}
		b.WriteString(wrapIndent(line, w, 5) + "\n")
	}
	b.WriteString("\n " + wrapIndent(m.styles.Subtle.Render("+ installed  ↑ upgraded  ↓ downgraded  ⟳ reinstalled  - removed"), w-2, 1) + "\n")
	if anyCached {
		b.WriteString(" " + wrapIndent(m.styles.Subtle.Render("● the old version is still in the package cache: D can downgrade to it"), w-2, 1) + "\n")
	}
	if m.hist.cacheEr != nil {
		b.WriteString("\n " + m.styles.Warn.Render("⚠ the package cache couldn't be listed: "+m.hist.cacheEr.Error()) + "\n")
	}
	return b.String()
}

// refreshHistoryDetail renders the selected row into the detail pane.
func (m *Model) refreshHistoryDetail() tea.Cmd {
	var content, k string
	switch row, ok := m.hist.selected(); {
	case !m.cfg.History:
		content = " Turn on history in the config to read pacman.log."
	case ok:
		content, k = m.historyDetail(row, m.detail.Width()), row.key()
	case m.histSec.err != nil:
		content = " " + m.styles.Subtle.Render("pacman.log couldn't be read.")
	case m.histSec.loading:
		content = " " + m.styles.Subtle.Render("Reading pacman.log…")
	case m.filterQuery() != "":
		content = " No package matches “" + m.filter.Value() + "”."
	default:
		content = " No transactions in pacman.log."
	}
	m.detail.SetContent(content)
	if k != m.detailKey {
		m.detailKey = k
		m.detail.GotoTop()
	}
	return nil
}

// historyStatus are the status bar parts of the History screen.
func (m Model) historyStatus() []string {
	parts := []string{plural(len(m.hist.txs), "transaction", "transactions")}
	if m.filterQuery() != "" {
		parts = append(parts, plural(len(m.hist.rows), "change", "changes")+" shown")
	}
	return parts
}
