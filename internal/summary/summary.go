// Package summary gathers every data source once and sums up what an
// upgrade would bring. It backs --check, --json and --waybar, and holds the
// loading and assessment steps the TUI shares with them, so both always
// agree.
package summary

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/leNITHISH/cronen/internal/aur"
	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/news"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/system"
)

// LoadAUR lists the installed foreign packages and checks them against the
// AUR.
func LoadAUR(ctx context.Context, r system.Runner, f system.Fetcher, fresh bool) ([]aur.Package, error) {
	installed, err := pacman.ForeignPackages(ctx, r)
	if err != nil {
		return nil, err
	}
	c := aur.NewClient(f, r)
	// Every installed name tells debug splits of repo packages apart.
	// Without it, only splits of foreign packages are recognized.
	if all, err := pacman.InstalledNames(ctx, r); err == nil {
		c.Installed = all
	}
	return c.Check(ctx, installed, fresh)
}

// LoadLastUpgrade returns the time of the last full upgrade in pacman.log,
// or zero if there is none. An unreadable log is an error; callers treat
// the time as unknown either way.
func LoadLastUpgrade(fsys system.FS, loc *time.Location) (time.Time, error) {
	last, err := pacman.ReadLastFullUpgrade(fsys, pacman.LogPath, loc)
	if errors.Is(err, pacman.ErrNoFullUpgrade) {
		// Not an error worth showing: the time is just unknown.
		err = nil
	}
	return last, err
}

