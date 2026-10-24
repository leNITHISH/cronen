package ui

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/system"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/")

// fixtureOptions returns Options serving the fixture system with a fixed
// clock and time zone, so snapshots don't depend on the machine.
func fixtureOptions() Options {
	return Options{
		Demo:     true,
		Runner:   fixtures.Runner(),
		Fetcher:  fixtures.Fetcher(),
		FS:       fixtures.FS(),
		Now:      func() time.Time { return fixtures.Now },
		Location: time.UTC,
	}
}

// send feeds msg to m, then runs any commands it returns and feeds their
// results back, except spinner ticks and terminal queries, which never
// resolve in a test.
func send(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, cmd := m.Update(msg)
	return run(t, next.(Model), cmd)
}

func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			m = run(t, m, c)
		}
	case repoMsg, aurMsg, newsMsg, logMsg, pacnewMsg, archwebMsg:
		m = send(t, m, msg)
	}
	return m
}

func press(s string) tea.KeyPressMsg {
	switch s {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "ctrl+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

// sized returns a fresh model at the given terminal size.
func sized(t *testing.T, opts Options, w, h int) Model {
	t.Helper()
	return send(t, New(opts), tea.WindowSizeMsg{Width: w, Height: h})
}

// loaded returns a model with every source loaded from opts.
func loaded(t *testing.T, opts Options, w, h int) Model {
	t.Helper()
	m := sized(t, opts, w, h)
	for _, c := range m.loadCmds(false) {
		m = run(t, m, c)
	}
	return m
}

func plain(m Model) string {
	return ansi.Strip(m.View().Content)
}

// golden compares got with testdata/name.golden, or rewrites it with
// -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/ui -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("%s does not match the golden file (run with -update to accept)\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// checkSize asserts the view fills exactly w×h cells.
func checkSize(t *testing.T, m Model, w, h int) {
	t.Helper()
	lines := strings.Split(m.View().Content, "\n")
	if len(lines) != h {
		t.Errorf("%dx%d: view has %d lines", w, h, len(lines))
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Errorf("%dx%d: line %d is %d cells wide: %q", w, h, i, got, ansi.Strip(l))
		}
	}
}

func TestSnapshots(t *testing.T) {
	const w, h = 100, 30

	failing := fixtureOptions()
	runner := fixtures.Runner()
	runner.Set("checkupdates", fixtures.CheckupdatesErrorResult())
	failing.Runner = runner
	fetcher := fixtures.Fetcher()
	fetcher.Set(fixtures.NewsURL, system.FakeResponse{Status: 500})
	failing.Fetcher = fetcher

	tests := []struct {
		name  string
		model func(t *testing.T) Model
	}{
		{"loading", func(t *testing.T) Model { return sized(t, fixtureOptions(), w, h) }},
		{"loaded", func(t *testing.T) Model { return loaded(t, fixtureOptions(), w, h) }},
		{"errors", func(t *testing.T) Model { return loaded(t, failing, w, h) }},
		{"news", func(t *testing.T) Model {
			return send(t, loaded(t, fixtureOptions(), w, h), press("n"))
		}},
		{"news-second-item", func(t *testing.T) Model {
			m := send(t, loaded(t, fixtureOptions(), w, h), press("n"))
			return send(t, m, press("j"))
		}},
		{"help", func(t *testing.T) Model {
			return send(t, loaded(t, fixtureOptions(), w, h), press("?"))
		}},
		{"aur-orphan-selected", func(t *testing.T) Model {
			m := loaded(t, fixtureOptions(), w, h)
			return send(t, m, press("G"))
		}},
		{"small", func(t *testing.T) Model { return loaded(t, fixtureOptions(), 60, 16) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			golden(t, tt.name, plain(tt.model(t)))
		})
	}
}

func TestResize(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 30)
	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 16}, {40, 10}, {20, 5}, {200, 60}} {
		m = send(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, k := range []string{"", "n", "?", "?", "n"} {
			if k != "" {
				m = send(t, m, press(k))
			}
			checkSize(t, m, size[0], size[1])
		}
	}
}

