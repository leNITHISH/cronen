package archive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"time"

	"github.com/leNITHISH/cronen/internal/ops"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/system"
)

// ErrNoFakeroot reports that fakeroot (base-devel) isn't installed, so the
// archive database can't be synced as a normal user.
var ErrNoFakeroot = errors.New("fakeroot is not installed (it is in base-devel), so the archive database can't be synced as a normal user")

// ErrNoLastUpgrade reports that the day of the last full upgrade is unknown.
var ErrNoLastUpgrade = errors.New("the time of your last full upgrade is unknown (pacman.log has none), so there is no archive day to use")

// Input is what Prepare needs.
type Input struct {
	// Base is $XDG_CACHE_HOME/cronen/archive. Every install gets a fresh
	// 0700 directory under it.
	Base string
	// RealDB is the real database directory; its local/ is linked into the
	// archive directory so an install updates the real local database.
	RealDB string
	// RealConf is the contents of the real pacman.conf.
	RealConf []byte
	// LastUpgrade is the last full upgrade, from pacman.log.
	LastUpgrade time.Time
	// Now and Nonce name the install's directory; Nonce is random hex.
	Now   time.Time
	Nonce string
	Names []string
}

// Prepared is an archive directory that was synced and checked.
type Prepared struct {
	// Dir is the directory the install uses as --dbpath.
	Dir string
	// Day is the chosen snapshot, and Candidate holds how well it matches.
	Day       string
	Candidate Candidate
	// Tried are all the days that synced.
	Tried []Candidate
	// SyncErrors are the days that failed to sync, with why.
	SyncErrors map[string]error
	// Conf is the pacman.conf written into Dir, kept to compare right
	// before the hand-off.
	Conf   []byte
	Report Report
	// DryRun is the dry run command, and its result: pacman's output and
	// error text, and whether it failed.
	DryRun       []string
	DryOut       []byte
	DryStderr    []byte
	DryFailed    bool
	DryRunErr    error
	realDB       string
	archiveBase  string
	installNames []string
}

// Available reports whether fakeroot exists. It runs `fakeroot --version`,
// which reads nothing and changes nothing.
func Available(ctx context.Context, r system.Runner) error {
	_, _, err := r.Run(ctx, "fakeroot", "--version")
	switch {
	case errors.Is(err, system.ErrCommandNotFound):
		return ErrNoFakeroot
	case err != nil:
		return fmt.Errorf("fakeroot: %w", err)
	}
	return nil
}

// run runs argv through the runner after the guard has approved it.
func run(ctx context.Context, r system.Runner, g ops.Guard, argv []string) ([]byte, int, error) {
	if err := g.Verify(argv); err != nil {
		return nil, -1, err
	}
	return r.Run(ctx, argv[0], argv[1:]...)
}

// Prepare sets up and syncs an archive directory for each candidate day,
// chooses the best day, and dry-runs the install of names against it. It
// stops, with nothing created, when names are invalid, fakeroot is missing,
// the last upgrade is unknown, or the config can't be generated. Each
// directory is new (MkdirNew fails if it exists), mode 0700, holds a
// freshly generated pacman.conf (0600) and a symlink to the real local
// database. Nothing here runs as root.
func Prepare(ctx context.Context, r system.Runner, fsys system.FS, in Input) (Prepared, error) {
	var p Prepared
	if err := ops.CheckInstallNames(in.Names); err != nil {
		return p, err
	}
	if in.LastUpgrade.IsZero() {
		return p, ErrNoLastUpgrade
	}
	if err := Available(ctx, r); err != nil {
		return p, err
	}
	base := path.Clean(in.Base)
	guard := ops.Guard{RealDB: in.RealDB, ArchiveBase: base}

	// Generate every config first: a real pacman.conf we can't use stops
	// everything before any directory exists.
	days := Days(in.LastUpgrade)
	confs := make([][]byte, len(days))
	for i, d := range days {
		conf, rep, err := GenerateConf(in.RealConf, d)
		if err != nil {
			return p, err
		}
		confs[i], p.Report = conf, rep
	}

	if fi, err := fsys.Lstat(base); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		return p, fmt.Errorf("refusing %s: it is a symbolic link", base)
	}
	if err := fsys.MkdirAll(base, 0o700); err != nil {
		return p, fmt.Errorf("creating %s: %w", base, err)
	}
	id := in.Now.UTC().Format("20060102T150405Z") + "-" + in.Nonce
	if in.Nonce == "" {
		return p, errors.New("no nonce for the archive directory")
	}
	root := path.Join(base, id)
	if err := fsys.MkdirNew(root, 0o700); err != nil {
		return p, fmt.Errorf("creating %s: %w", root, err)
	}

	p.SyncErrors = map[string]error{}
	dirs := map[string]string{}
	for i, d := range days {
		day := d.Format(DayFormat)
		dir := path.Join(root, day)
		if err := setup(fsys, dir, in.RealDB, confs[i]); err != nil {
			return p, err
		}
		syncArgv, err := ops.ArchiveSyncArgv(dir)
		if err != nil {
			return p, err
		}
		if _, _, err := run(ctx, r, guard, syncArgv); err != nil {
			p.SyncErrors[day] = err
			continue
		}
		driftArgv, err := ops.ArchiveDriftArgv(dir)
		if err != nil {
			return p, err
		}
		out, code, err := run(ctx, r, guard, driftArgv)
		var differ []pacman.Differing
		var exitErr *system.ExitError
		switch {
		case err == nil:
			if differ, err = pacman.ParseQu(out); err != nil {
				p.SyncErrors[day] = err
				continue
			}
		case code == 1 && errors.As(err, &exitErr) && len(bytes.TrimSpace(out)) == 0 && len(bytes.TrimSpace(exitErr.Stderr)) == 0:
			// Nothing differs: pacman -Qu exits 1 with no output.
		default:
			p.SyncErrors[day] = err
			continue
		}
		dirs[day] = dir
		p.Tried = append(p.Tried, Candidate{Day: day, Differ: differ})
	}
	best, ok := Choose(p.Tried)
	if !ok {
		return p, fmt.Errorf("could not sync the archive for %s or %s: %w", days[0].Format(DayFormat), days[1].Format(DayFormat), firstErr(p.SyncErrors, days))
	}
	p.Candidate, p.Day, p.Dir = best, best.Day, dirs[best.Day]
	for i, d := range days {
		if d.Format(DayFormat) == best.Day {
			p.Conf = confs[i]
		}
	}
	p.installNames, p.realDB, p.archiveBase = in.Names, in.RealDB, base

	dry, err := ops.ArchiveDryArgv(p.Dir, in.Names)
	if err != nil {
		return p, err
	}
	p.DryRun = dry
	out, _, err := run(ctx, r, guard, dry)
	p.DryOut = out
	if err != nil {
		p.DryFailed, p.DryRunErr = true, err
		var exitErr *system.ExitError
		if errors.As(err, &exitErr) {
			p.DryStderr = exitErr.Stderr
		}
	}
	return p, nil
}

