package pacman

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/leNITHISH/cronen/internal/system"
)

// ErrPreviewUnavailable reports that the transaction preview can't be made,
// for example because the database checkupdates syncs isn't there. Its
// message is meant to be shown to the user as is.
var ErrPreviewUnavailable = errors.New("transaction preview unavailable")

// SyncDBPath is where checkupdates leaves the database it synced:
// $CHECKUPDATES_DB, or ${TMPDIR:-/tmp}/checkup-db-<uid>.
//
// Needs checking on Arch: that this is the path (and the uid, not the user
// name, is in it) on the current pacman-contrib. See PLAN.md.
func SyncDBPath(getenv func(string) string, uid int) string {
	if dir := getenv("CHECKUPDATES_DB"); dir != "" {
		return path.Clean(dir)
	}
	tmp := getenv("TMPDIR")
	if tmp == "" {
		tmp = "/tmp"
	}
	return path.Join(tmp, "checkup-db-"+strconv.Itoa(uid))
}

// CheckSyncDB reports ErrPreviewUnavailable unless dir and its sync/
// subdirectory exist, which they do once checkupdates has run. cronen reads
// that database and never creates or writes it.
func CheckSyncDB(fsys system.FS, dir string) error {
	for _, d := range []string{dir, path.Join(dir, "sync")} {
		fi, err := fsys.Stat(d)
		switch {
		case errors.Is(err, fs.ErrNotExist) || (err == nil && !fi.IsDir()):
			return fmt.Errorf("%w: checkupdates left no database at %s", ErrPreviewUnavailable, d)
		case err != nil:
			return fmt.Errorf("%w: %w", ErrPreviewUnavailable, err)
		}
	}
	return nil
}

// dbArgs are the options that point a read-only pacman query at the
// checkupdates database. They go on every call that touches it.
func dbArgs(db string) []string {
	return []string{"--dbpath", db, "--logfile", "/dev/null"}
}

// packageName matches the package names cronen passes to pacman. Anything
// else is refused rather than escaped.
var packageName = regexp.MustCompile(`^[a-z0-9@._+][a-z0-9@._+-]*$`)

func checkNames(names []string) error {
	for _, n := range names {
		if !packageName.MatchString(n) {
			return fmt.Errorf("refusing package name %q", n)
		}
	}
	return nil
}

// Printed is one package of `pacman -Sup --print-format '%n %v %r %s'`: a
// package -Syu would install.
type Printed struct {
	Name    string
	Version string
	Repo    string
	// DownloadSize is how many bytes pacman would download, 0 when the
	// package is already in the cache.
	DownloadSize int64
}

// ParsePrint parses the output of -Sup --print-format '%n %v %r %s', one
// package per line. Blank lines are skipped; any other line that doesn't
// have that shape is an error.
func ParsePrint(data []byte) ([]Printed, error) {
	var pkgs []Printed
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 4 {
			return nil, fmt.Errorf("pacman -Sup: line %d: unexpected format %q", n, line)
		}
		size, err := strconv.ParseInt(f[3], 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("pacman -Sup: line %d: bad download size %q", n, f[3])
		}
		pkgs = append(pkgs, Printed{Name: f[0], Version: f[1], Repo: f[2], DownloadSize: size})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("pacman -Sup: %w", err)
	}
	return pkgs, nil
}

// SyncPrint asks pacman, as the current user, what -Syu would install,
// against the checkupdates database at db. It is a dry run: -p never takes
// the lock or touches the real database.
//
// Needs checking on Arch: that this works as a normal user with --dbpath at
// the checkupdates database. See PLAN.md.
func SyncPrint(ctx context.Context, r system.Runner, db string) ([]Printed, error) {
	args := append([]string{"-Sup"}, dbArgs(db)...)
	args = append(args, "--noconfirm", "--print-format", "%n %v %r %s")
	out, _, err := r.Run(ctx, "pacman", args...)
	if err != nil {
		return nil, fmt.Errorf("previewing the transaction: %w", err)
	}
	return ParsePrint(out)
}

