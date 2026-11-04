package fixtures

import (
	"fmt"
	"slices"
	"strings"

	"github.com/leNITHISH/cronen/internal/system"
)

// The M14 fixtures are HAND-WRITTEN, pending capture (see HAND_WRITTEN.txt).
const (
	PacmanRp       = "pacman_Rp.txt"             // pacman -Rsp --print-format '%n %v' -- spotify
	PacmanRpBreaks = "pacman_Rp_breaks.stderr"   // the stderr of a removal that breaks dependencies
	DiffMirrorlist = "diff_u_mirrorlist.txt"     // diff -u of the mirrorlist and its .pacnew
	SystemctlFail  = "systemctl_failed.txt"      // systemctl --failed --plain --no-legend
	SystemctlUser  = "systemctl_failed_user.txt" // the same with --user
	PacmanQii      = "pacman_Qii.txt"            // pacman -Qii, the packages that have backup files
)

type qiBlock struct {
	name, version string
	explicit      bool
	depends       []string
	requiredBy    []string
}

// qiBlocks parses the fixture pacman -Qi into just what a removal dry run
// needs. It stays independent of internal/pacman, which tests this package.
func qiBlocks() map[string]qiBlock {
	out := map[string]qiBlock{}
	for _, block := range strings.Split(string(Bytes(PacmanQiAll)), "\n\n") {
		var b qiBlock
		for l := range strings.Lines(block) {
			k, v, ok := strings.Cut(strings.TrimRight(l, "\n"), ":")
			if !ok {
				continue
			}
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			switch k {
			case "Name":
				b.name = v
			case "Version":
				b.version = v
			case "Install Reason":
				b.explicit = strings.HasPrefix(v, "Explicitly")
			case "Depends On":
				b.depends = depNames(v)
			case "Required By":
				b.requiredBy = depNames(v)
			}
		}
		if b.name != "" {
			out[b.name] = b
		}
	}
	return out
}

func depNames(v string) []string {
	var out []string
	for _, f := range strings.Fields(v) {
		if f == "None" {
			continue
		}
		if i := strings.IndexAny(f, "<>="); i > 0 {
			f = f[:i]
		}
		out = append(out, f)
	}
	return out
}

// qiiHandler answers `pacman -Qii -- names...` from the hand-written
// pacman_Qii.txt; a package it holds no block for has no backup files.
func qiiHandler(line string) (system.FakeResult, bool) {
	rest, ok := strings.CutPrefix(line, "pacman -Qii -- ")
	if !ok {
		return system.FakeResult{}, false
	}
	have := map[string]string{}
	for _, block := range strings.Split(string(Bytes(PacmanQii)), "\n\n") {
		for l := range strings.Lines(block) {
			if v, ok := strings.CutPrefix(l, "Name"); ok {
				_, v, _ = strings.Cut(v, ":")
				have[strings.TrimSpace(v)] = strings.TrimRight(block, "\n") + "\n"
			}
		}
	}
	blocks := qiBlocks()
	var out []string
	for _, n := range strings.Fields(rest) {
		b, ok := blocks[n]
		if !ok {
			return system.FakeResult{Stderr: []byte("error: package '" + n + "' was not found\n"), Code: 1}, true
		}
		if h, ok := have[n]; ok {
			out = append(out, h)
		} else {
			out = append(out, "Name            : "+n+"\nVersion         : "+b.version+"\nBackup Files    : (none)\n")
		}
	}
	return system.FakeResult{Stdout: []byte(strings.Join(out, "\n"))}, true
}

// removeHandler answers the dry run `pacman -R<s>p --print-format %n %v
// -- names...` the way pacman does for the fixture packages: unknown names
// are "target not found", a package other installed packages need makes it
// fail with the breaks-dependency error, and with -s the dependencies
// nothing else needs are listed too.
func removeHandler(line string) (system.FakeResult, bool) {
	rest, ok := strings.CutPrefix(line, "pacman -R")
	if !ok {
		return system.FakeResult{}, false
	}
	flags, rest, ok := strings.Cut(rest, "p --print-format %n %v -- ")
	if !ok || strings.Trim(flags, "s") != "" {
		return system.FakeResult{}, false
	}
	names := strings.Fields(rest)
	blocks := qiBlocks()
	removed := map[string]bool{}
	for _, n := range names {
		if _, ok := blocks[n]; !ok {
			return system.FakeResult{Stderr: []byte("error: target not found: " + n + "\n"), Code: 1}, true
		}
		removed[n] = true
	}

	if strings.Contains(flags, "s") {
		// Dependencies that are not explicit and are needed by nothing that
		// stays.
		for changed := true; changed; {
			changed = false
			for _, n := range slices.Sorted(mapKeys(removed)) {
				for _, d := range blocks[n].depends {
					db, ok := blocks[d]
					if !ok || removed[d] || db.explicit {
						continue
					}
					if allIn(db.requiredBy, removed) {
						removed[d], changed = true, true
					}
				}
			}
		}
	}

	var breaks []string
	for _, n := range slices.Sorted(mapKeys(removed)) {
		for _, r := range blocks[n].requiredBy {
			if !removed[r] {
				breaks = append(breaks, fmt.Sprintf(":: removing %s breaks dependency '%s' required by %s", n, n, r))
			}
		}
	}
	if len(breaks) > 0 {
		stderr := "checking dependencies...\nerror: failed to prepare transaction (could not satisfy dependencies)\n" +
			strings.Join(breaks, "\n") + "\n"
		return system.FakeResult{Stderr: []byte(stderr), Code: 1}, true
	}
	var out strings.Builder
	for _, n := range names {
		fmt.Fprintf(&out, "%s %s\n", n, blocks[n].version)
	}
	for _, n := range slices.Sorted(mapKeys(removed)) {
		if !slices.Contains(names, n) {
			fmt.Fprintf(&out, "%s %s\n", n, blocks[n].version)
		}
	}
	return system.FakeResult{Stdout: []byte(out.String())}, true
}

func mapKeys(m map[string]bool) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

func allIn(names []string, set map[string]bool) bool {
	for _, n := range names {
		if !set[n] {
			return false
		}
	}
	return true
}
