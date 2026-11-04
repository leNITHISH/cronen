package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/gitlab"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/summary"
	"github.com/leNITHISH/cronen/internal/system"
)

// The security tracker and GitLab feeds depend on the pending updates (and,
// for GitLab, on their build dates from the preview), so they start when
// those arrive rather than with the other sources.

type securityMsg struct {
	gen  int
	data summary.SecurityData
	err  error
}

type issuesMsg struct {
	gen    int
	issues map[string][]gitlab.Issue
	err    error
}

func loadSecurity(ctx context.Context, r system.Runner, f system.Fetcher, updates []risk.Update, gen int, fresh bool) tea.Cmd {
	return func() tea.Msg {
		data, err := summary.LoadSecurity(ctx, r, f, updates, fresh)
		return securityMsg{gen: gen, data: data, err: err}
	}
}

func loadIssues(ctx context.Context, f system.Fetcher, updates []risk.Update, tx pacman.Transaction, gen int, fresh bool) tea.Cmd {
	return func() tea.Msg {
		issues, err := summary.LoadIssues(ctx, f, updates, tx, fresh)
		return issuesMsg{gen: gen, issues: issues, err: err}
	}
}

// startSecurity loads the security tracker once the repo and AUR updates
// are in. Without the repo updates there is nothing to look at.
func (m *Model) startSecurity() tea.Cmd {
	if !m.cfg.Security || m.secStarted || m.repoSec.loading || m.aurSec.loading {
		return nil
	}
	m.secStarted = true
	if m.repoSec.err != nil {
		m.secSec = section{}
		return nil
	}
	return loadSecurity(m.ctx(), m.opts.Runner, m.opts.Fetcher, summary.Updates(m.repo, m.aurPkgs), m.gen, m.freshLoad)
}

// startIssues looks up the GitLab issues once the preview has the build
// dates. The preview failing or being off means there is nothing to look
// up.
func (m *Model) startIssues() tea.Cmd {
	if !m.cfg.KnownIssues {
		return nil
	}
	if !m.previewed {
		m.issuesSec = section{}
		return nil
	}
	return loadIssues(m.ctx(), m.opts.Fetcher, summary.Updates(m.repo, nil), m.tx, m.gen, m.freshLoad)
}

// feedsLoading reports whether the security or GitLab data is on its way.
func (m Model) feedsLoading() bool {
	return m.secSec.loading || m.issuesSec.loading
}

// handleFeeds handles the two messages above. ok is false for other
// messages.
func (m *Model) handleFeeds(msg tea.Msg) (cmd tea.Cmd, ok bool) {
	switch msg := msg.(type) {
	case securityMsg:
		if msg.gen != m.gen {
			return nil, true
		}
		m.secSec = section{err: msg.err}
		m.feeds.Security = msg.data
		return m.changed(), true
	case issuesMsg:
		if msg.gen != m.gen {
			return nil, true
		}
		m.issuesSec = section{err: msg.err}
		m.feeds.Issues = msg.issues
		return m.changed(), true
	}
	return nil, false
}

// addFeedErrorRows lists the feeds that failed, each in a section of its
// own, like the transaction preview.
func (m *Model) addFeedErrorRows(p *updatesPane, note func(string, bool, bool)) {
	for _, f := range []struct {
		title string
		sec   section
	}{{"SECURITY", m.secSec}, {"KNOWN ISSUES", m.issuesSec}} {
		if f.sec.err != nil && !f.sec.loading {
			p.rows = append(p.rows, row{kind: rowSection, text: f.title})
			note("✗ "+f.sec.err.Error(), false, true)
		}
	}
}

// securityLines are the detail pane's bullets for an entry's security
// fixes, highest severity first.
func securityLines(p risk.Package) []string {
	var lines []string
	for _, f := range p.Security {
		lines = append(lines, "🛡 "+risk.SecurityReason(f))
	}
	return lines
}

// helpNote is shown under the keys on the help screen.
const helpNote = "Security data comes from security.archlinux.org, which can lag: a hint, not a guarantee."
