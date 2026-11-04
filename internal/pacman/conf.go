package pacman

import (
	"bufio"
	"bytes"
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
