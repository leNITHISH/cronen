package pacman

import (
	"context"
	"fmt"

	"github.com/leNITHISH/cronen/internal/system"
)

// InstalledPackages runs `pacman -Q` and returns every installed package
// with its version. It has the same format as `pacman -Qm`.
func InstalledPackages(ctx context.Context, r system.Runner) ([]Package, error) {
	out, _, err := r.Run(ctx, "pacman", "-Q")
	if err != nil {
		return nil, fmt.Errorf("listing installed packages: %w", err)
	}
	return ParseQm(out)
}
