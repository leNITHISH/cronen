package fixtures

import (
	"github.com/leNITHISH/cronen/internal/system"
)

// M19, the snapshot helper. Hand-written, pending capture from a real
// system (see testdata/HAND_WRITTEN.txt). Timeshift and snapper are not
// installed on the maintainer's machine, so the default Runner() has
// neither: "no tool detected" is the state every older test runs in.

// Fixture file names for the snapshot tools.
const (
	TimeshiftList      = "timeshift_list.txt"
	TimeshiftListEmpty = "timeshift_list_empty.txt"
	TimeshiftJSON      = "timeshift.json"
	SnapperList        = "snapper_list.txt"
	SnapperDenied      = "snapper_list_denied.stderr"
	SudoListDenied     = "sudo_timeshift_list_denied.stderr"

	// TimeshiftConfigPath is where Timeshift keeps its configuration.
	TimeshiftConfigPath = "/etc/timeshift/timeshift.json"
	// SnapperConfig is the snapper config the fixtures list.
	SnapperConfig = "root"
)

// SnapshotKit chooses what the fake system has installed.
type SnapshotKit struct {
	Timeshift, Snapper bool
	// Hook is the installed pacman hook package ("timeshift-autosnap" or
	// "snap-pac"), "" for none.
	Hook string
	// SnapperDenied makes `snapper list` fail the way it does for a user
	// the config does not allow.
	SnapperDenied bool
	// SudoList answers `sudo -n timeshift --list` with the fixture list;
	// SudoDenied with sudo's "a password is required".
	SudoList, SudoDenied bool
}

// WithSnapshots registers the snapshot tools on r as kit says. A tool that
// is not installed fails with system.ErrCommandNotFound, like a missing
// command. `timeshift --list` as the user is never registered: it needs
// root, and the FakeRunner refuses it.
func WithSnapshots(r *system.FakeRunner, kit SnapshotKit) *system.FakeRunner {
	for _, t := range []struct {
		name string
		on   bool
		out  string
	}{{"timeshift", kit.Timeshift, "Timeshift v25.07.7\n"}, {"snapper", kit.Snapper, "snapper 0.12.2\n"}} {
		if t.on {
			r.Set(t.name+" --version", system.FakeResult{Stdout: []byte(t.out)})
		} else {
			r.Set(t.name+" --version", system.FakeResult{Err: system.ErrCommandNotFound})
		}
	}
	for _, h := range []string{"timeshift-autosnap", "snap-pac"} {
		if kit.Hook == h {
			r.Set("pacman -Qq -- "+h, system.FakeResult{Stdout: []byte(h + "\n")})
		} else {
			r.Set("pacman -Qq -- "+h, system.FakeResult{Code: 1, Stderr: []byte("error: package '" + h + "' was not found\n")})
		}
	}
	if kit.Snapper {
		if kit.SnapperDenied {
			r.Set("snapper -c "+SnapperConfig+" list", system.FakeResult{Code: 1, Stderr: Bytes(SnapperDenied)})
		} else {
			r.Set("snapper -c "+SnapperConfig+" list", system.FakeResult{Stdout: Bytes(SnapperList)})
		}
	}
	switch {
	case kit.SudoList:
		r.Set("sudo -n timeshift --list", system.FakeResult{Stdout: Bytes(TimeshiftList)})
	case kit.SudoDenied:
		r.Set("sudo -n timeshift --list", system.FakeResult{Code: 1, Stderr: Bytes(SudoListDenied)})
	}
	return r
}

// WithTimeshiftConfig adds /etc/timeshift/timeshift.json to fsys.
func WithTimeshiftConfig(fsys *system.FakeFS) *system.FakeFS {
	_ = fsys.WriteFile(TimeshiftConfigPath, Bytes(TimeshiftJSON), 0o644)
	return fsys
}
