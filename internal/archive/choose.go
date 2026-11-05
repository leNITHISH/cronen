package archive

import (
	"fmt"
	"strings"

	"github.com/leNITHISH/cronen/internal/pacman"
)

// Candidate is one archive day that was synced, and the installed packages
// whose version differs from that snapshot.
type Candidate struct {
	Day    string
	Differ []pacman.Differing
}

// Choose keeps the candidate with the fewest differing packages. A tie
// goes to the earlier one (the day of the upgrade itself). ok is false
// with no candidates.
func Choose(cands []Candidate) (best Candidate, ok bool) {
	for i, c := range cands {
		if i == 0 || len(c.Differ) < len(best.Differ) {
			best, ok = c, true
		}
	}
	return best, ok
}

// Confidence is the line the modal shows about how well the snapshot
// matches the system.
func (c Candidate) Confidence() string {
	switch n := len(c.Differ); n {
	case 0:
		return "your system matches " + c.Day + " exactly"
	case 1:
		return "your system doesn't exactly match " + c.Day + ": 1 package differs"
	default:
		return fmt.Sprintf("your system doesn't exactly match %s: %d packages differ", c.Day, n)
	}
}

// DifferList is "name old → new" for each differing package.
func (c Candidate) DifferList() []string {
	out := make([]string, len(c.Differ))
	for i, d := range c.Differ {
		out[i] = d.Name + " " + d.OldVersion + " → " + d.NewVersion
	}
	return out
}

func (c Candidate) String() string {
	return c.Day + " (" + strings.Join(c.DifferList(), ", ") + ")"
}