// LoadPacnew returns the .pacnew and .pacsave files pacdiff -o lists.
func LoadPacnew(ctx context.Context, r system.Runner) ([]string, error) {
	out, _, err := r.Run(ctx, "pacdiff", "-o")
	if err != nil {
		return nil, fmt.Errorf("pacdiff: %w", err)
	}
	var files []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// LastUpgrade is the later of pacman.log's last full upgrade and the last
// upgrade cronen ran, or zero if both are unknown.
func LastUpgrade(fromLog time.Time, st config.State) time.Time {
	if st.LastUpgrade.After(fromLog) {
		return st.LastUpgrade
	}
	return fromLog
}

// Unread returns the news items that are unread: published after the last
// full upgrade (or within news_days if that is unknown) and not marked read.
func Unread(items []news.Item, lastUpgrade time.Time, st config.State, cfg config.Config, now time.Time) []news.Item {
	read := map[string]bool{}
	for _, g := range st.ReadNews() {
		read[g] = true
	}
	rs := news.ReadState{
		LastUpgrade:   lastUpgrade,
		Read:          read,
		Now:           now,
		UnknownWindow: time.Duration(cfg.NewsDays) * 24 * time.Hour,
	}
	return rs.Unread(items)
}

// Updates turns the pending repo and AUR updates into risk input. AUR
// packages that aren't pending (VCS packages, debug splits, packages
// without a newer version) are left out.
func Updates(repo []pacman.Update, aurPkgs []aur.Package) []risk.Update {
	var updates []risk.Update
	for _, u := range repo {
		updates = append(updates, risk.Update{Name: u.Name, OldVersion: u.OldVersion, NewVersion: u.NewVersion})
	}
	for _, p := range aurPkgs {
		if p.Pending && p.Info != nil {
			updates = append(updates, risk.Update{
				Name: p.Name, Base: p.Info.PackageBase,
				OldVersion: p.InstalledVersion, NewVersion: p.Info.Version, AUR: true,
			})
		}
	}
	return updates
}

// Assess applies the risk rules with the user's configuration.
func Assess(updates []risk.Update, unread []news.Item, cfg config.Config, now time.Time) risk.Result {
	return risk.Assess(risk.Input{
		Updates:     updates,
		UnreadNews:  unread,
		Watchlist:   cfg.EffectiveWatchlist(),
		IgnoreMajor: cfg.IgnoreMajor,
		Now:         now,
	})
}

// Sources is the system access Gather needs.
type Sources struct {
	Runner  system.Runner
	Fetcher system.Fetcher
	FS      system.FS
	Config  config.Config
	State   config.State
	// Now is the current time. Nil means time.Now.
	Now func() time.Time
	// Location is the time zone of pacman.log. Nil means time.Local.
	Location *time.Location
}

// Summary is what an upgrade would bring, as far as the sources could tell.
type Summary struct {
	// Repo and AUR count the pending updates of each kind.
	Repo, AUR int
	// Flagged are the pending updates with at least one flag.
	Flagged []risk.Package
	// Interventions are the unread news items requiring manual
	// intervention.
	Interventions []news.Item
	// UnreadNews counts every unread news item.
	UnreadNews int
	// Pacnew counts the .pacnew and .pacsave files waiting to be merged.
	Pacnew int
	// Reboot is set when a kernel is among the updates.
	Reboot bool
	// Errors are the sources that failed. The rest of the summary holds
	// what the others found.
	Errors []error
}

// Gather loads every source concurrently and sums them up. Cached network
// responses are used while fresh, so frequent status bar polling doesn't
// hit the AUR or archlinux.org every time.
func Gather(ctx context.Context, src Sources) Summary {
	now := time.Now
	if src.Now != nil {
		now = src.Now
	}
	loc := time.Local
	if src.Location != nil {
		loc = src.Location
	}

	var (
		wg                       sync.WaitGroup
		repo                     []pacman.Update
		aurPkgs                  []aur.Package
		items                    []news.Item
		lastLog                  time.Time
		pacnew                   []string
		repoErr, aurErr, newsErr error
	)
	wg.Go(func() { repo, repoErr = pacman.CheckUpdates(ctx, src.Runner) })
	if src.Config.AUR {
		wg.Go(func() { aurPkgs, aurErr = LoadAUR(ctx, src.Runner, src.Fetcher, false) })
	}
	wg.Go(func() { items, newsErr = news.Fetch(ctx, src.Fetcher, false) })
	// An unreadable log only makes the last upgrade unknown, and a missing
	// pacdiff only hides the .pacnew count, as in the TUI.
	wg.Go(func() { lastLog, _ = LoadLastUpgrade(src.FS, loc) })
	wg.Go(func() { pacnew, _ = LoadPacnew(ctx, src.Runner) })
	wg.Wait()

	var s Summary
	if newsErr != nil {
		newsErr = fmt.Errorf("news: %w", newsErr)
	}
	for _, err := range []error{repoErr, aurErr, newsErr} {
		if err != nil {
			s.Errors = append(s.Errors, err)
		}
	}

	t := now()
	unread := Unread(items, LastUpgrade(lastLog, src.State), src.State, src.Config, t)
	res := Assess(Updates(repo, aurPkgs), unread, src.Config, t)
	for _, p := range res.Packages {
		if p.AUR {
			s.AUR++
		} else {
			s.Repo++
		}
	}
	s.Flagged = res.Flagged()
	s.Interventions = res.Interventions
	s.UnreadNews = len(unread)
	s.Pacnew = len(pacnew)
	s.Reboot = res.RebootRequired
	return s
}

// Updates is the number of pending updates, repo and AUR.
func (s Summary) Updates() int {
	return s.Repo + s.AUR
}

// Exit codes of --check.
const (
	ExitNothing      = 0
	ExitError        = 1
	ExitUpdates      = 10
	ExitAttention    = 20
	ExitIntervention = 30
)

// ExitCode is the --check exit code. Any failed source makes it ExitError:
// with a source missing, "nothing to do" can't be told apart from "didn't
// look", so cronen doesn't guess.
func (s Summary) ExitCode() int {
	switch {
	case len(s.Errors) > 0:
		return ExitError
	case len(s.Interventions) > 0:
		return ExitIntervention
	case len(s.Flagged) > 0:
		return ExitAttention
	case s.Updates() > 0:
		return ExitUpdates
	}
	return ExitNothing
}
