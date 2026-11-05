package preflight

import (
	"fmt"
	"time"

	"github.com/leNITHISH/cronen/internal/risk"
)

// CheckSnapshot is raised by the snapshot helper, not by Check: see
// SnapshotProblem and Add.
const CheckSnapshot ID = "snapshot"

// SnapshotFacts is what the snapshot check looks at.
type SnapshotFacts struct {
	// Tool is the backend's name as the UI writes it ("Timeshift").
	Tool string
	Now  time.Time
	// Last is the date of the newest known snapshot, zero if none is known.
	Last time.Time
	// MaxAge is how old that may be before it is a warning.
	MaxAge time.Duration
	// FromRecords reports that only the snapshots cronen itself took are
	// known, because the tool could not be listed.
	FromRecords bool
	// Hook is the installed package that snapshots on every pacman
	// transaction, "" for none.
	Hook string
}

// SnapshotProblem reports a snapshot that is too old (warn) or unknown
// (info). ok is false when the newest snapshot is recent enough.
func SnapshotProblem(f SnapshotFacts) (Problem, bool) {
	known := ""
	if f.FromRecords {
		known = " Only the snapshots cronen took are known; the tool can't be listed without more rights."
	}
	hook := ""
	if f.Hook != "" {
		hook = " " + f.Hook + " snapshots on every pacman transaction."
	}
	if f.Last.IsZero() {
		return Problem{risk.Info, CheckSnapshot, fmt.Sprintf(
			"No %s snapshot is known: press S in housekeeping to take one before upgrading.%s%s", f.Tool, known, hook)}, true
	}
	age := f.Now.Sub(f.Last)
	if f.MaxAge > 0 && age > f.MaxAge {
		days := int(age / (24 * time.Hour))
		return Problem{risk.Warn, CheckSnapshot, fmt.Sprintf(
			"The newest known %s snapshot is %d days old (limit %d days).%s%s",
			f.Tool, days, int(f.MaxAge/(24*time.Hour)), known, hook)}, true
	}
	return Problem{}, false
}
