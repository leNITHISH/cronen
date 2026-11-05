package summary

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/preflight"
	"github.com/leNITHISH/cronen/internal/snapshot"
	"github.com/leNITHISH/cronen/internal/system"
)

// TimeshiftConfigPath is Timeshift's configuration file. Needs checking on
// Arch: the path and the field names (see internal/snapshot).
const TimeshiftConfigPath = "/etc/timeshift/timeshift.json"

// SnapshotSources is the system access the snapshot helper needs.
type SnapshotSources struct {
	Runner   system.Runner
	FS       system.FS
	Disk     system.Disk
	Config   config.Config
	State    config.State
	Location *time.Location
}

// BackupInfo is the Timeshift backup device, for rsync mode.
type BackupInfo struct {
	Mounted bool
	Path    string
	Free    int64
}

// SnapshotInfo is what the snapshot helper found. The zero value is "no
// tool detected": a normal state in which the snapshot UI is hidden and
// nothing is said.
type SnapshotInfo struct {
	Detect snapshot.Detection
	// Hooks are the installed packages that snapshot on every pacman
	// transaction.
	Hooks []string
	// Snapshots are what the tool listed, newest last; Listed is set when
	// it was listed at all.
	Snapshots []snapshot.Snapshot
	Listed    bool
	// Note says why the tool was not listed, when that is expected
	// (Timeshift without snapshot_list_sudo, or no permission). It is not an
	// error.
	Note string
	// ListErr is a listing that failed or could not be read. Unlike Note it
	// is a failed source: the screen shows "could not read the snapshot
	// list" and --check exits 1.
	ListErr error
	// Records are the snapshots cronen took with this tool.
	Records []config.SnapshotRecord
	// Last is the time of the newest known snapshot (zero: none known),
	// and LastFromRecords says it comes from cronen's own records only.
	Last            time.Time
	LastFromRecords bool
	// Mode is Timeshift's "rsync" or "btrfs", "" when
	// /etc/timeshift/timeshift.json was not readable; ConfigErr says why
	// when it exists but couldn't be understood.
	Mode      string
	ConfigErr error
	Backup    BackupInfo
	// Config is the snapper config in use.
	SnapperConfig string
	// ListSudo reports snapshot_list_sudo.
	ListSudo bool
}

// Found reports a usable backend.
func (i SnapshotInfo) Found() bool { return i.Detect.Found }

// Tool is the backend, "" when none was found.
func (i SnapshotInfo) Tool() snapshot.Tool { return i.Detect.Tool }

// Hook is the first installed pacman hook package, "" for none.
func (i SnapshotInfo) Hook() string {
	if len(i.Hooks) > 0 {
		return i.Hooks[0]
	}
	return ""
}

// DefaultTake says whether "take a snapshot first" starts ticked, for
// snapshot_before_upgrade = mode. "ask" ticks it when a backend was found
// and no hook already snapshots; "always" ticks it whenever a backend was
// found; "never" hides the option altogether.
func (i SnapshotInfo) DefaultTake(mode string) bool {
	if !i.Found() {
		return false
	}
	switch mode {
	case config.SnapshotAlways:
		return true
	case config.SnapshotNever:
		return false
	}
	return i.Hook() == ""
}

// Offered reports whether the upgrade modal shows the snapshot line.
func (i SnapshotInfo) Offered(mode string) bool {
	return i.Found() && mode != config.SnapshotNever
}

// Deletable reports whether the listed snapshots can be deleted and
// restored by name (the tool listed them).
func (i SnapshotInfo) Deletable() bool { return i.Found() && i.Listed }

// Installed reports whether name exists as a command, by asking it for its
// version: a command that can't be started is not installed, any other
// outcome (even a non-zero exit) means it is. Anything the runner can't
// start counts as absent, so a missing tool is a quiet state.
func toolInstalled(ctx context.Context, r system.Runner, name string) bool {
	_, _, err := r.Run(ctx, name, "--version")
	if err == nil {
		return true
	}
	var exit *system.ExitError
	return errors.As(err, &exit)
}

// hookInstalled asks pacman whether a hook package is installed.
func hookInstalled(ctx context.Context, r system.Runner, pkg string) bool {
	_, _, err := r.Run(ctx, "pacman", "-Qq", "--", pkg)
	return err == nil
}

