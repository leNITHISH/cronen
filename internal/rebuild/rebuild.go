package rebuild

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// Signal says how a rebuild was detected. A package can be caught by more
// than one, and each is reported.
type Signal int

// Signals.
const (
	// SignalMetadata: the foreign package's Depends On names a soname the
	// updated package no longer provides.
	SignalMetadata Signal = iota + 1
	// SignalELF: an ELF file of the foreign package lists a lost soname in
	// DT_NEEDED.
	SignalELF
	// SignalModuleDir: the foreign package owns files under a directory
	// tied to the old version of an updated interpreter.
	SignalModuleDir
)

func (s Signal) String() string {
	switch s {
	case SignalMetadata:
		return "metadata"
	case SignalELF:
		return "elf"
	case SignalModuleDir:
		return "module-dir"
	}
	return "Signal(" + strconv.Itoa(int(s)) + ")"
}

// Update is a pending repo update with the Provides of both versions.
type Update struct {
	Name                   string
	OldVersion, NewVersion string
	// OldProvides is from pacman -Qi, NewProvides from pacman -Si.
	OldProvides, NewProvides []string
}

// Need is a library an ELF file asks for.
type Need struct {
	// Lib is the DT_NEEDED entry: "libicuuc.so.75".
	Lib string `json:"lib"`
	// Arch is "64" or "32", from the ELF class.
	Arch string `json:"arch"`
}

// Foreign is an installed foreign package, as far as detection needs it.
type Foreign struct {
	Name, Version string
	// Debug marks a makepkg debug split. Its ELF files are stripped-out
	// debug info, not programs, so it is never reported.
	Debug bool
	// Depends is "Depends On" from pacman -Qi.
	Depends []string
	// Needs are the DT_NEEDED entries of the package's ELF files. Nil
	// means the ELF scan didn't run.
	Needs []Need
	// Files are the paths the package owns.
	Files []string
}

// Finding is one installed foreign package that needs a rebuild after the
// upgrade, with the update to blame.
type Finding struct {
	// Package and Version are the foreign package.
	Package, Version string
	Signal           Signal
	// Culprit is the pending update behind it, with its versions.
	Culprit                string
	CulpritOld, CulpritNew string
	// Detail is the library (libicuuc.so.75) for SignalMetadata and
	// SignalELF, and the directory (/usr/lib/python3.13/) for
	// SignalModuleDir.
	Detail string
}

// Lost returns the sonames u's old version provided that the new one no
// longer does. A soname both versions provide, even with a different
// spelling, is not lost.
func Lost(u Update) []Soname {
	newS := Sonames(u.NewProvides)
	var lost []Soname
	for _, o := range Sonames(u.OldProvides) {
		if !slices.ContainsFunc(newS, o.Same) && !slices.ContainsFunc(lost, o.Same) {
			lost = append(lost, o)
		}
	}
	return lost
}

// Detect finds the foreign packages the updates leave needing a rebuild.
// Findings are ordered by culprit, package, signal and detail.
func Detect(updates []Update, foreign []Foreign) []Finding {
	var out []Finding
	for _, u := range updates {
		lost := Lost(u)
		moved, hasMoved := Moved(u)
		if len(lost) == 0 && !hasMoved {
			continue
		}
		base := Finding{Culprit: u.Name, CulpritOld: u.OldVersion, CulpritNew: u.NewVersion}
		for _, f := range foreign {
			if f.Debug {
				continue
			}
			base.Package, base.Version = f.Name, f.Version
			for _, l := range lost {
				if slices.ContainsFunc(Sonames(f.Depends), l.Same) {
					out = append(out, withSignal(base, SignalMetadata, l.File()))
				}
				if slices.ContainsFunc(f.Needs, needs(l)) {
					out = append(out, withSignal(base, SignalELF, l.File()))
				}
			}
			if hasMoved {
				if dir, ok := ownsUnder(f.Files, moved); ok {
					out = append(out, withSignal(base, SignalModuleDir, dir))
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b Finding) int {
		return cmp.Or(
			strings.Compare(a.Culprit, b.Culprit), strings.Compare(a.Package, b.Package),
			cmp.Compare(a.Signal, b.Signal), strings.Compare(a.Detail, b.Detail))
	})
	return slices.Compact(out)
}

func withSignal(f Finding, s Signal, detail string) Finding {
	f.Signal, f.Detail = s, detail
	return f
}

// needs matches an ELF's DT_NEEDED entry against a lost soname. A 32-bit
// binary doesn't need the 64-bit library, and the other way round.
func needs(l Soname) func(Need) bool {
	return func(n Need) bool {
		return n.Lib == l.File() && (l.Arch == "" || n.Arch == "" || l.Arch == n.Arch)
	}
}

// ownsUnder returns the first directory in dirs that one of files lies
// under.
func ownsUnder(files, dirs []string) (string, bool) {
	for _, d := range dirs {
		for _, f := range files {
			if strings.HasPrefix(f, d) {
				return d, true
			}
		}
	}
	return "", false
}
