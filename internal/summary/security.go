package summary

import (
	"context"
	"fmt"
	"sync"

	"github.com/leNITHISH/cronen/internal/archweb"
	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/gitlab"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/security"
	"github.com/leNITHISH/cronen/internal/system"
)

// SecurityData is what the security tracker says about this system.
type SecurityData struct {
	// Fixes maps a package name to the advisory groups its pending update
	// fixes.
	Fixes map[string][]security.Fix
	// Vulnerable are installed packages with a known, unfixed problem.
	Vulnerable []security.Vulnerable
}

// Feeds is the extra data the security and known-issues rules look at. The
// zero value turns both off.
type Feeds struct {
	Security SecurityData
	Issues   map[string][]gitlab.Issue
}

// vercmpCompare is a security.Compare that shells out to vercmp, asking
// once per pair.
func vercmpCompare(ctx context.Context, r system.Runner) security.Compare {
	type pair struct{ a, b string }
	var mu sync.Mutex
	seen := map[pair]int{}
	return func(a, b string) (int, error) {
		mu.Lock()
		n, ok := seen[pair{a, b}]
		mu.Unlock()
		if ok {
			return n, nil
		}
		n, err := pacman.Vercmp(ctx, r, a, b)
		if err != nil {
			return 0, err
		}
		mu.Lock()
		seen[pair{a, b}] = n
		mu.Unlock()
		return n, nil
	}
}

// LoadSecurity reads the security tracker and works out which pending repo
// updates fix an advisory and which installed packages are vulnerable with
// no fix. updates are the pending updates, repo and AUR. A failed lookup of
// installed packages still returns the fixes, with the error.
func LoadSecurity(ctx context.Context, r system.Runner, f system.Fetcher, updates []risk.Update, fresh bool) (SecurityData, error) {
	groups, err := security.NewClient(f).Groups(ctx, fresh)
	if err != nil {
		return SecurityData{}, err
	}
	cmp := vercmpCompare(ctx, r)

	data := SecurityData{Fixes: map[string][]security.Fix{}}
	for _, u := range updates {
		if u.AUR {
			continue
		}
		base := u.Base
		fixes, err := security.FixesFor(groups, security.Pending{
			Name: u.Name, Base: base, Installed: u.OldVersion, New: u.NewVersion,
		}, cmp)
		if err != nil {
			return SecurityData{}, fmt.Errorf("security tracker: %w", err)
		}
		if len(fixes) > 0 {
			data.Fixes[u.Name] = fixes
		}
	}

	// Native packages only: the tracker covers the official repos, and a
	// foreign package that shares a name with one is a different package.
	native, err := pacman.NativePackages(ctx, r)
	if err != nil {
		return data, fmt.Errorf("security tracker: %w", err)
	}
	var repoPkgs []security.Installed
	for _, p := range native {
		repoPkgs = append(repoPkgs, security.Installed{Name: p.Name, Version: p.Version})
	}
	data.Vulnerable, err = security.Unfixed(groups, repoPkgs, cmp)
	if err != nil {
		return data, fmt.Errorf("security tracker: %w", err)
	}
	return data, nil
}

// LoadIssues looks up the open GitLab issues of each pending repo update
// since its new build. tx supplies the build dates; without one nothing is
// looked up. Updates are looked up under their pkgbase when it is known,
// else under their name.
func LoadIssues(ctx context.Context, f system.Fetcher, updates []risk.Update, tx pacman.Transaction, fresh bool) (map[string][]gitlab.Issue, error) {
	built := map[string]pacman.UpdateDetail{}
	for _, d := range tx.Updates {
		built[d.Name] = d
	}
	var queries []gitlab.Query
	for _, u := range updates {
		d, ok := built[u.Name]
		if u.AUR || !ok || d.BuildDate.IsZero() {
			continue
		}
		base := u.Base
		if base == "" {
			base = u.Name
		}
		queries = append(queries, gitlab.Query{Key: u.Name, Project: archweb.GitLabProject(base), Since: d.BuildDate})
	}
	if len(queries) == 0 {
		return nil, nil
	}
	issues, err := gitlab.NewClient(f).Lookup(ctx, queries, fresh)
	if err != nil {
		return issues, fmt.Errorf("known issues: %w", err)
	}
	return issues, nil
}

// WithFeeds gives Assess the security and known-issues data, for the
// feeds the config has on.
func WithFeeds(feeds Feeds, cfg config.Config) AssessOption {
	return func(in *risk.Input) {
		if cfg.Security {
			in.Fixes = feeds.Security.Fixes
			in.Vulnerable = feeds.Security.Vulnerable
		}
		if cfg.KnownIssues {
			in.Issues = feeds.Issues
		}
	}
}

// loadFeeds loads whatever of the feeds the config turns on, concurrently.
// With both off it makes no request at all. The errors are the failed
// sources, each saying which.
func loadFeeds(ctx context.Context, src Sources, updates []risk.Update, tx *pacman.Transaction) (Feeds, []error) {
	var (
		wg       sync.WaitGroup
		feeds    Feeds
		secErr   error
		issueErr error
	)
	if src.Config.Security {
		wg.Go(func() { feeds.Security, secErr = LoadSecurity(ctx, src.Runner, src.Fetcher, updates, false) })
	}
	if src.Config.KnownIssues && tx != nil {
		wg.Go(func() { feeds.Issues, issueErr = LoadIssues(ctx, src.Fetcher, updates, *tx, false) })
	}
	wg.Wait()
	return feeds, []error{secErr, issueErr}
}
