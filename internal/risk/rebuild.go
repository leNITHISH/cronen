package risk

import (
	"fmt"
	"slices"
	"strings"

	"github.com/leNITHISH/cronen/internal/rebuild"
)

// rebuildFlags is rule 7: installed foreign packages that will need a
// rebuild after u is upgraded. Packages caught the same way for the same
// library or directory share a flag, and each way of catching one gets its
// own, so "depends on" and "links against" both show. Module directories of
// python are left to the Python bump flag, which carries the list.
func rebuildFlags(u Update, findings []rebuild.Finding) []Flag {
	type key struct {
		signal rebuild.Signal
		detail string
	}
	groups := map[key][]string{}
	var keys []key
	for _, f := range findings {
		if f.Culprit != u.Name || (f.Signal == rebuild.SignalModuleDir && u.Name == "python") {
			continue
		}
		k := key{f.Signal, f.Detail}
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		if !slices.Contains(groups[k], f.Package) {
			groups[k] = append(groups[k], f.Package)
		}
	}
	slices.SortFunc(keys, func(a, b key) int {
		if a.signal != b.signal {
			return int(a.signal) - int(b.signal)
		}
		return strings.Compare(a.detail, b.detail)
	})

	// Versions without epoch and pkgrel, unless that would make them equal.
	oldV, newV := upstreamVersion(u.OldVersion), upstreamVersion(u.NewVersion)
	if oldV == newV {
		oldV, newV = u.OldVersion, u.NewVersion
	}

	var flags []Flag
	for _, k := range keys {
		pkgs := groups[k]
		slices.Sort(pkgs)
		verb := ""
		switch k.signal {
		case rebuild.SignalMetadata:
			verb = pick(len(pkgs), "depends on", "depend on")
		case rebuild.SignalELF:
			verb = pick(len(pkgs), "links against", "link against")
		case rebuild.SignalModuleDir:
			verb = pick(len(pkgs), "installs into", "install into")
		}
		flags = append(flags, Flag{
			Rule: RuleRebuild, Severity: Warn,
			Reason: fmt.Sprintf("Needs rebuild: %s %s %s (%s %s → %s)",
				strings.Join(pkgs, ", "), verb, k.detail, u.Name, oldV, newV),
		})
	}
	return flags
}

func pick(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// pythonDependents are the foreign packages to name in the Python bump
// warning: the ones installed under the old python's directory, and any the
// caller listed.
func pythonDependents(in Input) []string {
	names := slices.Clone(in.PythonDependents)
	for _, f := range in.Rebuilds {
		if f.Culprit == "python" && f.Signal == rebuild.SignalModuleDir && !slices.Contains(names, f.Package) {
			names = append(names, f.Package)
		}
	}
	slices.Sort(names)
	return names
}
