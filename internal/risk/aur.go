package risk

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// The AUR review rules (12 and 13). Their values are far from the base
// rules' so they stay stable as other rules are added.
const (
	// RuleMaintainer is rule 12: the AUR maintainer changed.
	RuleMaintainer Rule = 120 + iota
	// RuleSuspicious is rule 13: a suspicious PKGBUILD change.
	RuleSuspicious
)

func aurRuleName(r Rule) (string, bool) {
	switch r {
	case RuleMaintainer:
		return "aur-maintainer", true
	case RuleSuspicious:
		return "aur-suspicious", true
	}
	return "", false
}

// Maintainers is who looks after an AUR package base.
type Maintainers struct {
	// Maintainer is empty for an orphaned package.
	Maintainer string
	// Co are the co-maintainers, in any order.
	Co []string
}

// MaintainerCheck is the recorded and the current maintainers of one AUR
// package.
type MaintainerCheck struct {
	// Recorded is what state holds, or nil if nothing is recorded yet.
	Recorded *Maintainers
	Current  Maintainers
}

// AURInput is what the AUR review rules look at.
type AURInput struct {
	// Maintainers is keyed by package name. A package without an entry is
	// never flagged.
	Maintainers map[string]MaintainerCheck
	// DiffReasons is keyed by package name and holds one reason per kind
	// of suspicious change found in the loaded PKGBUILD diff. A package
	// whose diff isn't loaded has no entry.
	DiffReasons map[string][]string
}

// AddAUR raises rules 12 and 13 on the AUR updates in res and returns the
// result. res is not modified.
func AddAUR(res Result, in AURInput) Result {
	out := res
	out.Packages = slices.Clone(res.Packages)
	for i, p := range out.Packages {
		if !p.AUR {
			continue
		}
		var add []Flag
		if chk, ok := in.Maintainers[p.Name]; ok {
			for _, reason := range MaintainerReasons(chk) {
				add = append(add, Flag{Rule: RuleMaintainer, Severity: Warn, Reason: reason})
			}
		}
		for _, reason := range dedupe(in.DiffReasons[p.Name]) {
			add = append(add, Flag{Rule: RuleSuspicious, Severity: Warn, Reason: "Suspicious PKGBUILD change: " + reason})
		}
		if len(add) == 0 {
			continue
		}
		flags := append(slices.Clone(p.Flags), add...)
		slices.SortStableFunc(flags, func(a, b Flag) int {
			return cmp.Or(cmp.Compare(b.Severity, a.Severity), cmp.Compare(a.Rule, b.Rule))
		})
		out.Packages[i].Flags = flags
	}
	return out
}

func dedupe(s []string) []string {
	var out []string
	for _, v := range s {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// MaintainerReasons explains how the maintainers differ from the recorded
// ones, one reason per difference. With nothing recorded, or no change, it
// returns nil: the first maintainers seen are the baseline.
func MaintainerReasons(c MaintainerCheck) []string {
	rec := c.Recorded
	if rec == nil {
		return nil
	}
	var reasons []string
	switch {
	case rec.Maintainer == c.Current.Maintainer:
	case rec.Maintainer == "":
		reasons = append(reasons, fmt.Sprintf(
			"AUR package was orphaned and has been adopted by %s: adopting an orphan is a common way to take over a package",
			c.Current.Maintainer))
	case c.Current.Maintainer == "":
		reasons = append(reasons, fmt.Sprintf("AUR package was orphaned by its maintainer %s", rec.Maintainer))
	default:
		reasons = append(reasons, fmt.Sprintf("AUR maintainer changed: %s → %s", rec.Maintainer, c.Current.Maintainer))
	}

	before, after := sortedSet(rec.Co), sortedSet(c.Current.Co)
	if !slices.Equal(before, after) {
		var parts []string
		for _, n := range after {
			if !slices.Contains(before, n) {
				parts = append(parts, "+"+n)
			}
		}
		for _, n := range before {
			if !slices.Contains(after, n) {
				parts = append(parts, "-"+n)
			}
		}
		reasons = append(reasons, "AUR co-maintainers changed: "+strings.Join(parts, " "))
	}
	return reasons
}

func sortedSet(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return slices.Compact(out)
}