// Info is what pacman -Si or -Qi says about one package.
type Info struct {
	Name       string
	Version    string
	Repository string
	// Sizes are in bytes. DownloadSize is only known for -Si.
	InstalledSize int64
	DownloadSize  int64
	DependsOn     []string
	Provides      []string
	Replaces      []string
	// BuildDate is zero if pacman's date couldn't be parsed.
	BuildDate time.Time
}

// ParseInfo parses -Si or -Qi output in the C locale: "Key : value" lines,
// continuation lines indented, packages separated by blank lines. Build
// dates are local times, read in loc.
func ParseInfo(data []byte, loc *time.Location) ([]Info, error) {
	blocks, err := parseBlocks(data)
	if err != nil {
		return nil, err
	}
	var infos []Info
	for _, kv := range blocks {
		info, err := infoFrom(kv, loc)
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// parseBlocks splits pacman -Si/-Qi output into one key/value map per
// package. Continuation lines are appended to their key, separated by two
// spaces.
func parseBlocks(data []byte) ([]map[string]string, error) {
	var blocks []map[string]string
	var cur map[string]string
	lastKey := ""
	flush := func() {
		if len(cur) > 0 {
			blocks = append(blocks, cur)
		}
		cur, lastKey = nil, ""
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.TrimSpace(line) == "":
			flush()
		case line[0] == ' ' || line[0] == '\t':
			if cur != nil && lastKey != "" {
				cur[lastKey] += "  " + strings.TrimSpace(line)
			}
		default:
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				return nil, fmt.Errorf("pacman info: unexpected line %q", line)
			}
			if cur == nil {
				cur = map[string]string{}
			}
			lastKey = strings.TrimSpace(k)
			cur[lastKey] = strings.TrimSpace(v)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("pacman info: %w", err)
	}
	flush()
	return blocks, nil
}

func infoFrom(kv map[string]string, loc *time.Location) (Info, error) {
	info := Info{
		Name:       kv["Name"],
		Version:    kv["Version"],
		Repository: kv["Repository"],
		DependsOn:  listField(kv["Depends On"]),
		Provides:   listField(kv["Provides"]),
		Replaces:   listField(kv["Replaces"]),
	}
	if info.Name == "" {
		return Info{}, errors.New("pacman info: a package has no Name")
	}
	var err error
	if info.InstalledSize, err = parseSize(kv["Installed Size"]); err != nil {
		return Info{}, fmt.Errorf("pacman info: %s: Installed Size: %w", info.Name, err)
	}
	if info.DownloadSize, err = parseSize(kv["Download Size"]); err != nil {
		return Info{}, fmt.Errorf("pacman info: %s: Download Size: %w", info.Name, err)
	}
	// An unreadable date only loses the build age.
	if t, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", kv["Build Date"], loc); err == nil {
		info.BuildDate = t
	}
	return info, nil
}

// listField splits a pacman list ("a  b  c", or "None").
func listField(v string) []string {
	if v == "" || v == "None" {
		return nil
	}
	return strings.Fields(v)
}

var sizeUnits = map[string]float64{
	"B": 1, "KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40, "PiB": 1 << 50,
}

// parseSize parses pacman's "12.34 MiB" into bytes. An empty value is 0.
func parseSize(v string) (int64, error) {
	if v == "" {
		return 0, nil
	}
	num, unit, ok := strings.Cut(v, " ")
	mult, known := sizeUnits[unit]
	n, err := strconv.ParseFloat(num, 64)
	if !ok || !known || err != nil || n < 0 || math.IsInf(n, 0) || math.IsNaN(n) {
		return 0, fmt.Errorf("bad size %q", v)
	}
	return int64(math.Round(n * mult)), nil
}

// FormatSize formats a byte count the way pacman does, with a binary unit
// and one decimal below 10: "512 B", "3.5 KiB", "412 MiB", "1.2 GiB".
func FormatSize(n int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return strconv.FormatInt(n, 10) + " B"
	}
	if v < 10 {
		return strconv.FormatFloat(v, 'f', 1, 64) + " " + units[i]
	}
	return strconv.FormatFloat(v, 'f', 0, 64) + " " + units[i]
}

// FormatDelta is FormatSize with a sign: "+26 MiB", "-3.5 KiB", "+0 B".
func FormatDelta(n int64) string {
	if n < 0 {
		return "-" + FormatSize(-n)
	}
	return "+" + FormatSize(n)
}

