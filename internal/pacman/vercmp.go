package pacman

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/leNITHISH/cronen/internal/system"
)

// Vercmp compares two package versions with pacman's vercmp and returns -1
// if a is older than b, 0 if they are equal, and 1 if a is newer. Arch
// version comparison (epochs, pkgrel, alpha suffixes) is subtle, so this
// always shells out instead of reimplementing it.
func Vercmp(ctx context.Context, r system.Runner, a, b string) (int, error) {
	out, _, err := r.Run(ctx, "vercmp", a, b)
	if err != nil {
		return 0, fmt.Errorf("vercmp %s %s: %w", a, b, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("vercmp %s %s: unexpected output %q", a, b, out)
	}
	switch {
	case n < 0:
		return -1, nil
	case n > 0:
		return 1, nil
	}
	return 0, nil
}
