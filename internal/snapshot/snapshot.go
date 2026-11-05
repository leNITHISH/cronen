package snapshot

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Tool is a snapshot backend.
type Tool string

// The backends.
const (
	Timeshift Tool = "timeshift"
	Snapper   Tool = "snapper"
)

// Title is the tool's name as the UI writes it.
func (t Tool) Title() string {
	switch t {
	case Timeshift:
		return "Timeshift"
	case Snapper:
		return "snapper"
	}
	return string(t)
}

// Hooks are the packages that make pacman take a snapshot on every
// transaction by itself. With one installed cronen adds no second snapshot.
var Hooks = []string{"timeshift-autosnap", "snap-pac"}

var (
	timeshiftName = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}$`)
	// snapperName is a number. 0 is snapper's "current" pseudo-snapshot,
	// which can be neither deleted nor restored.
	snapperName = regexp.MustCompile(`^[1-9][0-9]{0,8}$`)
	configName  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`)
	// commentPattern is the only comment cronen passes: its own prefix, then
	// plain words. Nothing a shell, a parser or a terminal could mistake.
	commentPattern = regexp.MustCompile(`^cronen: [A-Za-z0-9 .,:()+@_-]{0,150}$`)
)

// TimeshiftNameLayout is how Timeshift names a snapshot (local time).
const TimeshiftNameLayout = "2006-01-02_15-04-05"

// ValidName reports whether name is a snapshot name of the tool: the
// timestamp for Timeshift, a number for snapper.
func ValidName(t Tool, name string) bool {
	switch t {
	case Timeshift:
		return timeshiftName.MatchString(name)
	case Snapper:
		return snapperName.MatchString(name)
	}
	return false
}

// ValidConfig reports whether name is a snapper config name cronen will
// pass to snapper (it never starts with a dash).
func ValidConfig(name string) bool { return configName.MatchString(name) }

// ValidComment reports whether c is a snapshot comment cronen will pass.
func ValidComment(c string) bool { return commentPattern.MatchString(c) }

// Comment is the description of the snapshot taken before an upgrade:
// "cronen: before -Syu (312 packages)". packages <= 0 leaves the count out.
func Comment(packages int) string {
	switch {
	case packages == 1:
		return "cronen: before -Syu (1 package)"
	case packages > 1:
		return fmt.Sprintf("cronen: before -Syu (%d packages)", packages)
	}
	return "cronen: before -Syu"
}

// Snapshot is one snapshot a tool listed.
type Snapshot struct {
	Tool Tool
	// Name is what delete and restore take: the Timeshift timestamp, the
	// snapper number.
	Name string
	// Date is zero when the tool's date could not be read.
	Date        time.Time
	Description string
	// Type is snapper's single/pre/post, or Timeshift's tags (O, B, H...).
	Type string
}

// ErrUnreadable reports a tool's output cronen could not make sense of. The
// UI says "could not read the snapshot list"; it never shows a guess.
var ErrUnreadable = errors.New("could not read the snapshot list")

// ErrPermission reports that the tool refused to list for lack of rights.
var ErrPermission = errors.New("not permitted to list snapshots")

func unreadable(why string) error { return fmt.Errorf("%w: %s", ErrUnreadable, why) }

// Newest returns the date of the newest snapshot with a known date.
func Newest(snaps []Snapshot) (time.Time, bool) {
	var best time.Time
	for _, s := range snaps {
		if s.Date.After(best) {
			best = s.Date
		}
	}
	return best, !best.IsZero()
}

// Detection is which backend cronen uses.
type Detection struct {
	Tool Tool
	// Found is false when no backend is usable. That is a normal state:
	// the snapshot UI is hidden, with nothing said.
	Found bool
	// Missing is set when snapshot_tool names a tool that is not installed;
	// the housekeeping screen mentions it once.
	Missing string
}

// Choose picks the backend. configured is snapshot_tool ("", "timeshift" or
// "snapper"); with "" Timeshift wins when both are installed.
func Choose(configured string, hasTimeshift, hasSnapper bool) Detection {
	switch Tool(configured) {
	case Timeshift:
		if hasTimeshift {
			return Detection{Tool: Timeshift, Found: true}
		}
		return Detection{Missing: "snapshot_tool = \"timeshift\" but timeshift isn't installed"}
	case Snapper:
		if hasSnapper {
			return Detection{Tool: Snapper, Found: true}
		}
		return Detection{Missing: "snapshot_tool = \"snapper\" but snapper isn't installed"}
	}
	switch {
	case hasTimeshift:
		return Detection{Tool: Timeshift, Found: true}
	case hasSnapper:
		return Detection{Tool: Snapper, Found: true}
	}
	return Detection{}
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// StripANSI removes terminal escape sequences.
func StripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// ClassifyError turns a failed listing into ErrPermission when the tool's
// stderr says it lacked the rights, and returns err unchanged otherwise.
// Permission-denied output must never look like an empty list.
func ClassifyError(err error, stderr string) error {
	if err == nil {
		return nil
	}
	low := strings.ToLower(stderr + " " + err.Error())
	for _, hint := range []string{
		"permission", "no permissions", "not permitted", "denied", "must be root",
		"run as root", "as root", "root privileges", "need root", "needs root",
		"a password is required", "not allowed",
	} {
		if strings.Contains(low, hint) {
			return fmt.Errorf("%w: %s", ErrPermission, firstLine(stderr, err))
		}
	}
	return err
}

func firstLine(stderr string, err error) string {
	for l := range strings.Lines(StripANSI(stderr)) {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return err.Error()
}
