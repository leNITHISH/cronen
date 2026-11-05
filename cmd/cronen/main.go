// Command cronen shows what a pacman -Syu is about to do before you run it.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/ops"
	"github.com/leNITHISH/cronen/internal/pacman"
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
	// install holds the package names of `cronen install <pkg>...`.
	install []string
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
       cronen [--demo] install <package>...

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
  30  critical: an unread news item requires manual intervention, or a
      preflight check found a critical problem (such as a full /boot)
  1   error: a source (checkupdates, the AUR, the news feed, the preview) failed
--json always exits 0 once it has printed; failures are in its "errors".
Bad flags exit 2.

cronen install opens the TUI in the install helper ("I need X now"): it offers
the full upgrade plus the package, or (experimental, archive_install = true)
the package as of your last full upgrade. It never runs pacman -Sy <package>.

Examples:
  cronen                  open the TUI
  cronen install htop     install a package the safe way
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
		if fs.Arg(0) != "install" {
			return o, fmt.Errorf("unexpected arguments: %v", fs.Args())
		}
		o.install = fs.Args()[1:]
		if len(o.install) == 0 {
			return o, errors.New("install needs at least one package name")
		}
		if err := ops.CheckInstallNames(o.install); err != nil {
			return o, fmt.Errorf("install: %w", err)
		}
		if o.check || o.json || o.waybar {
			return o, errors.New("install can't be combined with --check, --json or --waybar")
		}
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
		e.recordPending(s, stderr)
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
	if _, err := tea.NewProgram(ui.New(e.uiOptions(ctx, o.install)), opts...).Run(); err != nil {
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
	disk      system.Disk
	cfg       config.Config
	state     config.State
	statePath string
	// syncDB is the database checkupdates syncs.
	syncDB string
	// opener reads ELF files for the rebuild scan, and cacheDir holds its
	// cache (empty: none).
	opener   system.Opener
	cacheDir string
	now      func() time.Time
	loc      *time.Location
	// home is the user's home directory, for ~/ in extra_cache_dirs.
	home string
	// archiveBase is where the install helper's option B makes its fresh
	// directories, and nonce names them.
	archiveBase string
	nonce       func() string
	exec        func(argv []string, done tea.ExecCallback) tea.Cmd
}

// demoEnv serves fixture data only: no config, state or cache is read or
// written, and upgrading runs nothing.
func demoEnv() env {
	var st config.State
	for _, x := range fixtures.DemoUpgradeSamples() {
		st.AddUpgradeSample(x)
	}
	for _, x := range fixtures.DemoPrefetchSamples() {
		st.AddPrefetchSample(x)
	}
	return env{
		state:       st,
		demo:        true,
		runner:      fixtures.Runner(),
		fetcher:     fixtures.Fetcher(),
		fs:          fixtures.FS(),
		disk:        fixtures.Disk(),
		syncDB:      fixtures.SyncDBPath,
		opener:      fixtures.Opener(),
		cfg:         config.Default(),
		archiveBase: fixtures.ArchiveBase,
		nonce:       demoNonce(),
		now:         func() time.Time { return fixtures.Now },
		loc:         time.UTC,
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
		disk:   system.OSDisk{},
		cfg:    config.Default(),
		syncDB: pacman.SyncDBPath(os.Getenv, os.Getuid()),
		opener: system.OSOpener{},
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
	e.home, _ = os.UserHomeDir()
	e.statePath = paths.State
	e.fetcher = system.NewHTTPClient(paths.CacheDir)
	e.cacheDir = paths.CacheDir
	e.archiveBase = filepath.Join(paths.CacheDir, "archive")
	e.nonce = randomNonce
	return e
}

// recordPending keeps the day's pending download size in the state, for the
// growth trend, when a --check or --json run (a status bar poll, say) is the
// first to see the day's preview. The state file is only written when the
// sample changed, so polling doesn't rewrite it every time. Demo mode
// writes nothing.
func (e env) recordPending(s summary.Summary, stderr io.Writer) {
	if e.demo || e.statePath == "" || !s.Previewed {
		return
	}
	loc := e.loc
	if loc == nil {
		loc = time.Local
	}
	now := time.Now()
	if e.now != nil {
		now = e.now()
	}
	// Read the file again: a TUI may have saved since this run started, and
	// its changes must not be lost. An unreadable file is left alone.
	st, err := config.LoadState(e.fs, e.statePath)
	if err != nil {
		return
	}
	if !summary.RecordPending(&st, s.Transaction, now, loc) {
		return
	}
	if err := config.SaveState(e.fs, e.statePath, st); err != nil {
		_, _ = fmt.Fprintln(stderr, "cronen:", err)
	}
}

func (e env) uiOptions(ctx context.Context, install []string) ui.Options {
	cfg := e.cfg
	return ui.Options{
		Demo:      e.demo,
		Context:   ctx,
		Runner:    e.runner,
		Fetcher:   e.fetcher,
		FS:        e.fs,
		Disk:      e.disk,
		Config:    &cfg,
		State:     e.state,
		StatePath: e.statePath,
		SyncDB:    e.syncDB,
		Opener:    e.opener,
		CacheDir:  e.cacheDir,
		Exec:      e.exec,
		Home:      e.home,
		Now:       e.now,
		Location:  e.loc,

		Install:     install,
		ArchiveBase: e.archiveBase,
		Nonce:       e.nonce,
	}
}

func (e env) sources() summary.Sources {
	return summary.Sources{
		Runner:   e.runner,
		Fetcher:  e.fetcher,
		FS:       e.fs,
		Disk:     e.disk,
		Config:   e.cfg,
		State:    e.state,
		SyncDB:   e.syncDB,
		Opener:   e.opener,
		CacheDir: e.cacheDir,
		Now:      e.now,
		Location: e.loc,
	}
}

// randomNonce is 8 random bytes in hex, for a new archive directory's name.
// An empty result (no randomness) makes option B refuse.
func randomNonce() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// demoNonce counts, so the demo's fake filesystem never sees a name twice.
func demoNonce() func() string {
	n := 0
	return func() string {
		n++
		return fmt.Sprintf("demo%04d", n)
	}
}
