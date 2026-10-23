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
	fs.BoolVar(&o.check, "check", false, "print a short summary and exit (exit code reflects status)")
	fs.BoolVar(&o.json, "json", false, "print a machine-readable JSON summary and exit")
	fs.BoolVar(&o.waybar, "waybar", false, "with --json, print waybar custom module fields")
	fs.BoolVar(&o.demo, "demo", false, "use bundled fixture data and touch nothing on the system")
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

	if o.check || o.json {
		_, _ = fmt.Fprintln(stderr, "cronen: --check and --json are not implemented yet")
		return 1
	}

	var uiOpts ui.Options
	if o.demo {
		// Fixture data only: no config, state or cache is read or written.
		uiOpts = ui.Options{
			Demo:     true,
			Context:  ctx,
			Runner:   fixtures.Runner(),
			Fetcher:  fixtures.Fetcher(),
			FS:       fixtures.FS(),
			Now:      func() time.Time { return fixtures.Now },
			Location: time.UTC,
		}
	} else {
		uiOpts = realOptions(ctx, stderr)
	}

	opts := append([]tea.ProgramOption{tea.WithContext(ctx), tea.WithOutput(stdout)}, teaOpts...)
	if _, err := tea.NewProgram(ui.New(uiOpts), opts...).Run(); err != nil {
		_, _ = fmt.Fprintln(stderr, "cronen:", err)
		return 1
	}
	return 0
}

// realOptions wires the TUI to the real system. Problems with the config or
// state file are reported on stderr and fall back to defaults; they never
// stop cronen from starting.
func realOptions(ctx context.Context, stderr io.Writer) ui.Options {
	fsys := system.OSFS{}
	opts := ui.Options{
		Context: ctx,
		Runner:  system.ExecRunner{},
		FS:      fsys,
	}
	cfg := config.Default()
	paths, err := config.ResolvePaths(os.Getenv)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "cronen:", err)
		opts.Fetcher = system.NewHTTPClient("")
		opts.Config = &cfg
		return opts
	}
	if cfg, err = config.Load(fsys, paths.Config); err != nil {
		_, _ = fmt.Fprintln(stderr, "cronen:", err)
	}
	opts.Config = &cfg
	if opts.State, err = config.LoadState(fsys, paths.State); err != nil {
		_, _ = fmt.Fprintln(stderr, "cronen:", err)
	}
	opts.Fetcher = system.NewHTTPClient(paths.CacheDir)
	return opts
}
