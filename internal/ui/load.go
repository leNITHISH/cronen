package ui

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/archweb"
	"github.com/leNITHISH/cronen/internal/aur"
	"github.com/leNITHISH/cronen/internal/news"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/system"
)

// Every load message carries the generation of the load that produced it.
// A refresh bumps the generation, so results from an older load that arrive
// late are dropped instead of overwriting newer ones.

type repoMsg struct {
	gen     int
	updates []pacman.Update
	err     error
}

type aurMsg struct {
	gen  int
	pkgs []aur.Package
	err  error
}

type newsMsg struct {
	gen   int
	items []news.Item
	err   error
}

type logMsg struct {
	gen int
	// last is the time of the last full upgrade, or zero if unknown.
	last time.Time
	err  error
}

type pacnewMsg struct {
	gen   int
	files []string
	err   error
}

type archwebMsg struct {
	name string
	pkg  archweb.Package
	err  error
}

func loadRepo(ctx context.Context, r system.Runner, gen int) tea.Cmd {
	return func() tea.Msg {
		updates, err := pacman.CheckUpdates(ctx, r)
		return repoMsg{gen: gen, updates: updates, err: err}
	}
}

func loadAUR(ctx context.Context, r system.Runner, f system.Fetcher, gen int, fresh bool) tea.Cmd {
	return func() tea.Msg {
		installed, err := pacman.ForeignPackages(ctx, r)
		if err != nil {
			return aurMsg{gen: gen, err: err}
		}
		c := aur.NewClient(f, r)
		// Every installed name tells debug splits of repo packages apart.
		// Without it, only splits of foreign packages are recognized.
		if all, err := pacman.InstalledNames(ctx, r); err == nil {
			c.Installed = all
		}
		pkgs, err := c.Check(ctx, installed, fresh)
		return aurMsg{gen: gen, pkgs: pkgs, err: err}
	}
}

func loadNews(ctx context.Context, f system.Fetcher, gen int, fresh bool) tea.Cmd {
	return func() tea.Msg {
		items, err := news.Fetch(ctx, f, fresh)
		return newsMsg{gen: gen, items: items, err: err}
	}
}

func loadLog(fsys system.FS, loc *time.Location, gen int) tea.Cmd {
	return func() tea.Msg {
		last, err := pacman.ReadLastFullUpgrade(fsys, pacman.LogPath, loc)
		if errors.Is(err, pacman.ErrNoFullUpgrade) {
			// Not an error worth showing: the time is just unknown.
			err = nil
		}
		return logMsg{gen: gen, last: last, err: err}
	}
}

func loadPacnew(ctx context.Context, r system.Runner, gen int) tea.Cmd {
	return func() tea.Msg {
		out, _, err := r.Run(ctx, "pacdiff", "-o")
		if err != nil {
			return pacnewMsg{gen: gen, err: fmt.Errorf("pacdiff: %w", err)}
		}
		var files []string
		sc := bufio.NewScanner(bytes.NewReader(out))
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" {
				files = append(files, line)
			}
		}
		return pacnewMsg{gen: gen, files: files}
	}
}

func lookupArchweb(ctx context.Context, f system.Fetcher, name string, fresh bool) tea.Cmd {
	return func() tea.Msg {
		pkg, err := archweb.NewClient(f).Lookup(ctx, name, fresh)
		return archwebMsg{name: name, pkg: pkg, err: err}
	}
}
