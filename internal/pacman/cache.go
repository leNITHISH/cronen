package pacman

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/leNITHISH/cronen/internal/system"
)

// CachedPackage is one package file in a cache directory.
type CachedPackage struct {
	Name string
	// Version is [epoch:]pkgver-pkgrel, as the file name spells it.
	Version string
	Arch    string
	// File is the file name, Dir the directory it lies in and Path the two
	// joined.
	File, Dir, Path string
	Size            int64
	// HasSig reports a "<file>.sig" next to the file.
	HasSig bool
	// BuildDate is the package's Build Date, read from the file with
	// `pacman -Qip`; zero when it hasn't been read or couldn't be.
	BuildDate time.Time
}

// cacheExts are the package extensions, as IsPackageFile knows them.
var cacheExts = []string{".pkg.tar.zst", ".pkg.tar.xz", ".pkg.tar.gz", ".pkg.tar.bz2", ".pkg.tar.lz4", ".pkg.tar.lzo", ".pkg.tar.Z", ".pkg.tar"}

// ParseCacheName splits a cache file name,
// <name>-<pkgver>-<pkgrel>-<arch>.pkg.tar.<ext>, on its last three hyphens
// (package names contain hyphens). Signatures, partial downloads and
// anything else that doesn't fit report false.
func ParseCacheName(file string) (name, version, arch string, ok bool) {
	base := ""
	for _, ext := range cacheExts {
		if b, found := strings.CutSuffix(file, ext); found {
			base = b
			break
		}
	}
	if base == "" {
		return "", "", "", false
	}
	parts := strings.Split(base, "-")
	if len(parts) < 4 {
		return "", "", "", false
	}
	n := len(parts)
	arch, pkgrel, pkgver := parts[n-1], parts[n-2], parts[n-3]
	name = strings.Join(parts[:n-3], "-")
	if !ValidName(name) || !validArch(arch) || !validPkgrel(pkgrel) || !validPkgver(pkgver) {
		return "", "", "", false
	}
	return name, pkgver + "-" + pkgrel, arch, true
}

func validArch(a string) bool {
	if a == "" {
		return false
	}
	for _, r := range a {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func validPkgrel(r string) bool {
	if r == "" || r[0] == '.' || r[len(r)-1] == '.' {
		return false
	}
	for _, c := range r {
		if (c < '0' || c > '9') && c != '.' {
			return false
		}
	}
	return true
}

func validPkgver(v string) bool {
	if v == "" {
		return false
	}
	for _, r := range v {
		if r == ':' || r == '.' || r == '_' || r == '+' || r == '~' || unicode.IsLetter(r) && r < unicode.MaxASCII || r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// StripEpoch drops the "N:" epoch from a version.
func StripEpoch(v string) string {
	if i := strings.IndexByte(v, ':'); i > 0 && strings.Trim(v[:i], "0123456789") == "" {
		return v[i+1:]
	}
	return v
}

// SameVersion reports whether two spellings of a version are the same: equal,
// or equal apart from an epoch that only one of them carries (cache file
// names may leave the epoch out; pacman.log keeps it).
func SameVersion(a, b string) bool {
	if a == b {
		return true
	}
	hasEpoch := func(v string) bool { return StripEpoch(v) != v }
	if hasEpoch(a) != hasEpoch(b) {
		return StripEpoch(a) == StripEpoch(b)
	}
	return false
}

// ResolveCacheDirs returns the directories package files are looked for in:
// CacheDir from pacman.conf (else the default), then the directories
// matching the globs in extra, whose leading ~/ is expanded with home. They
// are cleaned and appear once.
func ResolveCacheDirs(fsys system.FS, extra []string, home string) []string {
	dirs := []string{DefaultCacheDir}
	if conf, err := fsys.ReadFile(ConfPath); err == nil {
		dirs = CacheDirs(conf)
	}
	for _, g := range extra {
		if rest, ok := strings.CutPrefix(g, "~/"); ok {
			if home == "" {
				continue
			}
			g = filepath.Join(home, rest)
		}
		matches, err := fsys.Glob(g)
		if err != nil {
			continue
		}
		dirs = append(dirs, matches...)
	}
	var out []string
	for _, d := range dirs {
		d = filepath.Clean(d)
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}

// ScanCache lists the package files in dirs. A directory that doesn't exist
// is skipped; any other read error is reported after the others are read.
// Files that don't parse, and signatures, are not listed. The result is
// sorted by name, then file name.
func ScanCache(fsys system.FS, dirs []string) ([]CachedPackage, error) {
	var out []CachedPackage
	var errs []error
	for _, d := range dirs {
		d = filepath.Clean(d)
		entries, err := fsys.ReadDir(d)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, fmt.Errorf("package cache %s: %w", d, err))
			}
			continue
		}
		names := make(map[string]bool, len(entries))
		for _, e := range entries {
			names[e.Name()] = true
		}
		for _, e := range entries {
			name, version, arch, ok := ParseCacheName(e.Name())
			if !ok {
				continue
			}
			out = append(out, CachedPackage{
				Name: name, Version: version, Arch: arch,
				File: e.Name(), Dir: d, Path: filepath.Join(d, e.Name()),
				Size: e.Size(), HasSig: names[e.Name()+".sig"],
			})
		}
	}
	slices.SortStableFunc(out, func(a, b CachedPackage) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.File, b.File)
	})
	return out, errors.Join(errs...)
}

