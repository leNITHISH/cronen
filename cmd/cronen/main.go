// Command cronen shows what a pacman -Syu is about to do before you run it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"

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

	uiOpts := ui.Options{
		Demo:    o.demo,
		Runner:  system.ExecRunner{},
		Fetcher: system.NewHTTPClient(""),
		FS:      system.OSFS{},
	}
	if o.demo {
		uiOpts.Runner = fixtures.Runner()
		uiOpts.Fetcher = fixtures.Fetcher()
		uiOpts.FS = fixtures.FS()
	}

	opts := append([]tea.ProgramOption{tea.WithContext(ctx), tea.WithOutput(stdout)}, teaOpts...)
	if _, err := tea.NewProgram(ui.New(uiOpts), opts...).Run(); err != nil {
		_, _ = fmt.Fprintln(stderr, "cronen:", err)
		return 1
	}
	return 0
}
