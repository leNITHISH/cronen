package pacman

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/leNITHISH/cronen/internal/system"
)

// LocalPackage is one installed package, as `pacman -Qi` describes it.
type LocalPackage struct {
	Name        string
	Version     string
	Description string
	URL         string
	// InstalledSize is in bytes.
	InstalledSize int64
	// InstallDate is zero if pacman's date couldn't be parsed.
	InstallDate time.Time
	// Explicit is true for "Explicitly installed", false for a package
	// installed as a dependency.
	Explicit bool
	// The dependency lists hold package names only: version constraints
	// ("glibc>=2.40") and descriptions ("bat: colored output") are cut.
	DependsOn   []string
	OptionalFor []string
	RequiredBy  []string
	Provides    []string
	Groups      []string
}

// LocalDB is every installed package, sorted by name, and what's known
// about where they came from.
type LocalDB struct {
	Packages []LocalPackage
	// Foreign are the packages in no sync database (`pacman -Qm`).
	Foreign map[string]bool
	// Orphans are the dependencies nothing needs any more (`pacman -Qdtq`).
	Orphans map[string]bool

	index map[string]int
}

// NewLocalDB builds a LocalDB from parsed packages, sorting them by name and
// indexing them.
func NewLocalDB(pkgs []LocalPackage, foreign, orphans map[string]bool) *LocalDB {
	pkgs = slices.Clone(pkgs)
	slices.SortStableFunc(pkgs, func(a, b LocalPackage) int { return strings.Compare(a.Name, b.Name) })
	db := &LocalDB{Packages: pkgs, Foreign: foreign, Orphans: orphans, index: make(map[string]int, len(pkgs))}
	if db.Foreign == nil {
		db.Foreign = map[string]bool{}
	}
	if db.Orphans == nil {
		db.Orphans = map[string]bool{}
	}
	for i, p := range pkgs {
		db.index[p.Name] = i
	}
	return db
}

// Lookup returns the installed package called name.
func (db *LocalDB) Lookup(name string) (LocalPackage, bool) {
	i, ok := db.index[name]
	if !ok {
		return LocalPackage{}, false
	}
	return db.Packages[i], true
}

