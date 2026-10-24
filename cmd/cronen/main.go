// Command cronen shows what a pacman -Syu is about to do before you run it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
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
	check   bool
	json    bool
	waybar  bool
	demo    bool
	version bool
}

// version is set at build time with -ldflags "-X main.version=...". A
// plain go build or go install falls back to the module version.
var version = ""

func versionString() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

const usageHeader = `Usage: cronen [--demo] [--check | --json [--waybar]]

Shows pending repo and AUR updates, unread Arch news and anything that needs
attention before you run pacman -Syu. cronen only reads; it never runs
pacman -Sy. Without flags it starts the TUI: press ? there for the keys and
U to hand off to the upgrade command.

Flags:
`

const usageFooter = `
Exit codes of --check:
  0   nothing to do
  10  updates available
  20  attention needed: at least one update is flagged
  30  an unread news item requires manual intervention
  1   error: a source (checkupdates, the AUR, the news feed) failed
--json always exits 0 once it has printed; failures are in its "errors".
Bad flags exit 2.

Examples:
  cronen                  open the TUI
  cronen --demo           try the TUI on sample data
  cronen --check          one-shot summary for scripts or a timer
  cronen --json --waybar  output for a waybar custom module

Files:
  $XDG_CONFIG_HOME/cronen/config.toml  config (default ~/.config/cronen/)
  $XDG_STATE_HOME/cronen/state.json    read news, last upgrade (~/.local/state/cronen/)
  $XDG_CACHE_HOME/cronen/              news and AUR cache, 15 min (~/.cache/cronen/)

See cronen(1) for the config keys.
`

// parseFlags prints the usage to stdout for --help, so it can be paged.
// Other errors are left to the caller.
func parseFlags(args []string, stdout io.Writer) (options, error) {
	var o options
	fs := flag.NewFlagSet("cronen", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&o.check, "check", false, "print a short summary and exit with a status code (see below)")
	fs.BoolVar(&o.json, "json", false, "print a machine-readable JSON summary and exit")
	fs.BoolVar(&o.waybar, "waybar", false, "with --json, print text, tooltip and class for a waybar custom module")
	fs.BoolVar(&o.demo, "demo", false, "use bundled sample data and touch nothing on the system;\nalso works with --check and --json")
	fs.BoolVar(&o.version, "version", false, "print the version and exit")
	fs.Usage = func() {}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(fs, stdout)
		}
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

// printUsage prints the flags with the double dashes the docs use, which
// flag.PrintDefaults doesn't.
func printUsage(fs *flag.FlagSet, w io.Writer) {
	var b strings.Builder
	b.WriteString(usageHeader)
	fs.VisitAll(func(f *flag.Flag) {
		lines := strings.Split(f.Usage, "\n")
		fmt.Fprintf(&b, "  --%-9s %s\n", f.Name, lines[0])
		for _, l := range lines[1:] {
			fmt.Fprintf(&b, "  %-11s %s\n", "", l)
		}
	})
	b.WriteString(usageFooter)
	_, _ = io.WriteString(w, b.String())
}

// run is main without the process exit, so it can be tested. Extra tea
// options are appended when starting the TUI.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, teaOpts ...tea.ProgramOption) int {
	o, err := parseFlags(args, stdout)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "cronen:", err)
		_, _ = fmt.Fprintln(stderr, "Run cronen --help for usage.")
		return 2
	}
	if o.version {
		_, _ = fmt.Fprintln(stdout, "cronen", versionString())
		return 0
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
