package pacman

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
)

// ConfPath is pacman's configuration file.
const ConfPath = "/etc/pacman.conf"

// DefaultCacheDir is pacman's cache directory when CacheDir isn't set.
const DefaultCacheDir = "/var/cache/pacman/pkg/"

// CacheDirs returns the CacheDir directories from the [options] section of
// a pacman.conf, in order, or DefaultCacheDir if there are none. Other
// sections, Include lines and everything else are ignored.
func CacheDirs(conf []byte) []string {
	var dirs []string
	inOptions := false
	sc := bufio.NewScanner(bytes.NewReader(conf))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		switch {
		case line == "":
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			inOptions = line == "[options]"
		case inOptions:
			key, val, ok := strings.Cut(line, "=")
			if ok && strings.TrimSpace(key) == "CacheDir" {
				dirs = append(dirs, strings.Fields(val)...)
			}
		}
	}
	if len(dirs) == 0 {
		return []string{DefaultCacheDir}
	}
	return dirs
}

// ParallelDownloads returns the ParallelDownloads setting of the [options]
// section: how many files pacman downloads at once. set is false when the
// key is missing or isn't a positive number, in which case pacman downloads
// one file at a time (its documented default; see the M16 pull request for
// how to confirm that on a real system).
func ParallelDownloads(conf []byte) (n int, set bool) {
	inOptions := false
	n = 1
	sc := bufio.NewScanner(bytes.NewReader(conf))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		switch {
		case line == "":
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			inOptions = line == "[options]"
		case inOptions:
			key, val, ok := strings.Cut(line, "=")
			if !ok || strings.TrimSpace(key) != "ParallelDownloads" {
				continue
			}
			if v, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && v > 0 {
				n, set = v, true
			} else {
				n, set = 1, false
			}
		}
	}
	return n, set
}
