package summary

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/leNITHISH/cronen/internal/aur"
	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/system"
)

func pkgBase(p aur.Package) string {
	if p.Info.PackageBase != "" {
		return p.Info.PackageBase
	}
	return p.Name
}

func maintainersOf(info aur.Info) risk.Maintainers {
	return risk.Maintainers{Maintainer: info.Maintainer, Co: info.CoMaintainers}
}

// MaintainerChecks compares the AUR's maintainers of every package it knows
// with the ones recorded in state (rule 12), keyed by package name.
func MaintainerChecks(pkgs []aur.Package, st config.State) map[string]risk.MaintainerCheck {
	checks := map[string]risk.MaintainerCheck{}
	for _, p := range pkgs {
		if p.Info == nil {
			continue
		}
		chk := risk.MaintainerCheck{Current: maintainersOf(*p.Info)}
		if rec, ok := st.Maintainer(pkgBase(p)); ok {
			chk.Recorded = &risk.Maintainers{Maintainer: rec.Maintainer, Co: rec.CoMaintainers}
		}
		checks[p.Name] = chk
	}
	return checks
}

// RecordMaintainers stores the AUR's current maintainers in st. Without
// force it only fills in package bases nothing is recorded for, which is
// how the first maintainers seen become the baseline. With force it
// overwrites, which is what a successful upgrade or an acknowledgement does.
// It reports whether st changed.
func RecordMaintainers(st *config.State, pkgs []aur.Package, force bool) bool {
	changed := false
	for _, p := range pkgs {
		if p.Info == nil {
			continue
		}
		cur := maintainersOf(*p.Info)
		rec := config.Maintainer{PkgBase: pkgBase(p), Maintainer: cur.Maintainer, CoMaintainers: cur.Co}
		old, ok := st.Maintainer(rec.PkgBase)
		if ok && !force {
			continue
		}
		st.SetMaintainer(rec)
		if now, _ := st.Maintainer(rec.PkgBase); !ok || old.Maintainer != now.Maintainer || !slices.Equal(old.CoMaintainers, now.CoMaintainers) {
			changed = true
		}
	}
	return changed
}

// AssessAUR adds the AUR review rules (12 and 13) to res: maintainer changes
// since the recorded ones, and suspicious changes in the PKGBUILD diffs
// that are loaded, keyed by package name.
func AssessAUR(res risk.Result, pkgs []aur.Package, st config.State, reviews map[string]aur.Review) risk.Result {
	in := risk.AURInput{Maintainers: MaintainerChecks(pkgs, st), DiffReasons: map[string][]string{}}
	for name, r := range reviews {
		for _, f := range r.Findings {
			in.DiffReasons[name] = append(in.DiffReasons[name], f.Reason)
		}
	}
	return risk.AddAUR(res, in)
}

// LoadAURReview fetches the history of a pending AUR update and its
// PKGBUILD diff. commits is the log if it is already fetched, or nil.
func LoadAURReview(ctx context.Context, r system.Runner, f system.Fetcher, loc *time.Location, p aur.Package, commits []aur.Commit, fresh bool) (aur.Review, error) {
	if p.Info == nil {
		return aur.Review{}, nil
	}
	built, err := aur.BuildDate(ctx, r, p.Name, loc)
	if err != nil {
		return aur.Review{}, err
	}
	c := aur.NewClient(f, r)
	if commits != nil {
		return c.ReviewLog(ctx, pkgBase(p), commits, built, fresh)
	}
	return c.Review(ctx, pkgBase(p), built, fresh)
}

// LoadAURLog fetches the commit log of an AUR package's base.
func LoadAURLog(ctx context.Context, f system.Fetcher, p aur.Package, fresh bool) ([]aur.Commit, error) {
	if p.Info == nil {
		return nil, nil
	}
	return aur.NewClient(f, nil).Log(ctx, pkgBase(p), fresh)
}

// loadReviews loads the diff of every pending AUR update, at most four at a
// time, for aur_review = "eager". It returns the reviews by package name
// and the errors, one per failed package.
func loadReviews(ctx context.Context, r system.Runner, f system.Fetcher, loc *time.Location, pkgs []aur.Package) (map[string]aur.Review, []error) {
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		reviews = map[string]aur.Review{}
		errs    []error
		sem     = make(chan struct{}, 4)
	)
	for _, p := range pkgs {
		if !p.Pending || p.Info == nil {
			continue
		}
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			rev, err := LoadAURReview(ctx, r, f, loc, p, nil, false)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, &reviewError{name: p.Name, err: err})
				return
			}
			reviews[p.Name] = rev
		})
	}
	wg.Wait()
	slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
	return reviews, errs
}

type reviewError struct {
	name string
	err  error
}

func (e *reviewError) Error() string { return "AUR review of " + e.name + ": " + e.err.Error() }
func (e *reviewError) Unwrap() error { return e.err }
