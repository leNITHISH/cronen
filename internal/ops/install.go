package ops

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode"
)

// This file builds every command the install helper ("I need X now") runs.
// There are exactly these, and nothing else can be produced:
//
//	option A, dry run   pacman -Sup --dbpath <checkupdates db> ... -- X
//	option A, hand-off  <priv> pacman -Syu -- X     or     paru|yay -Syu -- X
//	option B, sync      fakeroot -- pacman -Sy --dbpath <dir> --config <dir>/pacman.conf ...
//	option B, drift     pacman -Qu --dbpath <dir> --config <dir>/pacman.conf ...
//	option B, dry run   pacman -Sp --dbpath <dir> --config <dir>/pacman.conf ... -- X
//	option B, hand-off  <priv> pacman -S --dbpath <dir> --config <dir>/pacman.conf -- X
//
// "pacman -Sy X" and "pacman -S X" against the real database (a partial
// upgrade) are not among them: no builder here makes them, and Guard.Verify
// refuses them if some other code path ever hands it one.

// MaxInstallNames caps how many packages one install takes.
const MaxInstallNames = 20

// ArchiveConfName is the pacman.conf cronen writes into an archive
// directory.
const ArchiveConfName = "pacman.conf"

// privileges are the programs a hand-off may be prefixed with.
var privileges = []string{"sudo", "doas", "run0"}

// helpers are the AUR helpers option A may use.
var helpers = []string{"paru", "yay"}

// CheckInstallNames is CheckNames plus a cap and a duplicate check.
func CheckInstallNames(names []string) error {
	if err := CheckNames(names); err != nil {
		return err
	}
	if len(names) > MaxInstallNames {
		return fmt.Errorf("too many packages (%d, at most %d)", len(names), MaxInstallNames)
	}
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			return fmt.Errorf("package %q is listed twice", n)
		}
		seen[n] = true
	}
	return nil
}

// checkPath refuses a directory or file path that is not absolute and
// clean, or holds a control character: it goes into argv unquoted.
func checkPath(p string) error {
	switch {
	case p == "" || !path.IsAbs(p):
		return fmt.Errorf("refusing path %q: not absolute", p)
	case path.Clean(p) != strings.TrimSuffix(p, "/") && p != "/":
		return fmt.Errorf("refusing path %q: not clean", p)
	case strings.IndexFunc(p, unicode.IsControl) >= 0:
		return fmt.Errorf("refusing path %q: control character", p)
	}
	return nil
}

func checkPriv(priv []string) error {
	if len(priv) == 0 {
		return errors.New("no privilege command")
	}
	if !slices.Contains(privileges, priv[0]) {
		return fmt.Errorf("refusing privilege command %q", priv[0])
	}
	for _, a := range priv {
		if a == "" || strings.IndexFunc(a, unicode.IsControl) >= 0 {
			return fmt.Errorf("refusing privilege command argument %q", a)
		}
	}
	return nil
}

// UpgradeDryArgv is option A's dry run, as the user, against the database
// checkupdates synced: what -Syu plus names would install. With no names it
// is the plain transaction preview.
func UpgradeDryArgv(db string, names []string) ([]string, error) {
	if err := checkPath(db); err != nil {
		return nil, err
	}
	if len(names) > 0 {
		if err := CheckInstallNames(names); err != nil {
			return nil, err
		}
	}
	argv := []string{"pacman", "-Sup", "--dbpath", db, "--logfile", "/dev/null", "--noconfirm", "--print-format", "%n %v %r %s"}
	if len(names) > 0 {
		argv = append(argv, "--")
		argv = append(argv, names...)
	}
	return argv, nil
}

// UpgradeInstallArgv is option A's hand-off: the full upgrade plus names.
// With helper set ("paru" or "yay") the helper runs it, without the
// privilege command (helpers escalate themselves) and with AUR names
// allowed; otherwise it is <priv> pacman -Syu -- names. The -u is always
// there: there is no way to ask this function for a bare -Sy.
func UpgradeInstallArgv(priv []string, helper string, names []string) ([]string, error) {
	if err := CheckInstallNames(names); err != nil {
		return nil, err
	}
	if helper != "" {
		if !slices.Contains(helpers, helper) {
			return nil, fmt.Errorf("refusing AUR helper %q", helper)
		}
		return append([]string{helper, "-Syu", "--"}, names...), nil
	}
	if err := checkPriv(priv); err != nil {
		return nil, err
	}
	argv := append(slices.Clone(priv), "pacman", "-Syu", "--")
	return append(argv, names...), nil
}

// ArchiveConfPath is the pacman.conf inside an archive directory.
func ArchiveConfPath(dir string) string { return path.Join(dir, ArchiveConfName) }

