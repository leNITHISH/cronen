package pacman

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/leNITHISH/cronen/internal/system"
)

// LogPath is where pacman writes its log.
const LogPath = "/var/log/pacman.log"

// ErrNoFullUpgrade reports a pacman.log with no full system upgrade in it.
var ErrNoFullUpgrade = errors.New("pacman.log: no full system upgrade found")

const fullUpgradeMarker = "[PACMAN] starting full system upgrade"

// Timestamp layouts used by pacman.log. Current pacman writes ISO 8601
// with a numeric zone; logs from before pacman 5.2 used a zoneless,
// minute-precision format in local time.
var (
	zonedLayouts = []string{"2006-01-02T15:04:05-0700", time.RFC3339}
	localLayout  = "2006-01-02 15:04"
)

// LastFullUpgrade returns the time of the last "starting full system
// upgrade" entry in a pacman.log. Old zoneless timestamps are interpreted
// in loc. Entries whose timestamp can't be parsed are skipped, so a garbled
// last entry falls back to the one before it. It reports false if no entry
// is found.
func LastFullUpgrade(log []byte, loc *time.Location) (time.Time, bool) {
	for len(log) > 0 {
		i := bytes.LastIndexByte(log[:len(log)-1], '\n')
		line := log[i+1:]
		log = log[:i+1]
		if t, ok := parseFullUpgrade(line, loc); ok {
			return t, true
		}
		if i < 0 {
			break
		}
	}
	return time.Time{}, false
}

// parseFullUpgrade parses one log line, reporting whether it is a full
// upgrade entry with a valid timestamp.
func parseFullUpgrade(line []byte, loc *time.Location) (time.Time, bool) {
	line = bytes.TrimRight(line, "\r\n")
	if len(line) == 0 || line[0] != '[' {
		return time.Time{}, false
	}
	end := bytes.IndexByte(line, ']')
	if end < 0 {
		return time.Time{}, false
	}
	rest := bytes.TrimLeft(line[end+1:], " ")
	if string(bytes.TrimRight(rest, " ")) != fullUpgradeMarker {
		return time.Time{}, false
	}
	t, err := parseTimestamp(string(line[1:end]), loc)
	return t, err == nil
}

func parseTimestamp(s string, loc *time.Location) (time.Time, error) {
	for _, layout := range zonedLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	if loc == nil {
		loc = time.Local
	}
	return time.ParseInLocation(localLayout, s, loc)
}

// ReadLastFullUpgrade reads the pacman.log at path and returns the time of
// the last full upgrade. It returns ErrNoFullUpgrade if the log has none,
// or the read error if the log can't be read; callers should treat either
// as "unknown".
func ReadLastFullUpgrade(fsys system.FS, path string, loc *time.Location) (time.Time, error) {
	data, err := fsys.ReadFile(path)
	if err != nil {
		return time.Time{}, fmt.Errorf("reading pacman.log: %w", err)
	}
	t, ok := LastFullUpgrade(data, loc)
	if !ok {
		return time.Time{}, ErrNoFullUpgrade
	}
	return t, nil
}
