package rebuild

import "strings"

// Soname is a shared library as pacman names it in Provides and Depends On.
type Soname struct {
	// Name is the library without its version: "libicuuc.so".
	Name string
	// Version is the soname's version: "75" for libicuuc.so.75. It is
	// empty or "0" for libraries without one.
	Version string
	// Arch is "64" or "32", or empty when the spelling doesn't say.
	Arch string
}

// File is the file name an ELF file lists in DT_NEEDED for the soname:
// "libicuuc.so.75".
func (s Soname) File() string {
	if s.Version == "" || s.Version == "0" {
		return s.Name
	}
	return s.Name + "." + s.Version
}

// Same reports whether two sonames name the same library version. Arch is
// only compared when both spellings carry one.
func (s Soname) Same(o Soname) bool {
	if s.Name != o.Name || s.Version != o.Version {
		return false
	}
	return s.Arch == "" || o.Arch == "" || s.Arch == o.Arch
}

// ParseSoname parses a Provides or Depends On entry that names a shared
// library. Two spellings exist, and both are handled:
//
//   - "libfoo.so=75-64": the library, its version and the word size
//     (what makepkg's autodeps wrote up to pacman 7.0);
//   - "lib:libfoo.so.75" (and "lib32:" for 32-bit): the file name with a
//     prefix (the newer form).
//
// Anything else, such as "foo=1.0" or "bar>=2", is not a soname.
//
// Needs checking on Arch: both spellings against current repo packages.
// See PLAN.md.
func ParseSoname(dep string) (Soname, bool) {
	if file, ok := strings.CutPrefix(dep, "lib:"); ok {
		return fromFile(file, "")
	}
	if file, ok := strings.CutPrefix(dep, "lib32:"); ok {
		return fromFile(file, "32")
	}
	name, ver, ok := strings.Cut(dep, "=")
	if !ok || !strings.HasSuffix(name, ".so") || name == ".so" {
		return Soname{}, false
	}
	version, arch := ver, ""
	if i := strings.LastIndexByte(ver, '-'); i >= 0 {
		version, arch = ver[:i], ver[i+1:]
	}
	if (arch != "32" && arch != "64") || version == "" {
		return Soname{}, false
	}
	return Soname{Name: name, Version: version, Arch: arch}, true
}

// fromFile splits a file name such as "libfoo.so.75" at ".so".
func fromFile(file, arch string) (Soname, bool) {
	for from := 1; from < len(file); from++ {
		i := strings.Index(file[from:], ".so")
		if i < 0 {
			break
		}
		end := from + i + len(".so")
		if end == len(file) || file[end] == '.' {
			return Soname{Name: file[:end], Version: strings.TrimPrefix(file[end:], "."), Arch: arch}, true
		}
		from += i
	}
	return Soname{}, false
}

// Sonames returns the shared libraries named by a Provides or Depends On
// list, ignoring every other entry.
func Sonames(deps []string) []Soname {
	var out []Soname
	for _, d := range deps {
		if s, ok := ParseSoname(d); ok {
			out = append(out, s)
		}
	}
	return out
}
