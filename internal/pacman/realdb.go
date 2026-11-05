package pacman

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/leNITHISH/cronen/internal/system"
)

// RealPending runs `pacman -Qu` against the real database, with no --dbpath,
// and returns the installed packages the database holds a different version
// of, leaving out the lines marked "[ignored]". It never syncs, so it is
// read-only.
//
// checkupdates syncs a copy of the database, which cronen reads with
// --dbpath. The real database, on the other hand, only has newer versions in
// it when someone ran `pacman -Sy` (or an AUR helper's equivalent) without
// -u: the sync stored them, and nothing upgraded to them. A result here is
// therefore the sign of a partial upgrade in the making.
//
// pacman -Qu exits 1 and prints nothing when nothing is pending.
func RealPending(ctx context.Context, r system.Runner) ([]Differing, error) {
	out, code, err := r.Run(ctx, "pacman", "-Qu")
	if code == 0 && err == nil {
		return ParseQu(out)
	}
	var exitErr *system.ExitError
	if code == 1 && errors.As(err, &exitErr) &&
		len(bytes.TrimSpace(out)) == 0 && len(bytes.TrimSpace(exitErr.Stderr)) == 0 {
		return nil, nil
	}
	return nil, fmt.Errorf("checking the package database: %w", err)
}

// AllPackages runs `pacman -Q` and returns every installed package with its
// version.
func AllPackages(ctx context.Context, r system.Runner) ([]Package, error) {
	out, _, err := r.Run(ctx, "pacman", "-Q")
	if err != nil {
		return nil, fmt.Errorf("listing installed packages: %w", err)
	}
	return ParseQm(out)
}