func archiveOpts(dir string) ([]string, error) {
	if err := checkPath(dir); err != nil {
		return nil, err
	}
	return []string{"--dbpath", dir, "--config", ArchiveConfPath(dir)}, nil
}

// ArchiveSyncArgv syncs the archive directory's own database as the user,
// under fakeroot like checkupdates does. It only ever refreshes the
// temporary database that --dbpath names, and the temporary config that
// --config names: never the real one.
func ArchiveSyncArgv(dir string) ([]string, error) {
	o, err := archiveOpts(dir)
	if err != nil {
		return nil, err
	}
	argv := append([]string{"fakeroot", "--", "pacman", "-Sy"}, o...)
	return append(argv, "--logfile", "/dev/null"), nil
}

// ArchiveDriftArgv lists the installed packages that differ from the
// archive snapshot synced into dir.
func ArchiveDriftArgv(dir string) ([]string, error) {
	o, err := archiveOpts(dir)
	if err != nil {
		return nil, err
	}
	argv := append([]string{"pacman", "-Qu"}, o...)
	return append(argv, "--logfile", "/dev/null"), nil
}

// ArchiveDryArgv is option B's dry run: what installing names from the
// archive snapshot would do. It is -Sp: no refresh, no upgrade.
func ArchiveDryArgv(dir string, names []string) ([]string, error) {
	o, err := archiveOpts(dir)
	if err != nil {
		return nil, err
	}
	if err := CheckInstallNames(names); err != nil {
		return nil, err
	}
	argv := append([]string{"pacman", "-Sp"}, o...)
	argv = append(argv, "--logfile", "/dev/null", "--noconfirm", "--print-format", "%n %v %r %s", "--")
	return append(argv, names...), nil
}

// ArchiveInstallArgv is option B's hand-off: -S against the temporary
// database, with the temporary config, which writes the real local database
// through the symlink in dir. No -y, no -u.
func ArchiveInstallArgv(priv []string, dir string, names []string) ([]string, error) {
	if err := checkPriv(priv); err != nil {
		return nil, err
	}
	o, err := archiveOpts(dir)
	if err != nil {
		return nil, err
	}
	if err := CheckInstallNames(names); err != nil {
		return nil, err
	}
	argv := append(slices.Clone(priv), "pacman", "-S")
	argv = append(argv, o...)
	argv = append(argv, "--")
	return append(argv, names...), nil
}

// Guard verifies install commands before they run. It is the second
// line of defence behind the builders: whatever produced an argv, nothing
// that could be a partial upgrade gets through.
type Guard struct {
	// RealDB is the real pacman database directory (DBPath, usually
	// /var/lib/pacman).
	RealDB string
	// ArchiveBase is the directory every archive directory lives under.
	ArchiveBase string
}

// Verify returns nil only for the exact forms listed at the top of this
// file. It parses the argv the way pacman would (short option clusters,
// the value after each long option, "--" before the targets), so
// spellings such as -Syy, -yS, --sync --refresh or -Sy -- X are all seen
// for what they are.
func (g Guard) Verify(argv []string) error {
	rest := argv
	var priv, fakeroot bool
	if len(rest) > 0 && slices.Contains(privileges, path.Base(rest[0])) {
		priv, rest = true, rest[1:]
	}
	if len(rest) > 0 && path.Base(rest[0]) == "fakeroot" {
		fakeroot, rest = true, rest[1:]
		if len(rest) > 0 && rest[0] == "--" {
			rest = rest[1:]
		}
	}
	if len(rest) == 0 {
		return errors.New("refusing an empty install command")
	}
	prog := path.Base(rest[0])
	rest = rest[1:]
	for _, a := range argv {
		if a == "" || strings.IndexFunc(a, unicode.IsControl) >= 0 {
			return fmt.Errorf("refusing command argument %q", a)
		}
	}

	if slices.Contains(helpers, prog) {
		// paru|yay -Syu -- names, with no privilege command and no
		// fakeroot.
		if priv || fakeroot || len(rest) < 3 || rest[0] != "-Syu" || rest[1] != "--" {
			return fmt.Errorf("refusing %s command %q: only `%s -Syu -- names` is allowed", prog, strings.Join(argv, " "), prog)
		}
		return CheckInstallNames(rest[2:])
	}
	if prog != "pacman" {
		return fmt.Errorf("refusing to run %q", prog)
	}
	return g.verifyPacman(argv, rest, priv, fakeroot)
}