func TestViewBeforeWindowSize(t *testing.T) {
	m := New(fixtureOptions())
	checkSize(t, m, defaultWidth, defaultHeight)
	v := m.View()
	if !strings.Contains(v.Content, "cronen (demo)") || !strings.Contains(v.Content, "q quit") {
		t.Fatalf("view = %q", ansi.Strip(v.Content))
	}
	if !v.AltScreen {
		t.Error("want alt screen")
	}
}

func TestQuitKeys(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyPressMsg
		quit bool
	}{
		{"q", press("q"), true},
		{"ctrl+c", press("ctrl+c"), true},
		{"j", press("j"), false},
		{"c", press("c"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cmd := New(Options{}).Update(tt.msg)
			gotQuit := cmd != nil && isQuit(cmd())
			if gotQuit != tt.quit {
				t.Fatalf("quit = %v, want %v", gotQuit, tt.quit)
			}
		})
	}
}

func isQuit(msg tea.Msg) bool {
	_, ok := msg.(tea.QuitMsg)
	return ok
}

func TestFlagsAndBanner(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 30)
	// The banner arrived after the window size: the panes must have shrunk
	// to make room for it.
	if bh := m.bodyHeight(); m.detail.Height() != bh || m.newsView.Height() != bh {
		t.Errorf("viewport heights %d, %d; want body height %d", m.detail.Height(), m.newsView.Height(), bh)
	}
	if n := len(m.result.Interventions); n != 1 {
		t.Fatalf("interventions = %d, want 1", n)
	}
	out := plain(m)
	if !strings.Contains(out, "1 news item requires manual intervention") {
		t.Errorf("no banner in:\n%s", out)
	}
	// The first entry is the most severe: linux-firmware, mentioned by the
	// manual intervention post.
	e, ok := m.updates.selected()
	if !ok || e.pkg.Name != "linux-firmware" {
		t.Fatalf("selected = %+v, want linux-firmware", e)
	}
	if !strings.Contains(out, "Manual intervention in news") {
		t.Errorf("detail pane lacks the reason:\n%s", out)
	}
}

// News rules must not run until pacman.log is read: before that, every
// recent post would count as unread.
func TestNewsWaitsForLog(t *testing.T) {
	m := sized(t, fixtureOptions(), 100, 30)
	m = send(t, m, repoMsg{gen: m.gen, updates: nil})
	cmds := m.loadCmds(false)
	for _, c := range cmds {
		if msg, ok := c().(newsMsg); ok {
			m = send(t, m, msg)
		}
	}
	if m.newsDecided || len(m.result.Interventions) != 0 {
		t.Fatalf("news rules ran before pacman.log was read")
	}
	if !strings.Contains(plain(m), "checking news") {
		t.Errorf("want a checking-news note:\n%s", plain(m))
	}
}

func TestStaleGenerationIgnored(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 30)
	before := len(m.result.Packages)
	m = send(t, m, press("r"))
	if m.gen != 2 {
		t.Fatalf("gen = %d after refresh, want 2", m.gen)
	}
	m = send(t, m, repoMsg{gen: 1, err: errors.New("stale")})
	if m.repoSec.err != nil {
		t.Fatal("a stale result was applied")
	}
	if len(m.result.Packages) != before {
		t.Errorf("packages = %d, want %d", len(m.result.Packages), before)
	}
}

// The selection follows the package, not the row, when flags arrive and
// the package moves between sections.
func TestSelectionFollowsPackage(t *testing.T) {
	m := sized(t, fixtureOptions(), 100, 30)
	cmds := m.loadCmds(false)
	var deferred []tea.Msg
	for _, c := range cmds {
		switch msg := c().(type) {
		case newsMsg, logMsg:
			deferred = append(deferred, msg)
		default:
			m = send(t, m, msg)
		}
	}
	for m.updates.entries[m.updates.sel].pkg.Name != "linux" {
		before := m.updates.sel
		m = send(t, m, press("j"))
		if m.updates.sel == before {
			t.Fatal("linux not in the list")
		}
	}
	for _, msg := range deferred {
		m = send(t, m, msg)
	}
	if e, _ := m.updates.selected(); e.pkg.Name != "linux" {
		t.Fatalf("selected %q after news arrived, want nvidia-utils", e.pkg.Name)
	}
}

