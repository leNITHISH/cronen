package ui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/archweb"
	"github.com/leNITHISH/cronen/internal/aur"
	"github.com/leNITHISH/cronen/internal/news"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/summary"
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
		pkgs, err := summary.LoadAUR(ctx, r, f, fresh)
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
		last, err := summary.LoadLastUpgrade(fsys, loc)
		return logMsg{gen: gen, last: last, err: err}
	}
}

func loadPacnew(ctx context.Context, r system.Runner, gen int) tea.Cmd {
	return func() tea.Msg {
		files, err := summary.LoadPacnew(ctx, r)
		return pacnewMsg{gen: gen, files: files, err: err}
	}
}

func lookupArchweb(ctx context.Context, f system.Fetcher, name string, fresh bool) tea.Cmd {
	return func() tea.Msg {
		pkg, err := archweb.NewClient(f).Lookup(ctx, name, fresh)
		return archwebMsg{name: name, pkg: pkg, err: err}
	}
}
