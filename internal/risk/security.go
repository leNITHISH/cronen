package risk

import (
	"fmt"
	"slices"
	"strings"

	"github.com/leNITHISH/cronen/internal/gitlab"
	"github.com/leNITHISH/cronen/internal/security"
)

// securityFixes is rule 8: the advisory groups the update fixes, highest
// severity first. AUR packages never match: the tracker covers the
// official repos only.
func securityFixes(fixes []security.Fix, u Update) []security.Fix {
	if u.AUR || len(fixes) == 0 {
		return nil
	}
	out := slices.Clone(fixes)
	slices.SortStableFunc(out, func(a, b security.Fix) int {
		if c := security.Rank(a.Severity) - security.Rank(b.Severity); c != 0 {
			return c
		}
		return strings.Compare(a.Group, b.Group)
	})
	return out
}

// SecurityReason is the detail pane's line for a fix: "Fixes AVG-2843
// (High): CVE-2025-1234".
func SecurityReason(f security.Fix) string {
	return "Fixes " + f.Describe()
}

// sortedVulnerable is rule 9's list: highest severity first, then by
// package.
func sortedVulnerable(vs []security.Vulnerable) []security.Vulnerable {
	if len(vs) == 0 {
		return nil
	}
	out := slices.Clone(vs)
	slices.SortStableFunc(out, func(a, b security.Vulnerable) int {
		if c := security.Rank(a.Severity) - security.Rank(b.Severity); c != 0 {
			return c
		}
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.Group, b.Group)
	})
	return out
}

// VulnerableReason is "sudo 1.9.17-1 is affected by AVG-2901 (High) and no
// fix is available yet".
func VulnerableReason(v security.Vulnerable) string {
	s := v.Name + " " + v.Installed + " is affected by " + v.Group
	if v.Severity != "" {
		s += " (" + v.Severity + ")"
	}
	return s + " and no fix is available yet"
}

// knownIssuesFlag is rule 10: open issues on the package's GitLab project
// since the new build. It is a warning when one of them carries a critical
// or high severity label, and info otherwise.
func knownIssuesFlag(issues []gitlab.Issue, u Update) (Flag, bool) {
	if len(issues) == 0 || u.AUR {
		return Flag{}, false
	}
	sev := Info
	for _, i := range issues {
		if i.Severe() {
			sev = Warn
		}
	}
	const shown = 2
	var titles []string
	for _, i := range issues[:min(len(issues), shown)] {
		titles = append(titles, "'"+i.Title+"'")
	}
	noun := "issues"
	if len(issues) == 1 {
		noun = "issue"
	}
	reason := fmt.Sprintf("%d open %s since this build: %s", len(issues), noun, strings.Join(titles, ", "))
	if more := len(issues) - shown; more > 0 {
		reason += fmt.Sprintf(" and %d more", more)
	}
	return Flag{Rule: RuleKnownIssues, Severity: sev, Reason: reason}, true
}
