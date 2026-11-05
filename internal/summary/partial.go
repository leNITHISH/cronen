package summary

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/leNITHISH/cronen/internal/linkage"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/partial"
	"github.com/leNITHISH/cronen/internal/preflight"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/system"
)

// PartialSources is the system access the partial-upgrade detector needs.
type PartialSources struct {
	Runner system.Runner
	FS     system.FS
	// CacheDir holds the linkage scan cache and its last verdict. Empty
	// means there is none (demo mode).
	CacheDir string
	Location *time.Location
}

// PartialInfo is what the detector found.
type PartialInfo struct {
	// Pending are the packages the real database holds other versions of,
	// ignored ones left out. Anything here means the database was synced
	// without upgrading.
	Pending []pacman.Differing
	// History are the logged transactions that synced without upgrading
	// and installed something, oldest first, from the whole log.
	History []partial.Transaction
	// Linkage is the last linkage scan's verdict, valid only while
	// LinkageKnown is set: it was made for exactly the packages installed
	// now.
	Linkage      linkage.Saved
	LinkageKnown bool
}

// DBSynced reports that the real sync database holds newer versions than
// the installed ones: someone ran pacman -Sy without -u.
func (p PartialInfo) DBSynced() bool { return len(p.Pending) > 0 }

// Broken is the number of broken files the last scan found, 0 when no scan
// result applies.
func (p PartialInfo) Broken() int {
	if !p.LinkageKnown {
		return 0
	}
	return len(p.Linkage.Broken)
}

// LoadPartial runs `pacman -Qu` against the real database, reads the
// partial upgrades out of pacman.log, and looks up the last linkage verdict.
// Only the -Qu check can fail it: an unreadable log just means no history,
// and a missing cache or package list means no verdict.
func LoadPartial(ctx context.Context, src PartialSources) (PartialInfo, error) {
	var info PartialInfo
	pending, err := pacman.RealPending(ctx, src.Runner)
	if err != nil {
		return info, fmt.Errorf("partial-upgrade check: %w", err)
	}
	info.Pending = pending
	if data, err := src.FS.ReadFile(pacman.LogPath); err == nil {
		info.History = partial.Find(pacman.ParseHistory(data, src.Location))
	}
	info.Linkage, info.LinkageKnown = LoadLinkageVerdict(ctx, src.Runner, src.FS, src.CacheDir)
	return info, nil
}

// LoadLinkageVerdict returns the last linkage scan's verdict if one is saved
// and still describes the installed packages. Any problem reading it
// means "unknown".
func LoadLinkageVerdict(ctx context.Context, r system.Runner, fsys system.FS, cacheDir string) (linkage.Saved, bool) {
	if cacheDir == "" || fsys == nil {
		return linkage.Saved{}, false
	}
	data, err := fsys.ReadFile(path.Join(cacheDir, linkage.CacheFile))
	if err != nil {
		return linkage.Saved{}, false
	}
	cache := linkage.ParseCache(data)
	if cache.Last == nil {
		return linkage.Saved{}, false
	}
	installed, err := pacman.AllPackages(ctx, r)
	if err != nil {
		return linkage.Saved{}, false
	}
	return cache.Verdict(toLinkagePackages(installed))
}

func toLinkagePackages(pkgs []pacman.Package) []linkage.Package {
	out := make([]linkage.Package, len(pkgs))
	for i, p := range pkgs {
		out[i] = linkage.Package{Name: p.Name, Version: p.Version}
	}
	return out
}

// partialMessage is the global warning. It is worded as the plan words it.
const partialMessage = "Your package database was synced without upgrading. Installing anything now is a partial upgrade. Run the full upgrade first."

// Problem is the global banner for a database synced without upgrading: a
// warning, or critical once the linkage scan found damage. ok is false when
// the database is not in that state.
func (p PartialInfo) Problem() (preflight.Problem, bool) {
	if !p.DBSynced() {
		return preflight.Problem{}, false
	}
	pr := preflight.Problem{Severity: risk.Warn, Check: preflight.CheckPartial, Message: partialMessage}
	if n := p.Broken(); n > 0 {
		pr.Severity = risk.Critical
		pr.Message += " The linkage scan found " + plural(n, "broken file", "broken files") + "."
	}
	return pr, true
}

// AddPartial adds the detector's banner to the preflight problems, in
// order.
func AddPartial(ps []preflight.Problem, p PartialInfo) []preflight.Problem {
	if pr, ok := p.Problem(); ok {
		return preflight.Add(ps, pr)
	}
	return ps
}

// PartialSince are the partial upgrades after the last full upgrade (zero:
// unknown, all of the log).
func (p PartialInfo) PartialSince(last time.Time) []partial.Transaction {
	return partial.Since(p.History, last)
}

// PartialLine is the line for the updates header: "2 partial upgrades since
// your last full upgrade (Oct 3: pacman -Sy discord)", naming the most
// recent one. Empty when there are none.
func PartialLine(ts []partial.Transaction, loc *time.Location) string {
	if len(ts) == 0 {
		return ""
	}
	if loc == nil {
		loc = time.Local
	}
	t := ts[len(ts)-1]
	return plural(len(ts), "partial upgrade", "partial upgrades") + " since your last full upgrade (" +
		t.When.In(loc).Format("Jan 2") + ": " + oneLine(t.Command) + ")"
}

// LinkageSources is the system access a linkage scan needs.
type LinkageSources struct {
	Runner   system.Runner
	Opener   system.Opener
	FS       system.FS
	CacheDir string
	// Skip are the config's extra skip globs.
	Skip []string
	// Fresh ignores cached ELF data.
	Fresh bool
	// OnProgress, if set, hears of progress. While the package files are
	// being listed, Total is 0.
	OnProgress func(linkage.Progress)
	Now        func() time.Time
}

