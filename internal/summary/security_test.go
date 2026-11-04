package summary

import (
	"context"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/system"
)

// These tests run on HAND-WRITTEN fixtures, pending capture from the real
// services (see internal/fixtures/testdata/HAND_WRITTEN.txt).

func find(t *testing.T, ps []risk.Package, name string) risk.Package {
	t.Helper()
	for _, p := range ps {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("no package %s in %v", name, ps)
	return risk.Package{}
}

func TestGatherSecurityAndIssues(t *testing.T) {
	s := Gather(context.Background(), fixtureSources())
	if len(s.Errors) > 0 {
		t.Fatalf("errors: %v", s.Errors)
	}

	// A pending update shows "fixes AVG-…".
	var names []string
	for _, p := range s.Security {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "firefox,openssh" {
		t.Errorf("security fixes for %v", names)
	}
	if got := find(t, s.Security, "openssh").Security[0].Describe(); got != "AVG-2901 (High): CVE-2025-60001" {
		t.Errorf("openssh fix = %q", got)
	}
	if s.SecurityFixes() != 2 {
		t.Errorf("security fixes = %d", s.SecurityFixes())
	}
	// Fixes are a reason to upgrade, not a flag: firefox has none.
	for _, p := range s.Flagged {
		if p.Name == "firefox" || p.Name == "openssh" {
			t.Errorf("%s is flagged: %+v", p.Name, p.Flags)
		}
	}

	// An installed package with no fix. vim isn't affected yet; python's
	// group is "Not affected"; glibc's fix is installed.
	if len(s.Vulnerable) != 1 || s.Vulnerable[0].Name != "sudo" || s.Vulnerable[0].Group != "AVG-2930" {
		t.Errorf("vulnerable = %+v", s.Vulnerable)
	}

	// A repo update shows its open issues; linux has a high-severity one.
	linux := find(t, s.Flagged, "linux")
	var issues risk.Flag
	for _, f := range linux.Flags {
		if f.Rule == risk.RuleKnownIssues {
			issues = f
		}
	}
	if issues.Severity != risk.Warn || !strings.HasPrefix(issues.Reason, "2 open issues since this build: 'Crash on startup with Wayland'") {
		t.Errorf("linux known issues = %+v", issues)
	}
	mesa := find(t, s.Flagged, "mesa")
	if mesa.Flags[len(mesa.Flags)-1].Rule != risk.RuleKnownIssues || mesa.Flags[len(mesa.Flags)-1].Severity != risk.Info {
		t.Errorf("mesa flags = %+v", mesa.Flags)
	}
}

func TestSecurityOutput(t *testing.T) {
	s := Gather(context.Background(), fixtureSources())
	text := s.Text()
	for _, want := range []string{
		"🛡 openssh: fixes AVG-2901 (High): CVE-2025-60001",
		"🛡 firefox: fixes AVG-2915 (Critical): CVE-2025-60002, CVE-2025-60003",
		"⚠ sudo 1.9.17.p2-1 is affected by AVG-2930 (High) and no fix is available yet",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("--check output lacks %q:\n%s", want, text)
		}
	}
	// Ordered by severity: the unfixed vulnerability (warn) comes before
	// the security fixes (info).
	if strings.Index(text, "sudo") > strings.Index(text, "🛡") {
		t.Errorf("warn lines must precede info lines:\n%s", text)
	}
	if !strings.Contains(string(s.JSON()), `"security_fixes":2,"vulnerable":1`) {
		t.Errorf("json = %s", s.JSON())
	}
}

func TestSecurityDoesNotChangeExitCodes(t *testing.T) {
	src := fixtureSources()
	src.State.MarkRead(firmwareGUID, nvidiaGUID)
	r := fixtures.Runner()
	r.Set("checkupdates", system.FakeResult{Stdout: []byte("openssh 10.0p1-3 -> 10.2p1-1\n")})
	src.Runner = r
	src.Config.AUR = false
	// openssh has a security fix and nothing else: not flagged.
	src.Config.KnownIssues = false
	s := Gather(context.Background(), src)
	if len(s.Security) != 1 || len(s.Flagged) != 0 {
		t.Fatalf("security %d, flagged %d", len(s.Security), len(s.Flagged))
	}
	// The vulnerable sudo is global but doesn't flag a package either.
	if got := s.ExitCode(); got != ExitUpdates {
		t.Errorf("exit code %d, want %d", got, ExitUpdates)
	}
}

func TestFeedsOffMakeNoRequests(t *testing.T) {
	src := fixtureSources()
	src.Config.Security = false
	src.Config.KnownIssues = false
	f, r := fixtures.Fetcher(), fixtures.Runner()
	src.Fetcher, src.Runner = f, r
	s := Gather(context.Background(), src)
	if len(s.Errors) > 0 {
		t.Fatal(s.Errors)
	}
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
	if len(s.Security) != 0 || len(s.Vulnerable) != 0 {
		t.Errorf("security data with the feed off: %v %v", s.Security, s.Vulnerable)
	}
	for _, p := range s.Flagged {
		for _, fl := range p.Flags {
			if fl.Rule == risk.RuleKnownIssues {
				t.Errorf("known-issues flag with the feed off: %+v", fl)
			}
		}
	}
}

func TestEachFeedOnItsOwn(t *testing.T) {
	count := func(f *system.FakeFetcher, host string) int {
		n := 0
		for _, c := range f.Calls() {
			if strings.Contains(c.URL, host) {
				n++
			}
		}
		return n
	}
	src := fixtureSources()
	src.Config.KnownIssues = false
	f := fixtures.Fetcher()
	src.Fetcher = f
	Gather(context.Background(), src)
	if count(f, "security.archlinux.org") != 1 || count(f, "gitlab.archlinux.org/api") != 0 {
		t.Errorf("known_issues off: %+v", f.Calls())
	}

	src = fixtureSources()
	src.Config.Security = false
	f = fixtures.Fetcher()
	src.Fetcher = f
	Gather(context.Background(), src)
	// Only repo updates with a build date are looked up: 16 of them.
	if count(f, "security.archlinux.org") != 0 || count(f, "gitlab.archlinux.org/api") != 16 {
		t.Errorf("security off: security %d, gitlab %d", count(f, "security.archlinux.org"), count(f, "gitlab.archlinux.org/api"))
	}
}

func TestIssuesNeedThePreview(t *testing.T) {
	src := fixtureSources()
	src.Config.Preview = false
	f := fixtures.Fetcher()
	src.Fetcher = f
	Gather(context.Background(), src)
	for _, c := range f.Calls() {
		if strings.Contains(c.URL, "gitlab.archlinux.org") {
			t.Errorf("GitLab asked without build dates: %s", c.URL)
		}
	}
}

func TestFeedFailures(t *testing.T) {
	src := fixtureSources()
	f := fixtures.Fetcher()
	f.Set(fixtures.SecurityURL, system.FakeResponse{Status: 500})
	src.Fetcher = f
	s := Gather(context.Background(), src)
	if s.ExitCode() != ExitError {
		t.Errorf("exit code %d, want %d for a failed source", s.ExitCode(), ExitError)
	}
	found := false
	for _, err := range s.Errors {
		found = found || strings.Contains(err.Error(), "security tracker")
	}
	if !found {
		t.Errorf("errors = %v", s.Errors)
	}
	// The rest still works: known issues came through.
	linux := find(t, s.Flagged, "linux")
	ok := false
	for _, fl := range linux.Flags {
		ok = ok || fl.Rule == risk.RuleKnownIssues
	}
	if !ok {
		t.Errorf("known issues lost when the tracker failed: %+v", linux.Flags)
	}
}
