package ui

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/news"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/system"
)

// openedMsg reports how opening a link went.
type openedMsg struct {
	url string
	err error
}

// stateSavedMsg reports how saving the state file went.
type stateSavedMsg struct {
	err error
}

// upgradedMsg reports that the upgrade command exited. A nil err means
// it exited 0.
type upgradedMsg struct {
	err error
}

// upgradeOutcome is what the last upgrade did, for the notice under the
// header.
type upgradeOutcome struct {
	done bool
	err  error
	// reboot is decided when the upgrade starts: once it's done, the
	// kernel is no longer pending.
	reboot bool
}

// flash shows a one-line message in the status bar until the next key.
func (m *Model) flash(msg string, isErr bool) {
	m.flashMsg, m.flashErr = msg, isErr
}

// safeURL returns raw without control characters if it is an http or https
// URL, or "" otherwise. Links come from the network: one starting with "-"
// or using file: must never reach xdg-open.
func safeURL(raw string) string {
	raw = strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, strings.TrimSpace(raw))
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return raw
}

// openLink opens the selected item's upstream (changelog false) or
// changelog link with xdg-open.
func (m *Model) openLink(changelog bool) tea.Cmd {
	what := "upstream link"
	if changelog {
		what = "changelog"
	}
	var raw string
	if m.screen == screenNews {
		if changelog {
			m.flash("news posts have no changelog; o opens the post", false)
			return nil
		}
		if len(m.newsItems) == 0 {
			m.flash("no news item selected", false)
			return nil
		}
		raw, what = m.newsItems[clamp(m.newsPane.sel, 0, len(m.newsItems)-1)].Link, "news post"
	} else {
		e, ok := m.updates.selected()
		if !ok {
			m.flash("no package selected", false)
			return nil
		}
		upstream, cl, pending := m.links(e)
		if pending {
			m.flash("still looking up links on archlinux.org…", false)
			return nil
		}
		raw = upstream
		if changelog {
			raw = cl
		}
	}

	if raw == "" {
		m.flash("no "+what+" known", false)
		return nil
	}
	u := safeURL(raw)
	if u == "" {
		m.flash("not opening "+what+": not an http(s) URL", true)
		return nil
	}
	if m.opts.Demo {
		m.flash("demo: would open "+u, false)
		return nil
	}
	if m.opts.Runner == nil {
		return nil
	}
	m.flash("opening "+u+"…", false)
	return openURL(m.ctx(), m.opts.Runner, u)
}

func openURL(ctx context.Context, r system.Runner, u string) tea.Cmd {
	return func() tea.Msg {
		_, _, err := r.Run(ctx, "xdg-open", u)
		return openedMsg{url: u, err: err}
	}
}

func (m *Model) handleOpened(msg openedMsg) {
	switch {
	case errors.Is(msg.err, system.ErrCommandNotFound):
		m.flash("xdg-open not found — install xdg-utils", true)
	case msg.err != nil:
		m.flash("couldn't open link: "+msg.err.Error(), true)
	case m.flashMsg == "opening "+msg.url+"…":
		// Leave other messages alone; this one is just stale now.
		m.flash("", false)
	}
}

// markRead marks the selected news item read on the news screen, or on the
// updates screen the news behind the selected package's flags.
func (m *Model) markRead() tea.Cmd {
	var guids []string
	if m.screen == screenNews {
		if len(m.newsItems) == 0 {
			m.flash("no news item selected", false)
			return nil
		}
		it := m.newsItems[clamp(m.newsPane.sel, 0, len(m.newsItems)-1)]
		if !m.unread[it.GUID] {
			m.flash("already read", false)
			return nil
		}
		guids = []string{it.GUID}
	} else {
		e, ok := m.updates.selected()
		if ok {
			for _, f := range e.pkg.Flags {
				if f.Rule == risk.RuleNews && f.NewsGUID != "" {
					guids = append(guids, f.NewsGUID)
				}
			}
		}
		if len(guids) == 0 {
			m.flash("no unread news about this package", false)
			return nil
		}
	}
	m.opts.State = m.opts.State.Clone()
	m.opts.State.MarkRead(guids...)
	m.flash(plural(len(guids), "news item", "news items")+" marked read", false)
	return tea.Batch(m.changed(), m.saveState())
}

