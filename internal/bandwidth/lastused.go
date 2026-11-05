package bandwidth

import (
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Mount is one line of /proc/self/mounts.
type Mount struct {
	Point   string
	Options []string
}

// ParseMounts parses /proc/self/mounts: "device mountpoint fstype options
// dump pass", with spaces in the mount point written as \040. Lines that
// don't have that shape are skipped.
func ParseMounts(data []byte) []Mount {
	var out []Mount
	for line := range strings.Lines(string(data)) {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		point := mountEscaper.Replace(f[1])
		if len(point) > 1 {
			point = strings.TrimRight(point, "/")
		}
		out = append(out, Mount{Point: point, Options: strings.Split(f[3], ",")})
	}
	return out
}

var mountEscaper = strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)

// MountFor returns the mount holding p: the one with the longest mount
// point that is p or a parent of it.
func MountFor(mounts []Mount, p string) (Mount, bool) {
	var best Mount
	found := false
	for _, m := range mounts {
		inside := m.Point == "/" || p == m.Point || strings.HasPrefix(p, m.Point+"/")
		if inside && (!found || len(m.Point) > len(best.Point)) {
			best, found = m, true
		}
	}
	return best, found
}

// AtimeTrusted reports whether file access times on the mount mean
// something. They do under relatime (updated when the file is older than
// its mtime or a day) and strictatime (always; the kernel lists neither
// option for it). Under noatime they are never updated, so any "last used"
// date built from them would be a lie. Unknown mounts aren't trusted.
func (m Mount) AtimeTrusted() bool {
	if len(m.Options) == 0 {
		return false
	}
	return !slices.Contains(m.Options, "noatime")
}

// UsageFiles picks, from a package's files, the ones whose access time says
// the package was used: the executables in a bin or sbin directory and the
// .desktop files in an applications directory. Directories (trailing slash)
// are never picked.
func UsageFiles(files []string) []string {
	var out []string
	for _, f := range files {
		if f == "" || strings.HasSuffix(f, "/") {
			continue
		}
		dir := path.Base(path.Dir(f))
		switch {
		case dir == "bin" || dir == "sbin":
			out = append(out, f)
		case dir == "applications" && strings.HasSuffix(f, ".desktop"):
			out = append(out, f)
		}
	}
	return out
}

// Access is the access time of one file.
type Access struct {
	Path string
	Time time.Time
}

// LastUsed returns the newest access time among accesses whose file lies on
// a mount where access times are trusted. ok is false when there is none,
// which is also what a package on a noatime mount gets: the date is hidden
// rather than guessed.
func LastUsed(accesses []Access, mounts []Mount) (t time.Time, ok bool) {
	for _, a := range accesses {
		if a.Time.IsZero() {
			continue
		}
		m, found := MountFor(mounts, a.Path)
		if !found || !m.AtimeTrusted() {
			continue
		}
		if a.Time.After(t) {
			t, ok = a.Time, true
		}
	}
	return t, ok
}

// Ago says how long before now t was, roughly: "today", "3 days", "~4
// months", "~2 years".
func Ago(t, now time.Time) string {
	d := now.Sub(t)
	days := int(d / (24 * time.Hour))
	switch {
	case d < 24*time.Hour:
		return "today"
	case days == 1:
		return "1 day ago"
	case days < 60:
		return itoa(days) + " days ago"
	case days < 730:
		return "~" + itoa(days/30) + " months ago"
	}
	return "~" + itoa(days/365) + " years ago"
}

func itoa(n int) string { return strconv.Itoa(n) }
