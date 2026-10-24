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

// Package is an installed package and its version.
type Package struct {
	Name    string
	Version string
}

// ParseQm parses `pacman -Qm` output, one "name version" line per package.
// Blank lines are skipped; any other line that doesn't have that shape is an
// error.
func ParseQm(data []byte) ([]Package, error) {
	var pkgs []Package
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 {
			return nil, fmt.Errorf("pacman -Qm: line %d: unexpected format %q", n, line)
		}
		pkgs = append(pkgs, Package{Name: f[0], Version: f[1]})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("pacman -Qm: %w", err)
	}
	return pkgs, nil
}

// ForeignPackages runs `pacman -Qm` and returns the installed packages that
// aren't in any sync database (usually AUR packages). pacman exits 1 with no
// output when nothing matches, so that case returns an empty list.
func ForeignPackages(ctx context.Context, r system.Runner) ([]Package, error) {
	out, code, err := r.Run(ctx, "pacman", "-Qm")
	if code == 0 {
		return ParseQm(out)
	}
	var exitErr *system.ExitError
	if code == 1 && errors.As(err, &exitErr) &&
		len(bytes.TrimSpace(out)) == 0 && len(bytes.TrimSpace(exitErr.Stderr)) == 0 {
		return nil, nil
	}
	return nil, fmt.Errorf("listing foreign packages: %w", err)
}

// ParseQq parses `pacman -Qq` output, one package name per line, into a set.
func ParseQq(data []byte) map[string]bool {
	names := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if name := strings.TrimSpace(sc.Text()); name != "" {
			names[name] = true
		}
	}
	return names
}

// InstalledNames runs `pacman -Qq` and returns the names of every installed
// package.
func InstalledNames(ctx context.Context, r system.Runner) (map[string]bool, error) {
	out, _, err := r.Run(ctx, "pacman", "-Qq")
	if err != nil {
		return nil, fmt.Errorf("listing installed packages: %w", err)
	}
	return ParseQq(out), nil
}