// infoBatch is how many packages go to one pacman -Si or -Qi call.
const infoBatch = 100

// SyncInfo runs `pacman -Si` for names against the checkupdates database,
// in batches, and returns the answers by package name.
func SyncInfo(ctx context.Context, r system.Runner, db string, names []string, loc *time.Location) (map[string]Info, error) {
	return queryInfo(ctx, r, names, loc, func(batch []string) []string {
		args := append([]string{"-Si"}, dbArgs(db)...)
		return append(append(args, "--"), batch...)
	})
}

// LocalInfo runs `pacman -Qi` for names and returns the answers by package
// name.
func LocalInfo(ctx context.Context, r system.Runner, names []string, loc *time.Location) (map[string]Info, error) {
	return queryInfo(ctx, r, names, loc, func(batch []string) []string {
		return append([]string{"-Qi", "--"}, batch...)
	})
}

func queryInfo(ctx context.Context, r system.Runner, names []string, loc *time.Location, argv func([]string) []string) (map[string]Info, error) {
	if err := checkNames(names); err != nil {
		return nil, err
	}
	names = slices.Clone(names)
	slices.Sort(names)
	names = slices.Compact(names)
	infos := map[string]Info{}
	for batch := range slices.Chunk(names, infoBatch) {
		args := argv(batch)
		out, _, err := r.Run(ctx, "pacman", args...)
		if err != nil {
			return nil, fmt.Errorf("pacman %s: %w", args[0], err)
		}
		parsed, err := ParseInfo(out, loc)
		if err != nil {
			return nil, err
		}
		for _, info := range parsed {
			infos[info.Name] = info
		}
	}
	return infos, nil
}

// Held is an update that IgnorePkg or IgnoreGroup holds back.
type Held struct {
	Name       string
	OldVersion string
	NewVersion string
}

// ParseHeld parses `pacman -Qu --dbpath <db>` output and keeps the lines
// marked "[ignored]", which checkupdates filters out.
func ParseHeld(data []byte) ([]Held, error) {
	var held []Held
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 4 || f[2] != "->" || len(f) > 5 {
			return nil, fmt.Errorf("pacman -Qu: line %d: unexpected format %q", n, line)
		}
		if len(f) == 5 && f[4] == "[ignored]" {
			held = append(held, Held{Name: f[0], OldVersion: f[1], NewVersion: f[3]})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("pacman -Qu: %w", err)
	}
	return held, nil
}

// HeldPackages lists the updates held back by IgnorePkg or IgnoreGroup.
// pacman -Qu exits 1 with no output when nothing is pending.
func HeldPackages(ctx context.Context, r system.Runner, db string) ([]Held, error) {
	out, code, err := r.Run(ctx, "pacman", append([]string{"-Qu"}, dbArgs(db)...)...)
	if code == 0 {
		return ParseHeld(out)
	}
	var exitErr *system.ExitError
	if code == 1 && errors.As(err, &exitErr) &&
		len(bytes.TrimSpace(out)) == 0 && len(bytes.TrimSpace(exitErr.Stderr)) == 0 {
		return nil, nil
	}
	return nil, fmt.Errorf("listing held packages: %w", err)
}

// Differing is one line of `pacman -Qu`: an installed package whose version
// differs from the one in the database queried.
type Differing struct {
	Name       string
	OldVersion string
	NewVersion string
}

// ParseQu parses `pacman -Qu --dbpath <db>` output, skipping the lines
// marked "[ignored]" (IgnorePkg and IgnoreGroup hold those back whatever
// the snapshot says).
func ParseQu(data []byte) ([]Differing, error) {
	var out []Differing
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 4 || f[2] != "->" || len(f) > 5 {
			return nil, fmt.Errorf("pacman -Qu: line %d: unexpected format %q", n, line)
		}
		if len(f) == 5 {
			if f[4] == "[ignored]" {
				continue
			}
			return nil, fmt.Errorf("pacman -Qu: line %d: unexpected format %q", n, line)
		}
		out = append(out, Differing{Name: f[0], OldVersion: f[1], NewVersion: f[3]})
	}
	return out, sc.Err()
}
