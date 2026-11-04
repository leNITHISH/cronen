package risk

import "slices"

// Acknowledgeable reports whether the user may acknowledge f. Critical
// flags, such as manual-intervention news, can't be: the user reads the
// news or fixes the problem instead.
func Acknowledgeable(f Flag) bool {
	return f.Severity != Critical
}

// AcknowledgeableRules returns the IDs of the rules of p's acknowledgeable
// flags, sorted and without duplicates. It is what `a` records.
func AcknowledgeableRules(p Package) []string {
	var ids []string
	for _, f := range p.Flags {
		if Acknowledgeable(f) {
			ids = append(ids, f.Rule.String())
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// Acknowledged looks up the rule IDs acknowledged for exactly this version
// of a package. See config.State.Acknowledged.
type Acknowledged func(name, version string) []string

// Acknowledge moves the flags the user acknowledged from Flags to
// Acknowledged. A flag moves when its rule was acknowledged for the
// package's exact new version and it isn't critical, so a new version, a
// rule that wasn't acknowledged and every critical flag stay flagged. res
// is not modified.
func Acknowledge(res Result, acked Acknowledged) Result {
	out := res
	out.Packages = slices.Clone(res.Packages)
	for i, p := range out.Packages {
		rules := acked(p.Name, p.NewVersion)
		if len(rules) == 0 {
			continue
		}
		var keep, moved []Flag
		for _, f := range p.Flags {
			if Acknowledgeable(f) && slices.Contains(rules, f.Rule.String()) {
				moved = append(moved, f)
			} else {
				keep = append(keep, f)
			}
		}
		out.Packages[i].Flags = keep
		out.Packages[i].Acknowledged = moved
	}
	return out
}

// AcknowledgedPackages returns the packages with acknowledged flags and
// none left that are flagged: the ones the acknowledgement took out of
// NEEDS ATTENTION. They are in input order.
func (r Result) AcknowledgedPackages() []Package {
	var out []Package
	for _, p := range r.Packages {
		if len(p.Acknowledged) > 0 && len(p.Flags) == 0 {
			out = append(out, p)
		}
	}
	return out
}