// LoadSnapshots finds the backend, the hooks and the snapshots. The error
// is only a listing that failed or could not be read; the rest of the
// info is still returned. No backend is no error.
func LoadSnapshots(ctx context.Context, src SnapshotSources) (SnapshotInfo, error) {
	cfg := src.Config
	if !cfg.Snapshots || src.Runner == nil {
		return SnapshotInfo{}, nil
	}
	var hasTS, hasSn bool
	if cfg.SnapshotTool != string(snapshot.Snapper) {
		hasTS = toolInstalled(ctx, src.Runner, "timeshift")
	}
	if cfg.SnapshotTool != string(snapshot.Timeshift) && (cfg.SnapshotTool != "" || !hasTS) {
		hasSn = toolInstalled(ctx, src.Runner, "snapper")
	}
	info := SnapshotInfo{Detect: snapshot.Choose(cfg.SnapshotTool, hasTS, hasSn), SnapperConfig: cfg.SnapperConfig, ListSudo: cfg.SnapshotListSudo}
	if !info.Found() {
		return info, nil
	}
	tool := info.Tool()
	loc := src.Location
	if loc == nil {
		loc = time.Local
	}

	for _, h := range snapshot.Hooks {
		if hookInstalled(ctx, src.Runner, h) {
			info.Hooks = append(info.Hooks, h)
		}
	}
	for _, r := range src.State.Snapshots() {
		if r.Tool == string(tool) {
			info.Records = append(info.Records, r)
		}
	}

	var listErr error
	argv, ok, err := snapshot.ListArgv(tool, cfg.SnapperConfig, cfg.SnapshotListSudo)
	switch {
	case err != nil:
		listErr = err
	case !ok:
		info.Note = "Timeshift can't be listed without root, so only the snapshots cronen took are shown. snapshot_list_sudo = true allows `sudo -n timeshift --list`, which only reads."
	default:
		// The only command here that may be privileged; check it anyway.
		if err := (snapshot.Guard{Config: cfg.SnapperConfig}).Verify(argv); err != nil {
			listErr = err
			break
		}
		out, _, runErr := src.Runner.Run(ctx, argv[0], argv[1:]...)
		if runErr != nil {
			var stderr string
			var exit *system.ExitError
			if errors.As(runErr, &exit) {
				stderr = string(exit.Stderr)
			}
			cls := snapshot.ClassifyError(runErr, stderr)
			if errors.Is(cls, snapshot.ErrPermission) {
				info.Note = permissionNote(tool, cls)
			} else {
				listErr = fmt.Errorf("%s: %w", strings.Join(argv, " "), cls)
			}
			break
		}
		var snaps []snapshot.Snapshot
		if tool == snapshot.Timeshift {
			snaps, err = snapshot.ParseTimeshiftList(out, loc)
		} else {
			snaps, err = snapshot.ParseSnapperList(out, loc)
		}
		if err != nil {
			listErr = err
			break
		}
		info.Snapshots, info.Listed = snaps, true
	}
	info.ListErr = listErr

	if tool == snapshot.Timeshift {
		loadTimeshiftConfig(&info, src)
	}

	if d, ok := snapshot.Newest(info.Snapshots); ok {
		info.Last = d
	}
	for _, r := range info.Records {
		if r.At.After(info.Last) {
			info.Last, info.LastFromRecords = r.At, true
		}
	}
	if !info.Listed && !info.Last.IsZero() {
		info.LastFromRecords = true
	}
	if listErr != nil {
		return info, fmt.Errorf("snapshots: %w", listErr)
	}
	return info, nil
}

func permissionNote(tool snapshot.Tool, err error) string {
	why := strings.TrimPrefix(err.Error(), snapshot.ErrPermission.Error()+": ")
	if tool == snapshot.Snapper {
		return "snapper refused to list for your user (" + why + "): add yourself to ALLOW_USERS or ALLOW_GROUPS in the snapper config to list. Only the snapshots cronen took are shown."
	}
	return "`sudo -n timeshift --list` was refused (" + why + "): sudo needs a passwordless rule for exactly that command. Only the snapshots cronen took are shown."
}

// loadTimeshiftConfig reads the mode and, for rsync mode with the backup
// device mounted, its free space. Neither is needed for anything else, so
// failures only leave the fields empty.
func loadTimeshiftConfig(info *SnapshotInfo, src SnapshotSources) {
	if src.FS == nil {
		return
	}
	data, err := src.FS.ReadFile(TimeshiftConfigPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			info.ConfigErr = err
		}
		return
	}
	c, err := snapshot.ParseTimeshiftConfig(data)
	if err != nil {
		info.ConfigErr = err
		return
	}
	info.Mode = c.Mode
	if c.Mode != "rsync" || src.Disk == nil {
		return
	}
	mounts, err := src.FS.ReadFile(preflight.MountsPath)
	if err != nil {
		return
	}
	if mp, ok := snapshot.BackupMount(mounts, c.BackupUUID); ok {
		if u, err := src.Disk.Statfs(mp); err == nil {
			info.Backup = BackupInfo{Mounted: true, Path: mp, Free: u.FreeBytes}
		}
	}
}

// SnapshotProblem is the preflight problem for the newest snapshot's age,
// if any. No backend means no problem, and an unreadable listing is
// reported as a failed source instead of a guess about age.
func SnapshotProblem(i SnapshotInfo, cfg config.Config, now time.Time) (preflight.Problem, bool) {
	if !i.Found() || i.ListErr != nil {
		return preflight.Problem{}, false
	}
	return preflight.SnapshotProblem(preflight.SnapshotFacts{
		Tool: i.Tool().Title(), Now: now, Last: i.Last,
		MaxAge:      time.Duration(cfg.SnapshotMaxAgeDays) * 24 * time.Hour,
		FromRecords: i.LastFromRecords, Hook: i.Hook(),
	})
}

// AddSnapshot adds the snapshot problem to the preflight problems, in
// order.
func AddSnapshot(ps []preflight.Problem, i SnapshotInfo, cfg config.Config, now time.Time) []preflight.Problem {
	if p, ok := SnapshotProblem(i, cfg, now); ok {
		return preflight.Add(ps, p)
	}
	return ps
}

// SnapshotRecord is the state record of a snapshot cronen has just taken.
func SnapshotRecord(tool snapshot.Tool, comment string, at time.Time) config.SnapshotRecord {
	return config.SnapshotRecord{At: at, Tool: string(tool), Comment: comment}
}