// saveState writes the state file in a command, from a snapshot so later
// changes can't race with the write. With no state path, nothing is saved:
// in demo mode silently, otherwise with a warning.
func (m *Model) saveState() tea.Cmd {
	if m.opts.StatePath == "" || m.opts.FS == nil {
		if !m.opts.Demo {
			m.flash("state not saved: no state file location", true)
		}
		return nil
	}
	fsys, path, st := m.opts.FS, m.opts.StatePath, m.opts.State.Clone()
	return func() tea.Msg {
		return stateSavedMsg{err: config.SaveState(fsys, path, st)}
	}
}

// startFilter focuses the filter input on the updates screen.
func (m *Model) startFilter() tea.Cmd {
	m.screen, m.focus = screenUpdates, focusList
	m.filtering = true
	return m.filter.Focus()
}

// handleFilterKey sends keys to the filter input while it has focus, so
// typing "q" or "r" filters instead of quitting or refreshing.
func (m Model) handleFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.String() == "ctrl+c":
		return m, tea.Quit
	case key.Matches(msg, m.keys.Accept):
		m.filtering = false
		m.filter.Blur()
		return m, m.layout()
	case key.Matches(msg, m.keys.Back):
		m.clearFilter()
		return m, m.layout()
	}
	before := m.filter.Value()
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	if m.filter.Value() != before {
		// A new filter starts the selection over at the top.
		m.updates.moved = false
		return m, tea.Batch(cmd, m.layout())
	}
	return m, cmd
}

func (m *Model) clearFilter() {
	m.filtering = false
	m.filter.Blur()
	m.filter.Reset()
	m.updates.moved = false
}

// filterQuery is the lower-cased filter text, or "" if there is none.
func (m Model) filterQuery() string {
	return strings.ToLower(strings.TrimSpace(m.filter.Value()))
}

// requestUpgrade opens the confirm modal.
func (m *Model) requestUpgrade() {
	m.screen, m.focus = screenConfirm, focusList
}

// startUpgrade hands the terminal to the upgrade command.
func (m *Model) startUpgrade() tea.Cmd {
	m.screen = screenUpdates
	argv := m.cfg.UpgradeArgv()
	if m.opts.Exec == nil || len(argv) == 0 {
		m.flash("upgrading isn't available here", true)
		return nil
	}
	m.upgrade = upgradeOutcome{reboot: m.result.RebootRequired}
	return m.opts.Exec(argv, func(err error) tea.Msg { return upgradedMsg{err: err} })
}

// handleUpgraded records a finished upgrade and reloads everything. Only a
// successful one counts as an upgrade: it sets the last upgrade time and
// marks the news published until now read.
func (m *Model) handleUpgraded(msg upgradedMsg) tea.Cmd {
	m.upgrade.done, m.upgrade.err = true, msg.err
	if msg.err != nil {
		return m.refresh()
	}
	now := m.now()
	m.opts.State = m.opts.State.Clone()
	m.opts.State.LastUpgrade = now
	m.opts.State.MarkRead(publishedBy(m.newsItems, now)...)
	return tea.Batch(m.saveState(), m.refresh())
}

// publishedBy returns the GUIDs of the items published no later than t.
func publishedBy(items []news.Item, t time.Time) []string {
	var guids []string
	for _, it := range items {
		if !it.Published.After(t) {
			guids = append(guids, it.GUID)
		}
	}
	return guids
}

// noticeText is the line under the header after an upgrade, or "".
func (m Model) noticeText() string {
	u := m.upgrade
	switch {
	case !u.done:
		return ""
	case u.err != nil:
		return "✗ upgrade failed (" + u.err.Error() + "); news not marked read"
	}
	parts := []string{"✓ upgrade finished"}
	if u.reboot {
		parts = append(parts, "reboot to run the new kernel")
	}
	switch n := len(m.pacnew); {
	case m.pacnewSec.loading:
		parts = append(parts, "checking for .pacnew files…")
	case n > 0:
		parts = append(parts, plural(n, ".pacnew file", ".pacnew files")+" to merge (run pacdiff)")
	}
	return strings.Join(parts, " · ")
}
