package pacman

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/leNITHISH/cronen/internal/system"
)

// ErrCheckupdatesNotFound reports that checkupdates isn't installed. Its
// message is meant to be shown to the user as is.
var ErrCheckupdatesNotFound = errors.New("checkupdates not found — install pacman-contrib")

// Update is one pending repo update reported by checkupdates.
type Update struct {
	Name       string
	OldVersion string
	NewVersion string
}

// ParseCheckupdates parses checkupdates output, one "name oldver -> newver"
// line per package. Blank lines are skipped; any other line that doesn't
// have that shape is an error.
func ParseCheckupdates(data []byte) ([]Update, error) {
	var updates []Update
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 4 || f[2] != "->" {
			return nil, fmt.Errorf("checkupdates: line %d: unexpected format %q", n, line)
		}
		updates = append(updates, Update{Name: f[0], OldVersion: f[1], NewVersion: f[3]})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("checkupdates: %w", err)
	}
	return updates, nil
}

// CheckUpdates runs checkupdates and returns the pending repo updates.
// checkupdates syncs a temporary copy of the database, so this never causes
// a partial upgrade. Exit code 2 means there are no updates and returns an
// empty list; exit code 1, or any other failure, is an error. A missing
// checkupdates reports ErrCheckupdatesNotFound.
func CheckUpdates(ctx context.Context, r system.Runner) ([]Update, error) {
	out, code, err := r.Run(ctx, "checkupdates")
	switch {
	case code == 0:
		return ParseCheckupdates(out)
	case code == 2:
		return nil, nil
	case errors.Is(err, system.ErrCommandNotFound):
		return nil, ErrCheckupdatesNotFound
	case code < 0:
		return nil, fmt.Errorf("running checkupdates: %w", err)
	default:
		return nil, fmt.Errorf("checkupdates failed: %w", err)
	}
}
