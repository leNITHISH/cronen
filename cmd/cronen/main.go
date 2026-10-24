// Command cronen shows what a pacman -Syu is about to do before you run it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/summary"
	"github.com/leNITHISH/cronen/internal/system"
	"github.com/leNITHISH/cronen/internal/ui"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

type options struct {
	check  bool
	json   bool
	waybar bool
	demo   bool
}

func parseFlags(args []string, stderr io.Writer) (options, error) {
	var o options
	fs := flag.NewFlagSet("cronen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: cronen [flags]")
		_, _ = fmt.Fprintln(stderr, "\nShows pending updates, Arch news and anything needing attention before pacman -Syu.")
		_, _ = fmt.Fprintln(stderr, "\nFlags:")
		fs.PrintDefaults()
	}
	fs.BoolVar(&o.check, "check", false, "print a short summary and exit with 0 (nothing to do), 10 (updates),\n20 (attention needed), 30 (manual intervention) or 1 (error)")
	fs.BoolVar(&o.json, "json", false, "print a machine-readable JSON summary and exit")
	fs.BoolVar(&o.waybar, "waybar", false, "with --json, print text, tooltip and class for a waybar custom module")
	fs.BoolVar(&o.demo, "demo", false, "use bundled fixture data and touch nothing on the system\n(also works with --check and --json)")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() > 0 {
		return o, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if o.waybar && !o.json {
		return o, errors.New("--waybar requires --json")
	}
	if o.check && o.json {
		return o, errors.New("--check and --json are mutually exclusive")
	}
	return o, nil
}

// run is main without the process exit, so it can be tested. Extra tea
// options are appended when starting the TUI.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, teaOpts ...tea.ProgramOption) int {
	o, err := parseFlags(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "cronen:", err)
		return 2
	}

	var e env
	if o.demo {
		e = demoEnv()
	} else {
		e = realEnv(stderr)
	}

	if o.check || o.json {
		s := summary.Gather(ctx, e.sources())
		switch {
		case o.check:
			_, _ = io.WriteString(stdout, s.Text())
			return s.ExitCode()
		case o.waybar:
			_, _ = fmt.Fprintf(stdout, "%s\n", s.Waybar())
		default:
			_, _ = fmt.Fprintf(stdout, "%s\n", s.JSON())
		}
		// The JSON carries any errors itself; a status bar only needs it
		// printed.
		return 0
	}

	opts := append([]tea.ProgramOption{tea.WithContext(ctx), tea.WithOutput(stdout)}, teaOpts...)
	if _, err := tea.NewProgram(ui.New(e.uiOptions(ctx)), opts...).Run(); err != nil {
		_, _ = fmt.Fprintln(stderr, "cronen:", err)
		return 1
	}
	return 0
}

// env is the system cronen runs against: the real one, or the fixtures.
type env struct {
	demo      bool
	runner    system.Runner
	fetcher   system.Fetcher
	fs        system.FS
	cfg       config.Config
	state     config.State
	statePath string
	now       func() time.Time
	loc       *time.Location
	exec      func(argv []string, done tea.ExecCallback) tea.Cmd
}

// demoEnv serves fixture data only: no config, state or cache is read or
// written, and upgrading runs nothing.
func demoEnv() env {
	return env{
		demo:    true,
		runner:  fixtures.Runner(),
		fetcher: fixtures.Fetcher(),
		fs:      fixtures.FS(),
		cfg:     config.Default(),
		now:     func() time.Time { return fixtures.Now },
		loc:     time.UTC,
		exec: func(_ []string, done tea.ExecCallback) tea.Cmd {
			return func() tea.Msg { return done(nil) }
		},
	}
}

// realEnv wires cronen to the real system. Problems with the config or
// state file are reported on stderr and fall back to defaults; they never
// stop cronen from starting.
func realEnv(stderr io.Writer) env {
	fsys := system.OSFS{}
	e := env{
		runner: system.ExecRunner{},
		fs:     fsys,
		cfg:    config.Default(),
		exec: func(argv []string, done tea.ExecCallback) tea.Cmd {
			return tea.ExecProcess(system.InteractiveCommand(argv), done)
		},
	}
	paths, err := config.ResolvePaths(os.Getenv)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "cronen:", err)
		e.fetcher = system.NewHTTPClient("")
		return e
	}
	if e.cfg, err = config.Load(fsys, paths.Config); err != nil {
		_, _ = fmt.Fprintln(stderr, "cronen:", err)
	}
	if e.state, err = config.LoadState(fsys, paths.State); err != nil {
		_, _ = fmt.Fprintln(stderr, "cronen:", err)
	}
	e.statePath = paths.State
	e.fetcher = system.NewHTTPClient(paths.CacheDir)
	return e
}

func (e env) uiOptions(ctx context.Context) ui.Options {
	cfg := e.cfg
	return ui.Options{
		Demo:      e.demo,
		Context:   ctx,
		Runner:    e.runner,
		Fetcher:   e.fetcher,
		FS:        e.fs,
		Config:    &cfg,
		State:     e.state,
		StatePath: e.statePath,
		Exec:      e.exec,
		Now:       e.now,
		Location:  e.loc,
	}
}

func (e env) sources() summary.Sources {
	return summary.Sources{
		Runner:   e.runner,
		Fetcher:  e.fetcher,
		FS:       e.fs,
		Config:   e.cfg,
		State:    e.state,
		Now:      e.now,
		Location: e.loc,
	}
}
