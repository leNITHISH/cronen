package risk

import (
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/gitlab"
	"github.com/leNITHISH/cronen/internal/security"
)

func TestSecurityFixesRule(t *testing.T) {
	u := Update{Name: "openssh", OldVersion: "10.0p1-3", NewVersion: "10.2p1-1"}
	low := security.Fix{Group: "AVG-1", Severity: "Low"}
	high := security.Fix{Group: "AVG-3", Severity: "High", CVEs: []string{"CVE-2025-1"}}
	crit := security.Fix{Group: "AVG-2", Severity: "Critical"}

	p := assessOne(u, Input{Fixes: map[string][]security.Fix{"openssh": {low, high, crit}}})
	var got []string
	for _, f := range p.Security {
		got = append(got, f.Group)
	}
	if strings.Join(got, ",") != "AVG-2,AVG-3,AVG-1" {
		t.Errorf("fixes = %v, want highest severity first", got)
	}
	// A reason to upgrade is not a flag.
	if len(p.Flags) != 0 || p.Severity() != None {
		t.Errorf("flags = %v, security fixes must not flag", summary(p))
	}
	res := Assess(Input{Updates: []Update{u}, Now: now, Fixes: map[string][]security.Fix{"openssh": {high}}})
	if len(res.Flagged()) != 0 || res.Count(None) != 1 {
		t.Errorf("flagged = %v", res.Flagged())
	}
	if got := SecurityReason(high); got != "Fixes AVG-3 (High): CVE-2025-1" {
		t.Errorf("reason = %q", got)
	}

	// Fixes of another package, and AUR packages, are ignored.
	if p := assessOne(u, Input{Fixes: map[string][]security.Fix{"openssl": {high}}}); len(p.Security) != 0 {
		t.Errorf("other package's fixes attached: %v", p.Security)
	}
	aur := u
	aur.AUR = true
	if p := assessOne(aur, Input{Fixes: map[string][]security.Fix{"openssh": {high}}}); len(p.Security) != 0 {
		t.Errorf("AUR package got fixes: %v", p.Security)
	}
}

func TestVulnerableRule(t *testing.T) {
	vs := []security.Vulnerable{
		{Name: "zlib", Installed: "1-1", Group: "AVG-5", Severity: "Low"},
		{Name: "sudo", Installed: "1.9-1", Group: "AVG-9", Severity: "High"},
		{Name: "bash", Installed: "5-1", Group: "AVG-7", Severity: "High"},
	}
	res := Assess(Input{Now: now, Vulnerable: vs})
	var names []string
	for _, v := range res.Vulnerable {
		names = append(names, v.Name)
	}
	if strings.Join(names, ",") != "bash,sudo,zlib" {
		t.Errorf("vulnerable = %v, want severity then name", names)
	}
	if len(res.Packages) != 0 || len(res.Flagged()) != 0 {
		t.Error("vulnerable packages are global, never a package flag")
	}
	want := "sudo 1.9-1 is affected by AVG-9 (High) and no fix is available yet"
	if got := VulnerableReason(vs[1]); got != want {
		t.Errorf("reason = %q", got)
	}
}

func TestKnownIssuesRule(t *testing.T) {
	u := Update{Name: "linux", OldVersion: "6.17.4.arch1-1", NewVersion: "6.18.1.arch1-1"}
	plain := gitlab.Issue{Title: "Crash on startup with Wayland"}
	severe := gitlab.Issue{Title: "No boot", Labels: []string{"severity::1-critical"}}

	tests := []struct {
		name   string
		issues []gitlab.Issue
		upd    Update
		want   string
		reason string
	}{
		{"none", nil, u, "", ""},
		{"one plain issue", []gitlab.Issue{plain}, u, "known-issues/info",
			"1 open issue since this build: 'Crash on startup with Wayland'"},
		{"a severe label makes it a warning", []gitlab.Issue{plain, severe}, u, "known-issues/warn",
			"2 open issues since this build: 'Crash on startup with Wayland', 'No boot'"},
		{"long lists are cut", []gitlab.Issue{plain, plain, plain, plain}, u, "known-issues/info",
			"4 open issues since this build: 'Crash on startup with Wayland', 'Crash on startup with Wayland' and 2 more"},
		{"not for AUR packages", []gitlab.Issue{plain}, Update{Name: "linux", AUR: true}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := assessOne(tt.upd, Input{Issues: map[string][]gitlab.Issue{"linux": tt.issues}})
			var kf []Flag
			for _, f := range p.Flags {
				if f.Rule == RuleKnownIssues {
					kf = append(kf, f)
				}
			}
			if tt.want == "" {
				if len(kf) != 0 {
					t.Fatalf("unexpected flag %v", kf)
				}
				return
			}
			if len(kf) != 1 || kf[0].Rule.String()+"/"+kf[0].Severity.String() != tt.want || kf[0].Reason != tt.reason {
				t.Errorf("flags = %+v, want %s %q", kf, tt.want, tt.reason)
			}
		})
	}
}
