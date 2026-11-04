package fixtures

import (
	"fmt"
	"strings"

	"github.com/leNITHISH/cronen/internal/system"
)

// The M15 fixtures are HAND-WRITTEN, pending capture from a real Arch
// system (see HAND_WRITTEN.txt): the pacman.log transactions, the cache
// files in FS (see cacheFiles) and the answers below.

// Build dates of the cached packages, as `pacman -Qip` prints them in the
// C locale.
var cachedBuildDates = map[string]string{
	"linux-6.17.3.arch1-1":     "Sat Oct 10 19:18:51 2026",
	"linux-6.17.2.arch1-1":     "Sat Oct  3 08:00:00 2026",
	"linux-6.17.4.arch1-1":     "Sat Oct 17 09:30:12 2026",
	"nvidia-utils-575.64-1":    "Tue Sep 23 12:00:00 2026",
	"nvidia-utils-580.95.05-1": "Thu Oct  2 12:00:00 2026",
	"firefox-143.0-1":          "Tue Sep 23 06:00:00 2026",
}

// splitCacheFile returns name and version of
// <dir>/<name>-<pkgver>-<pkgrel>-<arch>.pkg.tar.zst, or ok false.
func splitCacheFile(p string) (name, version string, ok bool) {
	base := p[strings.LastIndex(p, "/")+1:]
	base, ok = strings.CutSuffix(base, ".pkg.tar.zst")
	if !ok {
		return "", "", false
	}
	parts := strings.Split(base, "-")
	if len(parts) < 4 {
		return "", "", false
	}
	n := len(parts)
	return strings.Join(parts[:n-3], "-"), parts[n-3] + "-" + parts[n-2], true
}

// downgradeHandler answers `pacman -Qip -- files...` (name, version and
// build date of each package file) and the dry run `pacman -Up
// --print-format %n %v -- files...`. A file that isn't in the fixture cache
// is "could not open file"; installing linux 6.17.2 fails the way a kernel
// older than its modules package does.
func downgradeHandler(line string) (system.FakeResult, bool) {
	if rest, ok := strings.CutPrefix(line, "pacman -Qip -- "); ok {
		var out []string
		for _, f := range strings.Fields(rest) {
			name, ver, ok := splitCacheFile(f)
			date, known := cachedBuildDates[name+"-"+ver]
			if !ok || !known {
				return system.FakeResult{Stderr: []byte("error: '" + f + "': could not open file\n"), Code: 1}, true
			}
			out = append(out, fmt.Sprintf("Name            : %s\nVersion         : %s\nBuild Date      : %s\nInstalled Size  : 140.00 MiB\n", name, ver, date))
		}
		return system.FakeResult{Stdout: []byte(strings.Join(out, "\n"))}, true
	}
	rest, ok := strings.CutPrefix(line, "pacman -Up --print-format %n %v -- ")
	if !ok {
		return system.FakeResult{}, false
	}
	var out strings.Builder
	for _, f := range strings.Fields(rest) {
		name, ver, ok := splitCacheFile(f)
		if _, known := cachedBuildDates[name+"-"+ver]; !ok || !known {
			return system.FakeResult{Stderr: []byte("error: '" + f + "': could not open file\n"), Code: 1}, true
		}
		if name == "linux" && ver == "6.17.2.arch1-1" {
			return system.FakeResult{Stderr: Bytes(PacmanUpUnsatisfied), Code: 1}, true
		}
		fmt.Fprintf(&out, "%s %s\n", name, ver)
	}
	return system.FakeResult{Stdout: []byte(out.String())}, true
}

// PacmanUpUnsatisfied is the stderr of `pacman -Up` for linux 6.17.2.
const PacmanUpUnsatisfied = "pacman_Up_unsatisfied.stderr"
