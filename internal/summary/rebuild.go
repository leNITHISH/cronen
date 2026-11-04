package summary

import (
	"context"
	"fmt"
	"path"
	"slices"
	"time"

	"github.com/leNITHISH/cronen/internal/aur"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/rebuild"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/system"
)

// ELFCacheFile is the name, inside the cache directory, of the file holding
// the ELF scan results.
const ELFCacheFile = "elf-needs.json"

// RebuildSources is the system access LoadRebuild needs.
type RebuildSources struct {
	Runner system.Runner
	// Opener reads ELF files. Nil turns the ELF scan off.
	Opener system.Opener
	// FS holds the scan cache.
	FS system.FS
	// CacheDir is where the scan cache lives. Empty means no cache is read
	// or written (demo mode).
	CacheDir string
	// ScanELF is config's rebuild_scan_elf.
	ScanELF bool
	// Fresh ignores cached scan results (but still stores new ones).
	Fresh bool
}

// LoadRebuild finds the installed foreign packages the previewed upgrade
// leaves needing a rebuild: those depending on or linking against a library
// a pending update stops providing, and those installed under the old
// directory of an updated interpreter. tx is the transaction preview, which
// knows the Provides of both versions of every update.
//
// Without work to do (no update drops a soname or moves an interpreter) it
// runs no further commands.
func LoadRebuild(ctx context.Context, src RebuildSources, tx pacman.Transaction) ([]rebuild.Finding, error) {
	var updates []rebuild.Update
	lostAny, movedAny := false, false
	for _, u := range tx.Updates {
		ru := rebuild.Update{
			Name: u.Name, OldVersion: u.OldVersion, NewVersion: u.NewVersion,
			OldProvides: u.OldProvides, NewProvides: u.NewProvides,
		}
		if len(rebuild.Lost(ru)) > 0 {
			lostAny = true
		}
		if _, ok := rebuild.Moved(ru); ok {
			movedAny = true
		}
		updates = append(updates, ru)
	}
	if !lostAny && !movedAny {
		return nil, nil
	}

	pkgs, err := pacman.ForeignPackages(ctx, src.Runner)
	if err != nil {
		return nil, err
	}
	// Every installed name tells debug splits of repo packages apart.
	var installed map[string]bool
	if all, err := pacman.InstalledNames(ctx, src.Runner); err == nil {
		installed = all
	}
	foreign := make([]rebuild.Foreign, 0, len(pkgs))
	var names []string
	for _, p := range pkgs {
		if !pacman.ValidName(p.Name) {
			continue
		}
		foreign = append(foreign, rebuild.Foreign{
			Name: p.Name, Version: p.Version,
			Debug: aur.Classify(p.Name, func(n string) bool { return installed[n] }) == aur.KindDebug,
		})
		names = append(names, p.Name)
	}
	if len(foreign) == 0 {
		return nil, nil
	}

	infos, err := pacman.LocalInfo(ctx, src.Runner, names, time.UTC)
	if err != nil {
		return nil, err
	}
	for i := range foreign {
		foreign[i].Depends = infos[foreign[i].Name].DependsOn
	}

	scan := lostAny && src.ScanELF && src.Opener != nil
	if scan || movedAny {
		var live []string
		for _, f := range foreign {
			if !f.Debug {
				live = append(live, f.Name)
			}
		}
		files, err := pacman.PackageFiles(ctx, src.Runner, live)
		if err != nil {
			return nil, err
		}
		for i := range foreign {
			foreign[i].Files = files[foreign[i].Name]
		}
	}
	if scan {
		if err := scanNeeds(ctx, src, foreign); err != nil {
			return nil, err
		}
	}
	return rebuild.Detect(updates, foreign), nil
}

// scanNeeds fills in the DT_NEEDED entries of every live foreign package,
// from the cache where it can, and saves the cache.
func scanNeeds(ctx context.Context, src RebuildSources, foreign []rebuild.Foreign) error {
	cache := rebuild.ParseCache(nil)
	cachePath := ""
	if src.CacheDir != "" && src.FS != nil {
		cachePath = path.Join(src.CacheDir, ELFCacheFile)
		// An unreadable cache is an empty one.
		if data, err := src.FS.ReadFile(cachePath); err == nil {
			cache = rebuild.ParseCache(data)
		}
	}
	versions := map[string]string{}
	for _, f := range foreign {
		versions[f.Name] = f.Version
	}
	cache.Keep(versions)

	for i, f := range foreign {
		if f.Debug {
			continue
		}
		needs, err := cache.Needs(ctx, src.Opener, f.Name, f.Version, f.Files, src.Fresh)
		if err != nil {
			return fmt.Errorf("scanning %s: %w", f.Name, err)
		}
		foreign[i].Needs = needs
	}
	if cachePath != "" && cache.Dirty {
		// The cache only saves time: failing to write it costs nothing.
		_ = src.FS.WriteFile(cachePath, cache.Marshal(), 0o644)
	}
	return nil
}

// AssessOption adjusts Assess with an extra source of findings.
type AssessOption func(*risk.Input)

// WithRebuilds adds the findings of LoadRebuild, so the updates to blame are
// flagged "Needs rebuild".
func WithRebuilds(findings []rebuild.Finding) AssessOption {
	return func(in *risk.Input) { in.Rebuilds = findings }
}

// RebuildPackages lists the distinct foreign packages among findings.
func RebuildPackages(findings []rebuild.Finding) []string {
	var names []string
	for _, f := range findings {
		if !slices.Contains(names, f.Package) {
			names = append(names, f.Package)
		}
	}
	slices.Sort(names)
	return names
}