func TestFocusScrollsDetail(t *testing.T) {
	m := loaded(t, fixtureOptions(), 80, 12)
	sel := m.updates.sel
	m = send(t, m, press("tab"))
	m = send(t, m, press("j"))
	if m.updates.sel != sel {
		t.Error("j moved the list while the detail pane had focus")
	}
	if m.detail.YOffset() != 1 {
		t.Errorf("detail offset = %d, want 1", m.detail.YOffset())
	}
	m = send(t, m, press("tab"))
	m = send(t, m, press("j"))
	if m.updates.sel != sel+1 {
		t.Errorf("sel = %d, want %d", m.updates.sel, sel+1)
	}
}

func TestBackgroundColor(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 30)
	m = send(t, m, press("n"))
	m = send(t, m, tea.BackgroundColorMsg{Color: lightBackground{}})
	if m.isDark {
		t.Fatal("still dark after a light background report")
	}
	if !strings.Contains(plain(m), "linux-firmware") {
		t.Errorf("news view lost its content:\n%s", plain(m))
	}
}

type lightBackground struct{}

func (lightBackground) RGBA() (r, g, b, a uint32) { return 0xffff, 0xffff, 0xffff, 0xffff }

func entryNames(m Model) map[string]entry {
	out := map[string]entry{}
	for _, e := range m.updates.entries {
		out[e.pkg.Name] = e
	}
	return out
}

func TestDebugSplits(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 30)
	names := entryNames(m)
	for _, n := range []string{"paru-debug", "zstd-debug"} {
		if _, ok := names[n]; ok {
			t.Errorf("%s listed, want debug splits hidden by default", n)
		}
	}
	if strings.Contains(plain(m), "-debug") {
		t.Errorf("debug split visible:\n%s", plain(m))
	}

	cfg := config.Default()
	cfg.ShowDebug = true
	opts := fixtureOptions()
	opts.Config = &cfg
	m = loaded(t, opts, 100, 30)
	names = entryNames(m)
	for _, n := range []string{"paru-debug", "zstd-debug"} {
		e, ok := names[n]
		if !ok {
			t.Fatalf("%s not listed with show_debug", n)
		}
		if got := aurTag(*e.aur); got != "debug" {
			t.Errorf("%s tag = %q, want \"debug\" (never \"not in AUR\")", n, got)
		}
		if e.pkg.Severity() != risk.None {
			t.Errorf("%s flagged", n)
		}
	}
	if out := plain(m); !strings.Contains(out, "AUR (5 + 1 VCS + 2 debug)") {
		t.Errorf("AUR header should count debug splits apart:\n%s", out)
	}
}

func TestVCSPackages(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 30)
	e, ok := entryNames(m)["neovim-git"]
	if !ok {
		t.Fatal("neovim-git not listed")
	}
	if e.pending() || aurTag(*e.aur) != "VCS" {
		t.Errorf("neovim-git: pending=%v tag=%q, want a non-pending VCS entry", e.pending(), aurTag(*e.aur))
	}
	for _, p := range m.result.Packages {
		if p.Name == "neovim-git" {
			t.Error("VCS package counted as an update")
		}
	}
	out := plain(m)
	// 16 repo + 2 regular AUR updates; the newer neovim-git snapshot on the
	// AUR doesn't count.
	if !strings.Contains(out, "18 updates") || !strings.Contains(out, "AUR (5 + 1 VCS)") {
		t.Errorf("counts include the VCS package:\n%s", out)
	}
}

