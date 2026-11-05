package rebuild

import (
	"cmp"
	"context"
	"debug/elf"
	"slices"
	"strings"

	"github.com/leNITHISH/cronen/internal/system"
)

// scanDirs are where an ELF file worth scanning can live. Package files
// elsewhere (docs, icons, locale data) are skipped without being opened.
var scanDirs = []string{"/usr/bin/", "/usr/sbin/", "/usr/lib/", "/usr/lib32/", "/usr/libexec/", "/opt/", "/usr/local/"}

// skipDirs are under scanDirs but hold no programs that link libraries:
// debug info is the debug split's business.
var skipDirs = []string{"/usr/lib/debug/", "/usr/lib/modules/", "/usr/lib/firmware/"}

// skipSuffixes are file types that are never ELF.
var skipSuffixes = []string{
	".py", ".pyc", ".pyi", ".js", ".mjs", ".json", ".h", ".hpp", ".txt", ".md", ".html", ".css",
	".png", ".svg", ".jpg", ".ttf", ".otf", ".woff", ".woff2", ".gz", ".xz", ".zst", ".mo",
	".desktop", ".service", ".pak", ".dat", ".xml", ".pc", ".cmake", ".a",
}

// Candidate reports whether path is worth opening to look for ELF.
// Directories (trailing slash) are not.
func Candidate(path string) bool {
	if strings.HasSuffix(path, "/") {
		return false
	}
	for _, d := range skipDirs {
		if strings.HasPrefix(path, d) {
			return false
		}
	}
	if !slices.ContainsFunc(scanDirs, func(d string) bool { return strings.HasPrefix(path, d) }) {
		return false
	}
	for _, s := range skipSuffixes {
		if strings.HasSuffix(path, s) {
			return false
		}
	}
	return true
}

// ScanNeeds opens the candidate files and returns the DT_NEEDED entries of
// those that are ELF, without duplicates. Files that are missing, aren't
// regular, or aren't ELF are skipped. It stops with ctx.Err() when ctx is
// cancelled.
func ScanNeeds(ctx context.Context, op system.Opener, files []string) ([]Need, error) {
	seen := map[Need]bool{}
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !Candidate(path) {
			continue
		}
		for _, n := range needsOf(op, path) {
			seen[n] = true
		}
	}
	out := make([]Need, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	slices.SortFunc(out, func(a, b Need) int {
		return cmp.Or(strings.Compare(a.Lib, b.Lib), strings.Compare(a.Arch, b.Arch))
	})
	return out, nil
}

// ELFInfo is what the linkage scan and the rebuild scan read from an ELF
// file's dynamic section.
type ELFInfo struct {
	// Class32 reports a 32-bit file.
	Class32 bool
	// Needed are the DT_NEEDED entries, in order.
	Needed []string
	// RunPath are the DT_RUNPATH directories, or the DT_RPATH ones when
	// there is no DT_RUNPATH (the dynamic loader ignores RPATH then).
	// $ORIGIN is not expanded.
	RunPath []string
}

// ReadELF reads the dynamic section of the file at path. ok is false for
// files that are missing, aren't regular, can't be read, aren't ELF or have
// no dynamic section (static programs). It never panics on a corrupt file.
func ReadELF(op system.Opener, path string) (info ELFInfo, ok bool) {
	f, err := op.Open(path)
	if err != nil {
		return ELFInfo{}, false
	}
	defer func() { _ = f.Close() }()
	// A hostile or corrupt file must never take cronen down.
	defer func() {
		if recover() != nil {
			info, ok = ELFInfo{}, false
		}
	}()

	e, err := elf.NewFile(f)
	if err != nil {
		return ELFInfo{}, false
	}
	libs, err := e.ImportedLibraries()
	if err != nil {
		return ELFInfo{}, false
	}
	info.Class32 = e.Class == elf.ELFCLASS32
	info.Needed = libs
	for _, tag := range []elf.DynTag{elf.DT_RUNPATH, elf.DT_RPATH} {
		vals, err := e.DynString(tag)
		if err != nil || len(vals) == 0 {
			continue
		}
		for _, v := range vals {
			for _, d := range strings.Split(v, ":") {
				if d != "" {
					info.RunPath = append(info.RunPath, d)
				}
			}
		}
		break
	}
	return info, true
}

// needsOf reads the DT_NEEDED entries of one file, or nothing if it can't
// be read or isn't ELF.
func needsOf(op system.Opener, path string) []Need {
	info, ok := ReadELF(op, path)
	if !ok {
		return nil
	}
	arch := "64"
	if info.Class32 {
		arch = "32"
	}
	var needs []Need
	for _, l := range info.Needed {
		needs = append(needs, Need{Lib: l, Arch: arch})
	}
	return needs
}