// CheckCachePath refuses a path that may not be given to `pacman -U`
// (safety invariant 3): it must be absolute and clean (no "..", "//" or
// "./"), free of control characters, the name of a package file, and lie
// directly inside one of dirs. It looks at the text only; CheckCacheFile
// also looks at the file.
func CheckCachePath(path string, dirs []string) error {
	switch {
	case path == "":
		return errors.New("refusing an empty package path")
	case strings.ContainsFunc(path, unicode.IsControl):
		return fmt.Errorf("refusing package path %q: control characters", path)
	case !filepath.IsAbs(path):
		return fmt.Errorf("refusing package path %q: not absolute", path)
	case filepath.Clean(path) != path:
		return fmt.Errorf("refusing package path %q: not a clean path", path)
	}
	if _, _, _, ok := ParseCacheName(filepath.Base(path)); !ok {
		return fmt.Errorf("refusing package path %q: not a package file name", path)
	}
	dir := filepath.Dir(path)
	for _, d := range dirs {
		if d != "" && filepath.Clean(d) == dir {
			return nil
		}
	}
	return fmt.Errorf("refusing package path %q: not inside a configured cache directory", path)
}

// CheckCacheFile is CheckCachePath plus a look at the file: it must exist
// and be a regular file, not a symbolic link, a directory or anything else.
func CheckCacheFile(fsys system.FS, path string, dirs []string) error {
	if err := CheckCachePath(path, dirs); err != nil {
		return err
	}
	fi, err := fsys.Lstat(path)
	if err != nil {
		return fmt.Errorf("package file %s: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("refusing package path %q: not a regular file", path)
	}
	return nil
}

// OlderCached returns the files among cached that hold a version of name
// older than installed, newest version first, one per version. Versions are
// compared with vercmp. A file that is the installed version (apart from an
// epoch its name leaves out) is not older.
func OlderCached(ctx context.Context, r system.Runner, cached []CachedPackage, name, installed string) ([]CachedPackage, error) {
	var older []CachedPackage
	var firstErr error
	seen := map[string]bool{}
	for _, c := range cached {
		if c.Name != name || seen[StripEpoch(c.Version)] || SameVersion(c.Version, installed) {
			continue
		}
		n, err := Vercmp(ctx, r, c.Version, installed)
		if err != nil {
			return nil, err
		}
		if n < 0 {
			seen[StripEpoch(c.Version)] = true
			older = append(older, c)
		}
	}
	sort.SliceStable(older, func(i, j int) bool {
		if firstErr != nil {
			return false
		}
		n, err := Vercmp(ctx, r, older[i].Version, older[j].Version)
		if err != nil {
			firstErr = err
			return false
		}
		return n > 0
	})
	return older, firstErr
}

// ReadBuildDates reads the Build Date of each package file with one
// read-only `pacman -Qip -- <files>` and returns pkgs with BuildDate set
// where pacman said. A failure leaves the dates zero and is returned.
func ReadBuildDates(ctx context.Context, r system.Runner, dirs []string, pkgs []CachedPackage, loc *time.Location) ([]CachedPackage, error) {
	out := slices.Clone(pkgs)
	if len(out) == 0 {
		return out, nil
	}
	argv := []string{"-Qip", "--"}
	for _, p := range out {
		if err := CheckCachePath(p.Path, dirs); err != nil {
			return out, err
		}
		argv = append(argv, p.Path)
	}
	data, _, err := r.Run(ctx, "pacman", argv...)
	if err != nil {
		return out, fmt.Errorf("reading build dates: %w", err)
	}
	infos, err := ParseInfo(data, loc)
	if err != nil {
		return out, fmt.Errorf("reading build dates: %w", err)
	}
	for i := range out {
		for _, in := range infos {
			if in.Name == out[i].Name && SameVersion(in.Version, out[i].Version) {
				out[i].BuildDate = in.BuildDate
				break
			}
		}
	}
	return out, nil
}