func TestCheckupdatesMissing(t *testing.T) {
	opts := fixtureOptions()
	r := fixtures.Runner()
	r.Set("checkupdates", system.FakeResult{Err: system.ErrCommandNotFound})
	opts.Runner = r
	const hint = "✗ checkupdates not found — install pacman-contrib"
	// The list widens to fit the hint from 91 columns; narrower terminals
	// wrap it rather than cut it.
	for _, size := range [][2]int{{91, 24}, {100, 30}, {140, 30}} {
		m := loaded(t, opts, size[0], size[1])
		var notes []string
		for _, row := range m.updates.rows {
			if row.kind == rowNote && row.err {
				notes = append(notes, row.text)
			}
		}
		if len(notes) != 1 || notes[0] != hint {
			t.Errorf("%dx%d: error notes = %q, want the one-line hint", size[0], size[1], notes)
		}
		if !strings.Contains(plain(m), "\n "+hint) {
			t.Errorf("%dx%d: hint not on one line:\n%s", size[0], size[1], plain(m))
		}
		checkSize(t, m, size[0], size[1])
	}
	m := loaded(t, opts, 80, 24)
	if !strings.Contains(plain(m), "install") || !strings.Contains(plain(m), "pacman-contrib") {
		t.Errorf("80x24: hint lost:\n%s", plain(m))
	}
	checkSize(t, m, 80, 24)
}

func TestDetailLinks(t *testing.T) {
	m := loaded(t, fixtureOptions(), 80, 30)
	// Select paru, whose changelog URL is wider than the detail pane.
	for e, _ := m.updates.selected(); e.pkg.Name != "paru"; e, _ = m.updates.selected() {
		before := m.updates.sel
		m = send(t, m, press("j"))
		if m.updates.sel == before {
			t.Fatal("paru not in the list")
		}
	}
	const changelog = "https://aur.archlinux.org/cgit/aur.git/log/?h=paru"
	content := m.View().Content
	if !strings.Contains(content, ansi.SetHyperlink(changelog)) {
		t.Errorf("no OSC 8 hyperlink to the full changelog URL")
	}
	out := plain(m)
	var linkLines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "changelog") {
			linkLines = append(linkLines, l)
		}
	}
	if len(linkLines) != 1 || !strings.Contains(linkLines[0], "https://aur.archlinux.org/") || !strings.Contains(linkLines[0], "…") {
		t.Errorf("want the changelog URL truncated on its own line, got %q", linkLines)
	}
	if strings.Contains(out, "?h=paru\n") && !strings.Contains(linkLines[0], "?h=paru") {
		t.Errorf("URL wrapped onto the next line:\n%s", out)
	}
	checkSize(t, m, 80, 30)
}

func TestHyperlinkStripsControlCharacters(t *testing.T) {
	got := hyperlink("https://evil.example/\x1b]52;c;aGk=\x07x", 80)
	// Only the OSC 8 sequence itself may contain ESC and BEL.
	want := ansi.SetHyperlink("https://evil.example/]52;c;aGk=x") + "https://evil.example/]52;c;aGk=x" + ansi.ResetHyperlink()
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestVersionColumn(t *testing.T) {
	tests := []struct {
		name, old, new string
		w              int
		want           string
	}{
		{"fits", "6.17", "→6.18", 20, "linux      6.17→6.18"},
		{"old truncated first", "2.42+r17+g6f0cf3d0", "→2.42+r33", 26, "glibc 2.42+r17+g…→2.42+r33"},
		{"then the name", "20251011", "→20251021", 23, "linux-fir… 20…→20251021"},
		{"old dropped", "20251011", "→20251021", 18, "linux-f… →20251021"},
		{"name only when new can't fit", "1", "→2.42+r33+gc7ce5b4", 20, "linux-firmware      "},
		{"status tag", "", "not in AUR", 26, "gnome-shell-ex… not in AUR"},
	}
	names := map[string]string{
		"fits": "linux", "old truncated first": "glibc", "then the name": "linux-firmware",
		"old dropped": "linux-firmware", "name only when new can't fit": "linux-firmware",
		"status tag": "gnome-shell-extension-legacy",
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := versionColumn(names[tt.name], tt.old, tt.new, tt.w)
			if ansi.StringWidth(got) != tt.w {
				t.Errorf("width %d, want %d: %q", ansi.StringWidth(got), tt.w, got)
			}
			if strings.TrimRight(got, " ") != strings.TrimRight(tt.want, " ") {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if strings.Contains(got, "→") && !strings.HasSuffix(got, tt.new) {
				t.Errorf("new version %q cut or not right-aligned: %q", tt.new, got)
			}
		})
	}
}
