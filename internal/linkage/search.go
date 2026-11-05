package linkage

import (
	"path"
	"strings"
)

// File is the linkage-relevant content of one ELF file.
type File struct {
	Path string `json:"path"`
	// Class32 marks a 32-bit file.
	Class32 bool `json:"class32,omitempty"`
	// Needed are the DT_NEEDED entries.
	Needed []string `json:"needed"`
	// RunPath are the RUNPATH (or, without one, RPATH) directories, with
	// $ORIGIN not yet expanded.
	RunPath []string `json:"runpath,omitempty"`
}

// Default directories searched last, for 64-bit and 32-bit ELF.
const (
	libDir   = "/usr/lib"
	lib32Dir = "/usr/lib32"
)

// Searcher resolves DT_NEEDED entries.
type Searcher struct {
	// ConfDirs are the directories from ld.so.conf, in order.
	ConfDirs []string
	// Exists reports whether a path exists as a file, following symbolic
	// links: a dangling link doesn't exist.
	Exists func(path string) bool
}

// Dirs returns the directories the loader searches for f's libraries, in
// order, without duplicates: RUNPATH/RPATH with $ORIGIN expanded, the
// ld.so.conf directories, then /usr/lib, or /usr/lib32 for a 32-bit file
// (the loader skips libraries of the wrong word size, so /usr/lib's 64-bit
// ones can't satisfy it). The plan lists /usr/lib32 as an addition for
// 32-bit ELF; this is the reading that doesn't hide a missing lib32-glibc.
//
// RUNPATH entries that still hold a variable ($LIB, $PLATFORM) after
// expanding $ORIGIN, and relative ones, are left out: cronen can't say
// where they point, and a library that would only be found there is not
// reported as broken because of it (see Resolve).
func (s Searcher) Dirs(f File) []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(d string) {
		d = path.Clean(d)
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	origin := path.Dir(f.Path)
	for _, d := range f.RunPath {
		d = strings.ReplaceAll(d, "${ORIGIN}", origin)
		d = strings.ReplaceAll(d, "$ORIGIN", origin)
		if strings.Contains(d, "$") || !strings.HasPrefix(d, "/") {
			continue
		}
		add(d)
	}
	for _, d := range s.ConfDirs {
		add(d)
	}
	if f.Class32 {
		// The loader skips a library of the wrong word size, so a 32-bit
		// file is never satisfied by /usr/lib's 64-bit ones.
		add(lib32Dir)
	} else {
		add(libDir)
	}
	return dirs
}

// hasUnknownRunPath reports a RUNPATH entry Dirs had to leave out.
func hasUnknownRunPath(f File) bool {
	for _, d := range f.RunPath {
		d = strings.ReplaceAll(strings.ReplaceAll(d, "${ORIGIN}", ""), "$ORIGIN", "")
		if strings.Contains(d, "$") {
			return true
		}
	}
	return false
}

// Resolve reports whether lib, a DT_NEEDED entry of f, can be found.
//
// An entry with a slash is a path and is checked as it is (a relative one
// can't be judged, and counts as found). Otherwise each directory of Dirs
// is tried. A file whose RUNPATH holds a variable cronen can't expand is
// given the benefit of the doubt: the library counts as found.
func (s Searcher) Resolve(f File, lib string) bool {
	if strings.Contains(lib, "/") {
		if !strings.HasPrefix(lib, "/") {
			return true
		}
		return s.Exists(path.Clean(lib))
	}
	for _, d := range s.Dirs(f) {
		if s.Exists(path.Join(d, lib)) {
			return true
		}
	}
	return hasUnknownRunPath(f)
}