func (g Guard) verifyPacman(argv, rest []string, priv, fakeroot bool) error {
	refuse := func(why string) error {
		return fmt.Errorf("refusing %q: %s", strings.Join(argv, " "), why)
	}
	var (
		op                        byte
		y, u, p                   bool
		dbpath, conf, logfile     string
		haveDB, haveConf, haveLog bool
		printFormat               string
		names                     []string
		sawDashDash               bool
	)
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if sawDashDash {
			names = append(names, a)
			continue
		}
		value := func() (string, bool) {
			if i+1 >= len(rest) {
				return "", false
			}
			i++
			return rest[i], true
		}
		switch {
		case a == "--":
			sawDashDash = true
		case a == "--dbpath":
			dbpath, haveDB = value()
			if !haveDB {
				return refuse("--dbpath without a value")
			}
		case a == "--config":
			conf, haveConf = value()
			if !haveConf {
				return refuse("--config without a value")
			}
		case a == "--logfile":
			logfile, haveLog = value()
			if !haveLog {
				return refuse("--logfile without a value")
			}
		case a == "--print-format":
			var ok bool
			if printFormat, ok = value(); !ok {
				return refuse("--print-format without a value")
			}
		case a == "--noconfirm":
		case strings.HasPrefix(a, "--"):
			// --sync, --refresh, --sysupgrade, --downloadonly, --root,
			// --cachedir, --hookdir ...: not spelled by any builder.
			return refuse("unexpected option " + a)
		case strings.HasPrefix(a, "-") && len(a) > 1:
			for _, c := range []byte(a[1:]) {
				switch c {
				case 'S', 'Q':
					if op != 0 {
						return refuse("more than one operation")
					}
					op = c
				case 'y':
					y = true
				case 'u':
					u = true
				case 'p':
					p = true
				default:
					return refuse("unexpected option -" + string(c))
				}
			}
		default:
			return refuse("target " + a + " is not after --")
		}
	}
	if len(names) > 0 {
		if err := CheckInstallNames(names); err != nil {
			return err
		}
	}
	if haveLog && logfile != "/dev/null" {
		return refuse("--logfile must be /dev/null")
	}
	if printFormat != "" && printFormat != "%n %v %r %s" {
		return refuse("unexpected --print-format")
	}

	real := path.Clean(g.RealDB)
	isolated := false
	if haveDB && haveConf {
		if err := checkPath(dbpath); err != nil {
			return err
		}
		base := path.Clean(g.ArchiveBase)
		switch {
		case path.Clean(dbpath) == real:
			return refuse("the archive database must not be the real one")
		case g.ArchiveBase == "" || !strings.HasPrefix(path.Clean(dbpath), base+"/"):
			return refuse("the temporary database must be inside " + g.ArchiveBase)
		case conf != ArchiveConfPath(dbpath):
			return refuse("the config must be the pacman.conf inside the temporary database directory")
		}
		isolated = true
	} else if haveConf {
		return refuse("--config without --dbpath")
	}

	switch op {
	case 'Q':
		// Only `-Qu --dbpath <archive dir> --config <its conf>`.
		if y || p || !u || !isolated || len(names) > 0 || priv || fakeroot {
			return refuse("only the read-only -Qu of an archive directory is allowed")
		}
		return nil
	case 'S':
	default:
		return refuse("no operation")
	}

	switch {
	case p:
		// A dry run: it takes no root and no lock. -y is never part of it.
		if y || priv || fakeroot {
			return refuse("a dry run takes no -y, privilege command or fakeroot")
		}
		if u {
			// Option A: -Sup against the checkupdates database. Not
			// the real one, and no config of its own.
			if !haveDB || haveConf || path.Clean(dbpath) == real {
				return refuse("-Sup is only allowed against the checkupdates database")
			}
			return nil
		}
		if !isolated || len(names) == 0 {
			return refuse("-Sp is only allowed against an archive directory, with targets")
		}
		return nil
	case y && u:
		// The full upgrade against the real database: nothing else about
		// the database may be spelled.
		if haveDB || haveConf || fakeroot || !priv {
			return refuse("-Syu must use the real database, with the privilege command")
		}
		if len(names) == 0 {
			return refuse("-Syu needs the package to install")
		}
		return nil
	case y && !u:
		// A bare refresh: only of an archive directory, under fakeroot.
		if !isolated || !fakeroot || priv || len(names) > 0 {
			return refuse("-Sy without -u is only allowed under fakeroot, on an archive directory")
		}
		return nil
	case u:
		return refuse("-Su without -y")
	default:
		// -S names: only against an archive directory.
		if !isolated || !priv || fakeroot || len(names) == 0 {
			return refuse("-S against the real database is a partial upgrade; only an archive directory is allowed")
		}
		return nil
	}
}
