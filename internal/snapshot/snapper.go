package snapshot

import (
	"strconv"
	"strings"
	"time"
)

// snapperColumns are the columns of `snapper list`, in the order snapper
// prints them, used when the header line is missing.
var snapperColumns = []string{"#", "type", "pre #", "date", "user", "cleanup", "description", "userdata"}

// ParseSnapperList reads `snapper -c <config> list`, a table with `|`
// separators:
//
//	 # | Type   | Pre # | Date                     | User | Cleanup | Description | Userdata
//	---+--------+-------+--------------------------+------+---------+-------------+---------
//	0  | single |       |                          | root |         | current     |
//	1* | single |       | Mon Jan  1 10:00:00 2024 | root | number  | cronen: ... |
//
// Columns are found by their titles, so extra columns and a different order
// work; without a header snapper's default order is assumed. The number may
// carry a +, - or * (the default and active snapshots). Snapshot 0 is the
// running system, not a snapshot. A line with separators whose first column
// is not a number, a row too short for its columns, or no table at all
// gives ErrUnreadable. loc is the zone of the dates; nil means time.Local.
func ParseSnapperList(out []byte, loc *time.Location) ([]Snapshot, error) {
	if loc == nil {
		loc = time.Local
	}
	text := StripANSI(string(out))
	if strings.TrimSpace(text) == "" {
		return nil, unreadable("snapper printed nothing")
	}
	cols := snapperColumns
	var snaps []Snapshot
	table := false
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, "\r\n")
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.Trim(t, "-+ ") == "" && strings.Contains(t, "-") {
			table = true // the rule under the header
			continue
		}
		if !strings.Contains(line, "|") {
			return nil, unreadable("unexpected line: " + shorten(t))
		}
		cells := strings.Split(line, "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		first := strings.TrimRight(cells[0], "+-*")
		if _, err := strconv.Atoi(first); err != nil {
			if strings.EqualFold(first, "#") || strings.HasPrefix(first, "#") {
				cols = lowerAll(cells)
				table = true
				continue
			}
			return nil, unreadable("a row does not start with a snapshot number: " + shorten(t))
		}
		table = true
		if len(cells) < 2 {
			return nil, unreadable("a row is cut off: " + shorten(t))
		}
		get := func(name string) (string, bool) {
			for i, c := range cols {
				if c == name {
					if i < len(cells) {
						return cells[i], true
					}
					return "", false
				}
			}
			return "", false
		}
		num := first
		if first == "0" {
			continue
		}
		if !snapperName.MatchString(num) {
			return nil, unreadable("bad snapshot number " + num)
		}
		typ, okType := get("type")
		date, okDate := get("date")
		if !okType || !okDate {
			return nil, unreadable("a row is cut off: " + shorten(t))
		}
		desc, _ := get("description")
		s := Snapshot{Tool: Snapper, Name: num, Type: typ, Description: desc}
		if date != "" {
			s.Date = parseSnapperDate(date, loc)
		}
		snaps = append(snaps, s)
	}
	if !table {
		return nil, unreadable("no table in the output")
	}
	return snaps, nil
}

func lowerAll(cells []string) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = strings.ToLower(c)
	}
	return out
}

func shorten(s string) string {
	if r := []rune(s); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}

var snapperDateLayouts = []string{
	"Mon Jan _2 15:04:05 2006",
	"Mon Jan _2 15:04:05 MST 2006",
	"Mon 02 Jan 2006 03:04:05 PM MST",
	"Mon 02 Jan 2006 15:04:05 MST",
	"Mon 02 Jan 2006 03:04:05 PM",
	"Mon 02 Jan 2006 15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	time.RFC3339,
}

// parseSnapperDate returns the zero time for a date it does not know (a
// localised one, say): the snapshot is still listed, but never used to say
// how old the newest snapshot is.
func parseSnapperDate(s string, loc *time.Location) time.Time {
	for _, l := range snapperDateLayouts {
		if t, err := time.ParseInLocation(l, s, loc); err == nil {
			return t
		}
	}
	return time.Time{}
}