func firstErr(m map[string]error, days []time.Time) error {
	for _, d := range days {
		if err := m[d.Format(DayFormat)]; err != nil {
			return err
		}
	}
	return errors.New("no archive day synced")
}

// setup makes one archive directory: 0700, sync/, the local symlink and the
// config.
func setup(fsys system.FS, dir, realDB string, conf []byte) error {
	if err := fsys.MkdirNew(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	if err := fsys.MkdirNew(path.Join(dir, "sync"), 0o700); err != nil {
		return fmt.Errorf("creating %s/sync: %w", dir, err)
	}
	if err := fsys.Symlink(path.Join(realDB, "local"), path.Join(dir, "local")); err != nil {
		return fmt.Errorf("linking the local database: %w", err)
	}
	if err := fsys.WriteFile(ops.ArchiveConfPath(dir), conf, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", ops.ArchiveConfPath(dir), err)
	}
	return nil
}

// Recheck is run right before the option B hand-off. It refuses if the
// database lock exists (the install takes its lock inside the temporary
// directory, so it would not notice another pacman), or if the directory or
// config is no longer what Prepare made: a symbolic link, a mode other than
// 0700, or a config that differs from the one generated. root reads that
// config, so it is compared byte for byte.
func Recheck(fsys system.FS, p Prepared) error {
	lock := path.Join(p.realDB, "db.lck")
	if _, err := fsys.Stat(lock); err == nil {
		return fmt.Errorf("%s exists: another pacman is running (or crashed and left a stale lock). Option B can't see that lock, so cronen refuses; wait for it to end or remove the stale lock yourself", lock)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("checking %s: %w", lock, err)
	}
	fi, err := fsys.Lstat(p.Dir)
	switch {
	case err != nil:
		return fmt.Errorf("archive directory: %w", err)
	case !fi.IsDir() || fi.Mode()&fs.ModeSymlink != 0 || fi.Mode().Perm() != 0o700:
		return fmt.Errorf("archive directory %s is no longer a 0700 directory", p.Dir)
	}
	got, err := fsys.ReadFile(ops.ArchiveConfPath(p.Dir))
	if err != nil {
		return fmt.Errorf("reading the generated pacman.conf: %w", err)
	}
	if !bytes.Equal(got, p.Conf) {
		return fmt.Errorf("%s changed since cronen wrote it; refusing to let root read it", ops.ArchiveConfPath(p.Dir))
	}
	return nil
}

// InstallArgv is the hand-off for a prepared directory.
func (p Prepared) InstallArgv(priv []string) ([]string, error) {
	return ops.ArchiveInstallArgv(priv, p.Dir, p.installNames)
}

// Guard is the guard for this prepared directory's commands.
func (p Prepared) Guard() ops.Guard { return ops.Guard{RealDB: p.realDB, ArchiveBase: p.archiveBase} }

// ErrOff reports that archive_install is not set to true.
var ErrOff = errors.New("off: set archive_install = true in config.toml to enable this experimental option")

// Check reports why option B isn't available, or nil when it is: the config
// switch is off, the last full upgrade is unknown, or fakeroot is missing.
// The checks run in that order and the later ones are not made when an
// earlier one fails.
func Check(ctx context.Context, r system.Runner, enabled bool, lastUpgrade time.Time) error {
	switch {
	case !enabled:
		return ErrOff
	case lastUpgrade.IsZero():
		return ErrNoLastUpgrade
	}
	return Available(ctx, r)
}
