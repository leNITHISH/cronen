package snapshot

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	// tsRow is a row of the table `timeshift --list` prints: a number, an
	// optional marker, the name, then tags and a description:
	//
	//	0    >  2024-01-05_10-00-01  O     first
	tsRow = regexp.MustCompile(`^\s*(\d+)\s*(?:[>*+-]\s+|\s+)(\S+)\s*(.*)$`)
	// tsNameish is something that starts like a snapshot name. One that
	// is not a whole valid name is a cut-off or garbled row.
	tsNameish = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)
	// tsCount is the summary line ("2 snapshots, 85.5 GB free"). The words
	// are localised, so only its shape counts.
	tsCount = regexp.MustCompile(`^\s*(\d+)\s+\S+,\s`)
	tsRule  = regexp.MustCompile(`^\s*-{10,}\s*$`)
	tsTag   = regexp.MustCompile(`^[OBHDWMS](?:,[OBHDWMS])*$`)
)

// ParseTimeshiftList reads `timeshift --list`. It is tolerant of what a real
// system adds (mount chatter above the table, ANSI colours, extra columns,
// blank lines, a missing header) and strict about what it does not
// understand: a row that starts like a snapshot name but is not a whole one,
// a summary count that disagrees with the rows, or output with neither rows
// nor a table header all give ErrUnreadable rather than an empty list.
// loc is the zone of the names; nil means time.Local.
func ParseTimeshiftList(out []byte, loc *time.Location) ([]Snapshot, error) {
	if loc == nil {
		loc = time.Local
	}
	text := StripANSI(string(out))
	if strings.TrimSpace(text) == "" {
		return nil, unreadable("timeshift printed nothing")
	}
	var (
		snaps  []Snapshot
		header bool
		count  = -1
	)
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if tsRule.MatchString(line) {
			header = true
			continue
		}
		if m := tsRow.FindStringSubmatch(line); m != nil {
			if strings.HasPrefix(m[2], "-") || m[2] == ">" {
				continue
			}
			name := m[2]
			if !timeshiftName.MatchString(name) {
				if tsNameish.MatchString(name) {
					return nil, unreadable("a snapshot name is cut off or garbled: " + name)
				}
				// A count line ("2 snapshots, …"), or chatter that
				// starts with a number.
				if c := tsCount.FindStringSubmatch(line); c != nil {
					count, _ = strconv.Atoi(c[1])
				}
				continue
			}
			t, err := time.ParseInLocation(TimeshiftNameLayout, name, loc)
			if err != nil {
				return nil, unreadable("not a date: " + name)
			}
			tags, desc := splitTags(m[3])
			snaps = append(snaps, Snapshot{Tool: Timeshift, Name: name, Date: t, Type: tags, Description: desc})
			continue
		}
		if c := tsCount.FindStringSubmatch(line); c != nil {
			count, _ = strconv.Atoi(c[1])
		}
	}
	if count >= 0 && count != len(snaps) {
		return nil, unreadable(fmt.Sprintf("timeshift counts %d snapshots, %d rows were read (is the output cut off?)", count, len(snaps)))
	}
	if len(snaps) == 0 && !header && count != 0 {
		return nil, unreadable("no table of snapshots in the output")
	}
	return snaps, nil
}

// splitTags separates the leading tag letters (O B H D W M) from the
// description.
func splitTags(rest string) (tags, desc string) {
	f := strings.Fields(rest)
	i := 0
	var ts []string
	for i < len(f) && tsTag.MatchString(f[i]) {
		ts = append(ts, f[i])
		i++
	}
	return strings.Join(ts, " "), strings.Join(f[i:], " ")
}

// TimeshiftConfig is what cronen reads from /etc/timeshift/timeshift.json.
type TimeshiftConfig struct {
	// Mode is "btrfs" or "rsync".
	Mode string
	// BackupUUID is the backup device's UUID, "" if not set.
	BackupUUID string
}

// ParseTimeshiftConfig reads timeshift.json. Timeshift writes its booleans
// as the strings "true" and "false"; real booleans are accepted too. Unknown
// fields are ignored and missing ones left empty.
func ParseTimeshiftConfig(data []byte) (TimeshiftConfig, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return TimeshiftConfig{}, fmt.Errorf("timeshift.json: %w", err)
	}
	if raw == nil {
		return TimeshiftConfig{}, fmt.Errorf("timeshift.json: not an object")
	}
	c := TimeshiftConfig{Mode: "rsync"}
	if truthy(raw["btrfs_mode"]) {
		c.Mode = "btrfs"
	}
	if s, ok := raw["backup_device_uuid"].(string); ok && uuidRE.MatchString(s) {
		c.BackupUUID = s
	}
	return c, nil
}

var uuidRE = regexp.MustCompile(`^[0-9A-Fa-f-]{8,64}$`)

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(strings.TrimSpace(x), "true")
	}
	return false
}

var timeshiftMountRE = regexp.MustCompile(`^/run/timeshift/(?:\d+/)?backup$`)

// BackupMount finds where the backup device is mounted in the contents of
// /proc/self/mounts: a mount whose source names the UUID, or Timeshift's own
// mount point under /run/timeshift. ok is false when it is not mounted, in
// which case no free space is shown.
func BackupMount(mounts []byte, uuid string) (string, bool) {
	for line := range strings.Lines(string(mounts)) {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		mp := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\134`, `\`).Replace(f[1])
		if uuid != "" && strings.Contains(f[0], uuid) {
			return mp, true
		}
		if timeshiftMountRE.MatchString(mp) {
			return mp, true
		}
	}
	return "", false
}
