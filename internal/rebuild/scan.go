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

// needsOf reads the DT_NEEDED entries of one file, or nothing if it can't
// be read or isn't ELF.
func needsOf(op system.Opener, path string) (needs []Need) {
	f, err := op.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	// A hostile or corrupt file must never take cronen down.
	defer func() {
		if recover() != nil {
			needs = nil
		}
	}()

	e, err := elf.NewFile(f)
	if err != nil {
		return nil
	}
	libs, err := e.ImportedLibraries()
	if err != nil {
		return nil
	}
	arch := "64"
	if e.Class == elf.ELFCLASS32 {
		arch = "32"
	}
	for _, l := range libs {
		needs = append(needs, Need{Lib: l, Arch: arch})
	}
	return needs
}