// ParseLocalDB parses the output of `pacman -Qi` with no arguments, in the C
// locale. Dates are local times, read in loc.
func ParseLocalDB(data []byte, loc *time.Location) ([]LocalPackage, error) {
	blocks, err := parseBlocks(data)
	if err != nil {
		return nil, err
	}
	pkgs := make([]LocalPackage, 0, len(blocks))
	for _, kv := range blocks {
		p := LocalPackage{
			Name:        kv["Name"],
			Version:     kv["Version"],
			Description: kv["Description"],
			URL:         kv["URL"],
			Explicit:    strings.HasPrefix(kv["Install Reason"], "Explicitly"),
			DependsOn:   depNames(kv["Depends On"]),
			OptionalFor: depNames(kv["Optional For"]),
			RequiredBy:  depNames(kv["Required By"]),
			Provides:    listField(kv["Provides"]),
			Groups:      listField(kv["Groups"]),
			InstallDate: parseInstallDate(kv["Install Date"], loc),
		}
		if p.Name == "" {
			return nil, errors.New("pacman -Qi: a package has no Name")
		}
		if p.InstalledSize, err = parseSize(kv["Installed Size"]); err != nil {
			return nil, fmt.Errorf("pacman -Qi: %s: Installed Size: %w", p.Name, err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

// depNames splits a pacman list of dependencies and keeps the names: it cuts
// a version constraint ("glibc>=2.40") or a description ("bat: colored").
func depNames(v string) []string {
	var out []string
	for _, f := range listField(v) {
		if i := strings.IndexAny(f, "<>=:"); i > 0 {
			f = f[:i]
		}
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// BackupFile is one entry of a package's Backup Files.
type BackupFile struct {
	Path string
	// Status is what `pacman -Qii` printed after the path: "modified",
	// "unmodified" or "unreadable". It is "" when pacman printed none.
	Status string
}

// ParseBackupFiles parses the output of `pacman -Qii -- names...` into the
// backup files of each package, by package name. Plain `pacman -Qi` prints no
// Backup Files field; `-Qii` prints "path [status]", one per line.
func ParseBackupFiles(data []byte) (map[string][]BackupFile, error) {
	blocks, err := parseBlocks(data)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]BackupFile, len(blocks))
	for _, kv := range blocks {
		if kv["Name"] == "" {
			return nil, errors.New("pacman -Qii: a package has no Name")
		}
		out[kv["Name"]] = backupFiles(kv["Backup Files"])
	}
	return out, nil
}

// Modified returns the paths of the files whose status is "modified": only
// those become .pacsave files on removal.
func Modified(files []BackupFile) []string {
	var out []string
	for _, f := range files {
		if f.Status == "modified" {
			out = append(out, f.Path)
		}
	}
	return out
}

// backupFiles splits the Backup Files field (continuation lines are joined by
// two spaces): "path [status]", or the older "[status] path" spelling.
func backupFiles(v string) []BackupFile {
	if v == "" || v == "None" || v == "(none)" {
		return nil
	}
	var out []BackupFile
	for _, f := range strings.Split(v, "  ") {
		f = strings.TrimSpace(f)
		var status string
		switch {
		case strings.HasPrefix(f, "["):
			if i := strings.Index(f, "] "); i > 0 {
				status, f = f[1:i], strings.TrimSpace(f[i+2:])
			}
		case strings.HasSuffix(f, "]"):
			if i := strings.LastIndex(f, " ["); i > 0 {
				status, f = f[i+2:len(f)-1], strings.TrimSpace(f[:i])
			}
		}
		if f != "" {
			out = append(out, BackupFile{Path: f, Status: status})
		}
	}
	return out
}

// installDateLayouts are the spellings of Install Date cronen reads: the
// same as Build Date, and the one with a zone and a 12-hour clock.
//
// Needs checking on Arch: which one pacman prints in the C locale.
var installDateLayouts = []string{
	"Mon Jan _2 15:04:05 2006",
	"Mon 02 Jan 2006 03:04:05 PM MST",
}

// parseInstallDate returns the zero time for a date it can't read; only the
// date column is lost.
func parseInstallDate(v string, loc *time.Location) time.Time {
	for _, layout := range installDateLayouts {
		if t, err := time.ParseInLocation(layout, v, loc); err == nil {
			return t
		}
	}
	return time.Time{}
}

// LoadLocalDB reads every installed package with one `pacman -Qi`, plus the
// foreign packages (`pacman -Qm`) and the orphans (`pacman -Qdtq`).
func LoadLocalDB(ctx context.Context, r system.Runner, loc *time.Location) (*LocalDB, error) {
	out, _, err := r.Run(ctx, "pacman", "-Qi")
	if err != nil {
		return nil, fmt.Errorf("reading the package database: %w", err)
	}
	pkgs, err := ParseLocalDB(out, loc)
	if err != nil {
		return nil, err
	}
	foreign, err := ForeignPackages(ctx, r)
	if err != nil {
		return nil, err
	}
	orphans, err := Orphans(ctx, r)
	if err != nil {
		return nil, err
	}
	fset := make(map[string]bool, len(foreign))
	for _, f := range foreign {
		fset[f.Name] = true
	}
	oset := make(map[string]bool, len(orphans))
	for _, o := range orphans {
		oset[o] = true
	}
	return NewLocalDB(pkgs, fset, oset), nil
}

// Orphans runs `pacman -Qdtq`: dependencies that nothing requires. It exits
// 1 with no output when there are none, which is an empty list.
func Orphans(ctx context.Context, r system.Runner) ([]string, error) {
	out, code, err := r.Run(ctx, "pacman", "-Qdtq")
	if code != 0 {
		var exitErr *system.ExitError
		if code == 1 && errors.As(err, &exitErr) &&
			len(bytes.TrimSpace(out)) == 0 && len(bytes.TrimSpace(exitErr.Stderr)) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("listing orphans: %w", err)
	}
	var names []string
	for l := range strings.Lines(string(out)) {
		if n := strings.TrimSpace(l); n != "" {
			names = append(names, n)
		}
	}
	return names, nil
}

// WhyInstalled finds the shortest chain of Required By links from name up to
// an explicitly installed package: ["libxyz", "gtk4", "nautilus"], where the
// last one is explicit. A package that is itself explicit is a chain of one.
// ok is false when no explicit package leads to it (an orphan). Cycles are
// handled: every package is visited once. Ties are broken by name, so the
// answer is stable.
func (db *LocalDB) WhyInstalled(name string) (chain []string, ok bool) {
	start, found := db.index[name]
	if !found {
		return nil, false
	}
	prev := map[int]int{start: -1}
	queue := []int{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if db.Packages[cur].Explicit {
			for i := cur; i >= 0; i = prev[i] {
				chain = append(chain, db.Packages[i].Name)
			}
			slices.Reverse(chain)
			return chain, true
		}
		by := slices.Clone(db.Packages[cur].RequiredBy)
		slices.Sort(by)
		for _, n := range by {
			i, known := db.index[n]
			if _, seen := prev[i]; !known || seen {
				continue
			}
			prev[i] = cur
			queue = append(queue, i)
		}
	}
	return nil, false
}