// ScanLinkage lists the installed packages and their files, reads their ELF
// files and reports every library nothing provides. It saves the cache
// (ELF data per package version, and, when the scan completed, the verdict)
// to CacheDir. Cancelling ctx stops it with ctx.Err(); what was read stays
// cached.
func ScanLinkage(ctx context.Context, src LinkageSources) (linkage.Result, error) {
	if src.Opener == nil {
		return linkage.Result{}, fmt.Errorf("linkage scan: no way to read files")
	}
	installed, err := pacman.AllPackages(ctx, src.Runner)
	if err != nil {
		return linkage.Result{}, err
	}
	names := make([]string, 0, len(installed))
	for _, p := range installed {
		if pacman.ValidName(p.Name) {
			names = append(names, p.Name)
		}
	}
	if src.OnProgress != nil {
		src.OnProgress(linkage.Progress{})
	}

	cache := linkage.ParseCache(nil)
	cachePath := ""
	if src.CacheDir != "" && src.FS != nil {
		cachePath = path.Join(src.CacheDir, linkage.CacheFile)
		if data, err := src.FS.ReadFile(cachePath); err == nil {
			cache = linkage.ParseCache(data)
		}
	}
	pkgs := toLinkagePackages(installed)

	// Files are only needed for packages the cache doesn't have at this
	// version, which makes a repeat scan skip nearly every pacman -Ql.
	var need []string
	versions := map[string]string{}
	for _, p := range installed {
		versions[p.Name] = p.Version
	}
	for _, n := range names {
		if _, ok := cache.Get(n, versions[n]); !ok || src.Fresh {
			need = append(need, n)
		}
	}
	files, err := pacman.PackageFiles(ctx, src.Runner, need)
	if err != nil {
		return linkage.Result{}, err
	}
	for i := range pkgs {
		pkgs[i].Files = files[pkgs[i].Name]
	}

	res, err := linkage.Scan(ctx, linkage.Options{
		Opener: src.Opener, FS: src.FS, Cache: cache, Fresh: src.Fresh,
		Skip: linkage.SkipPatterns(src.Skip), OnProgress: src.OnProgress,
	}, pkgs)
	if err == nil {
		now := time.Now
		if src.Now != nil {
			now = src.Now
		}
		cache.Save(toLinkagePackages(installed), res, now())
	}
	if cachePath != "" && cache.Dirty {
		// The cache only saves time; failing to write it costs nothing.
		_ = src.FS.WriteFile(cachePath, cache.Marshal(), 0o644)
	}
	return res, err
}

// LinkageText is the report `cronen --scan-linkage` prints: each broken file
// on its own line, then a summary.
func LinkageText(res linkage.Result) string {
	var b strings.Builder
	for _, br := range res.Broken {
		b.WriteString(oneLine(br.String()) + "\n")
	}
	if len(res.Broken) == 0 {
		b.WriteString("no broken linkage found in " + strconv.Itoa(res.Files) + " ELF files of " + plural(res.Packages, "package", "packages") + "\n")
	} else {
		b.WriteString(plural(len(res.Broken), "broken library", "broken libraries") + " in " +
			plural(len(res.BrokenPackages()), "package", "packages") + "\n")
	}
	return b.String()
}

// FixSources is the system access and settings LinkageFixes needs.
type FixSources struct {
	Runner system.Runner
	FS     system.FS
	// Priv is the privilege command for a reinstall (Config.PrivilegeArgv).
	Priv []string
	// AURHelper is the configured helper, "" for none.
	AURHelper string
	// ExtraCacheDirs and Home find cached package files.
	ExtraCacheDirs []string
	Home           string
}

// LinkageFixes turns a scan's result into suggested fixes. pendingKnown is
// false when the pending repo updates couldn't be read: no rebuild is
// offered then. Foreign packages are told from repo ones by `pacman -Qm`; a
// reinstall is suggested only for a repo package whose file at the installed
// version is in the package cache.
func LinkageFixes(ctx context.Context, src FixSources, res linkage.Result, pendingRepo int, pendingKnown bool) ([]linkage.Fix, error) {
	if len(res.Broken) == 0 {
		return nil, nil
	}
	foreignPkgs, err := pacman.ForeignPackages(ctx, src.Runner)
	if err != nil {
		return nil, err
	}
	foreign := map[string]bool{}
	for _, p := range foreignPkgs {
		foreign[p.Name] = true
	}
	in := linkage.FixInput{
		Broken: res.Broken, Foreign: foreign, PendingRepo: pendingRepo, PendingUnknown: !pendingKnown,
		Priv: src.Priv, AURHelper: src.AURHelper,
	}
	// The cache is optional: without it the fix is the full upgrade alone.
	if src.FS != nil {
		in.CacheDirs = pacman.ResolveCacheDirs(src.FS, src.ExtraCacheDirs, src.Home)
		if files, _ := pacman.ScanCache(src.FS, in.CacheDirs); len(files) > 0 {
			in.Cached = map[string]string{}
			for _, b := range res.Broken {
				if foreign[b.Package] || in.Cached[b.Package] != "" {
					continue
				}
				for _, c := range files {
					if c.Name == b.Package && pacman.SameVersion(c.Version, b.Version) {
						in.Cached[b.Package] = c.Path
						break
					}
				}
			}
		}
	}
	return linkage.Suggest(in), nil
}
