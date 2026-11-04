package pacman

import (
	"context"
	"fmt"

	"github.com/leNITHISH/cronen/internal/system"
)

// NativePackages runs `pacman -Qn` and returns the installed packages that
// are in a sync database, with their versions. It has the same format as
// `pacman -Qm`.
func NativePackages(ctx context.Context, r system.Runner) ([]Package, error) {
	out, _, err := r.Run(ctx, "pacman", "-Qn")
	if err != nil {
		return nil, fmt.Errorf("listing native packages: %w", err)
	}
	return ParseQm(out)
}
