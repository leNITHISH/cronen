package summary

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/system"
)

// PacnewDiff is one .pacnew or .pacsave file and how it differs from the
// file in use.
type PacnewDiff struct {
	// File is the path pacdiff -o printed, Original the file it belongs to.
	File, Original string
	// Diff is `diff -u Original File`; empty when identical.
	Diff string
	// NeedsRoot reports that one of the files can't be read by the user.
	NeedsRoot bool
	// Err is any other reason there is no diff.
	Err error
}

// LoadPacnewDiffs runs `diff -u <original> <file>` for each file pacdiff -o
// returned. diff exits 1 when the files differ, which is a result; a file
// the user can't read is "needs root to view", not an error.
func LoadPacnewDiffs(ctx context.Context, r system.Runner, files []string) []PacnewDiff {
	out := make([]PacnewDiff, 0, len(files))
	for _, f := range files {
		d := PacnewDiff{File: f, Original: f}
		for _, suffix := range []string{".pacnew", ".pacsave"} {
			if o, ok := strings.CutSuffix(f, suffix); ok {
				d.Original = o
			}
		}
		if !pacmanSafePath(f) || !pacmanSafePath(d.Original) {
			d.Err = fmt.Errorf("not diffing %q", f)
			out = append(out, d)
			continue
		}
		stdout, code, err := r.Run(ctx, "diff", "-u", d.Original, f)
		var exitErr *system.ExitError
		switch {
		case err == nil:
		case errors.As(err, &exitErr) && code == 1:
			// The files differ.
		case errors.As(err, &exitErr) && strings.Contains(string(exitErr.Stderr), "Permission denied"):
			d.NeedsRoot = true
		default:
			d.Err = err
		}
		if d.Err == nil && !d.NeedsRoot {
			d.Diff = string(stdout)
		}
		out = append(out, d)
	}
	return out
}

// pacmanSafePath accepts absolute paths that can't be taken for an option.
func pacmanSafePath(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.ContainsAny(p, "\x00\n")
}

// CacheInfo is the size of the package cache.
type CacheInfo struct {
	Dirs  []string
	Bytes int64
	// Files counts package files; signatures are not counted.
	Files int
}

// LoadCache adds up the package files in pacman's CacheDir directories
// (from pacman.conf, else the default) and in extra, a list of globs whose
// leading ~/ is expanded with home.
func LoadCache(fsys system.FS, extra []string, home string) (CacheInfo, error) {
	conf, err := fsys.ReadFile(pacman.ConfPath)
	dirs := []string{pacman.DefaultCacheDir}
	if err == nil {
		dirs = pacman.CacheDirs(conf)
	}
	for _, g := range extra {
		if rest, ok := strings.CutPrefix(g, "~/"); ok {
			if home == "" {
				continue
			}
			g = filepath.Join(home, rest)
		}
		matches, err := fsys.Glob(g)
		if err != nil {
			continue
		}
		dirs = append(dirs, matches...)
	}
	var info CacheInfo
	seen := map[string]bool{}
	var firstErr error
	for _, d := range dirs {
		d = filepath.Clean(d)
		if seen[d] {
			continue
		}
		seen[d] = true
		entries, err := fsys.ReadDir(d)
		if err != nil {
			if firstErr == nil && !errors.Is(err, fs.ErrNotExist) {
				firstErr = fmt.Errorf("package cache %s: %w", d, err)
			}
			continue
		}
		info.Dirs = append(info.Dirs, d)
		for _, e := range entries {
			if pacman.IsPackageFile(e.Name()) {
				info.Files++
				info.Bytes += e.Size()
			}
		}
	}
	return info, firstErr
}

// FailedUnits are the units systemd reports as failed.
type FailedUnits struct {
	System, User []string
}

// Count is the number of failed units.
func (f FailedUnits) Count() int { return len(f.System) + len(f.User) }

// LoadFailedUnits runs systemctl --failed for the system and the user
// manager. A user manager that isn't running (no session bus) is not an
// error: the user list stays empty.
func LoadFailedUnits(ctx context.Context, r system.Runner) (FailedUnits, error) {
	var f FailedUnits
	out, _, err := r.Run(ctx, "systemctl", "--failed", "--plain", "--no-legend")
	if err != nil {
		return f, fmt.Errorf("systemctl --failed: %w", err)
	}
	f.System = parseUnits(out)
	if out, _, err := r.Run(ctx, "systemctl", "--user", "--failed", "--plain", "--no-legend"); err == nil {
		f.User = parseUnits(out)
	}
	return f, nil
}

func parseUnits(data []byte) []string {
	var units []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) > 0 {
			units = append(units, strings.TrimPrefix(f[0], "●"))
		}
	}
	slices.Sort(units)
	return units
}
