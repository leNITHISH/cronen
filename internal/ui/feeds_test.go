package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/system"
)

// These tests run on HAND-WRITTEN fixtures, pending capture from the real
// services (see internal/fixtures/testdata/HAND_WRITTEN.txt).

func TestSecurityInTheUI(t *testing.T) {
	const w, h = 100, 48
	m := loaded(t, fixtureOptions(), w, h)

	// The shield marks the update in the list, and the detail pane lists
	// the fix first.
	list := plain(m)
	for _, name := range []string{"firefox", "openssh"} {
		if !strings.Contains(list, name) {
			t.Fatalf("no %s in the list", name)
		}
	}
	m = selectEntry(t, m, "repo:openssh")
	out := plain(m)
	if !strings.Contains(out, "🛡 Fixes AVG-2901 (High): CVE-2025-60001") {
		t.Errorf("detail lacks the fix:\n%s", out)
	}
	if !strings.Contains(out, "openssh") || !strings.Contains(out, "10.2p1 🛡") {
		t.Errorf("list lacks the shield badge:\n%s", out)
	}
	if strings.Contains(out, "Why flagged") {
		t.Errorf("a security fix alone must not flag the package:\n%s", out)
	}
	golden(t, "security-fix", out)
	checkSize(t, m, w, h)

	// Installed and vulnerable with no fix is global, in the header.
	if !strings.Contains(strings.SplitN(out, "\n", 2)[0], "1 vulnerable, no fix") {
		t.Errorf("header lacks the vulnerable count:\n%s", out)
	}
}

func TestKnownIssuesInTheUI(t *testing.T) {
	const w, h = 100, 48
	m := loaded(t, fixtureOptions(), w, h)
	m = selectEntry(t, m, "repo:linux")
	out := plain(m)
	if !strings.Contains(out, "2 open issues since this build: 'Crash on startup") {
		t.Errorf("detail lacks the issues:\n%s", out)
	}
	golden(t, "known-issues", out)
	checkSize(t, m, w, h)
}

func TestHelpMentionsTrackerLag(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 30)
	m = send(t, m, press("?"))
	if !strings.Contains(plain(m), helpNote) {
		t.Errorf("help lacks the note about the tracker:\n%s", plain(m))
	}
}

func TestFeedsOffMakeNoRequests(t *testing.T) {
	opts := fixtureOptions()
	f, r := fixtures.Fetcher(), fixtures.Runner()
	opts.Fetcher, opts.Runner = f, r
	cfg := config.Default()
	cfg.Security, cfg.KnownIssues = false, false
	opts.Config = &cfg

	m := loaded(t, opts, 100, 30)
	m = send(t, m, press("r"))
	for _, c := range f.Calls() {
		if strings.Contains(c.URL, "security.archlinux.org") || strings.Contains(c.URL, "gitlab.archlinux.org") {
			t.Errorf("request with the feeds off: %s", c.URL)
		}
	}
	for _, c := range r.Calls() {
		if c == "pacman -Qn" {
			t.Errorf("security command with the feeds off: %s", c)
		}
	}
	if m.feedsLoading() || m.anyLoading() {
		t.Error("feeds that are off must not keep the spinner going")
	}
	if strings.Contains(plain(m), "🛡") || strings.Contains(plain(m), "vulnerable") {
		t.Errorf("security data with the feed off:\n%s", plain(m))
	}
}

func TestFeedFailureIsInline(t *testing.T) {
	opts := fixtureOptions()
	f := fixtures.Fetcher()
	f.Set(fixtures.SecurityURL, system.FakeResponse{Status: 503})
	opts.Fetcher = f
	m := loaded(t, opts, 100, 48)

	out := plain(m)
	if !strings.Contains(out, "security unavailable") || !strings.Contains(out, "SECURITY") {
		t.Errorf("the failure isn't shown:\n%s", out)
	}
	// Everything else still loaded, including the known issues.
	if len(m.result.Packages) != 18 || !strings.Contains(out, "NEEDS ATTENTION (12)") {
		t.Errorf("a failed feed blocked the rest:\n%s", out)
	}
	if m.anyLoading() {
		t.Error("still loading")
	}
}

func TestStaleFeedResultsAreDropped(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 30)
	before := len(m.feeds.Security.Fixes)
	m = send(t, m, securityMsg{gen: m.gen - 1, err: errors.New("stale")})
	m = send(t, m, issuesMsg{gen: m.gen - 1, err: errors.New("stale")})
	if m.secSec.err != nil || m.issuesSec.err != nil || len(m.feeds.Security.Fixes) != before {
		t.Error("a stale result was applied")
	}
}
