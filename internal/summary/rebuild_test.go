package summary

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/rebuild"
	"github.com/leNITHISH/cronen/internal/system"
)

func fixtureTransaction(t *testing.T) pacman.Transaction {
	t.Helper()
	r := fixtures.Runner()
	updates, err := pacman.CheckUpdates(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := LoadPreview(context.Background(), r, fixtures.FS(), fixtures.SyncDBPath, updates, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

func show(fs []rebuild.Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, fmt.Sprintf("%s %s <- %s (%s)", f.Package, f.Signal, f.Culprit, f.Detail))
	}
	return out
}

// Each signal catches one fixture package, and the false positives (an
// unchanged soname, a debug split, a VCS package that doesn't link the
// library) catch none.
func TestLoadRebuildFixtures(t *testing.T) {
	src := RebuildSources{Runner: fixtures.Runner(), Opener: fixtures.Opener(), FS: fixtures.FS(), ScanELF: true}
	got, err := LoadRebuild(context.Background(), src, fixtureTransaction(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"gnome-shell-extension-legacy metadata <- zstd (libzstd.so.1)",
		"visual-studio-code-bin elf <- zstd (libzstd.so.1)",
		"python-pywal module-dir <- python (/usr/lib/python3.13/)",
	}
	slices.Sort(want)
	gotS := show(got)
	slices.Sort(gotS)
	if !slices.Equal(gotS, want) {
		t.Errorf("findings =\n %q\nwant\n %q", gotS, want)
	}
}

func TestLoadRebuildELFScanOff(t *testing.T) {
	src := RebuildSources{Runner: fixtures.Runner(), Opener: fixtures.Opener(), FS: fixtures.FS(), ScanELF: false}
	got, err := LoadRebuild(context.Background(), src, fixtureTransaction(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range got {
		if f.Signal == rebuild.SignalELF {
			t.Errorf("ELF finding with the scan off: %v", show([]rebuild.Finding{f}))
		}
	}
	if len(got) != 2 {
		t.Errorf("findings = %q, want the metadata and module-dir ones", show(got))
	}

	// Nor does a nil opener scan.
	src.ScanELF, src.Opener = true, nil
	if got, err = LoadRebuild(context.Background(), src, fixtureTransaction(t)); err != nil || len(got) != 2 {
		t.Errorf("nil opener: %q, %v", show(got), err)
	}
}

func TestLoadRebuildCache(t *testing.T) {
	fsys := fixtures.FS()
	op := fixtures.Opener()
	src := RebuildSources{Runner: fixtures.Runner(), Opener: op, FS: fsys, CacheDir: "/cache", ScanELF: true}
	tx := fixtureTransaction(t)

	first, err := LoadRebuild(context.Background(), src, tx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fsys.ReadFile("/cache/" + ELFCacheFile); err != nil {
		t.Fatalf("the scan wasn't cached: %v", err)
	}
	opened := len(op.Opened())
	if opened == 0 {
		t.Fatal("nothing was scanned")
	}

	again, err := LoadRebuild(context.Background(), src, tx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(show(first), show(again)) {
		t.Errorf("cached run differs:\n %q\n %q", show(first), show(again))
	}
	if n := len(op.Opened()); n != opened {
		t.Errorf("the cached run opened %d more files", n-opened)
	}

	// r bypasses the cache.
	src.Fresh = true
	if _, err := LoadRebuild(context.Background(), src, tx); err != nil {
		t.Fatal(err)
	}
	if n := len(op.Opened()); n == opened {
		t.Error("a fresh run should scan again")
	}
}

func TestLoadRebuildNothingToDo(t *testing.T) {
	// No update drops a soname or moves an interpreter: no command runs.
	r := system.NewFakeRunner()
	tx := pacman.Transaction{Updates: []pacman.UpdateDetail{{
		Update:      pacman.Update{Name: "vim", OldVersion: "1-1", NewVersion: "2-1"},
		OldProvides: []string{"libgbm.so=1-64"}, NewProvides: []string{"libgbm.so=1-64"},
	}}}
	got, err := LoadRebuild(context.Background(), RebuildSources{Runner: r, ScanELF: true}, tx)
	if err != nil || got != nil || len(r.Calls()) != 0 {
		t.Errorf("got %v, %v, calls %v", got, err, r.Calls())
	}
}

func TestGatherFlagsTheCulprits(t *testing.T) {
	s := Gather(context.Background(), fixtureSources())
	reasons := map[string][]string{}
	for _, p := range s.Flagged {
		for _, f := range p.Flags {
			reasons[p.Name] = append(reasons[p.Name], f.Reason)
		}
	}
	for name, want := range map[string][]string{
		"zstd": {
			"Needs rebuild: gnome-shell-extension-legacy depends on libzstd.so.1 (zstd 1.5.7-2 → 1.5.7-3)",
			"Needs rebuild: visual-studio-code-bin links against libzstd.so.1 (zstd 1.5.7-2 → 1.5.7-3)",
		},
		"python": {"Python 3.13 → 3.14: rebuild these AUR packages: python-pywal"},
	} {
		for _, w := range want {
			if !slices.Contains(reasons[name], w) {
				t.Errorf("%s: no flag %q in %q", name, w, reasons[name])
			}
		}
	}
	if len(s.Rebuilds) != 3 {
		t.Errorf("Rebuilds = %d, want 3", len(s.Rebuilds))
	}
	if mesa := reasons["mesa"]; slices.ContainsFunc(mesa, func(r string) bool { return strings.Contains(r, "rebuild") }) {
		t.Errorf("mesa keeps its soname: %q", mesa)
	}

	// With the AUR switched off nothing foreign is looked at.
	src := fixtureSources()
	src.Config.AUR = false
	if s := Gather(context.Background(), src); len(s.Rebuilds) != 0 {
		t.Errorf("Rebuilds with aur = false: %d", len(s.Rebuilds))
	}
}

func TestLoadRebuildFailure(t *testing.T) {
	r := fixtures.Runner()
	r.Set("pacman -Qm", system.FakeResult{Code: 1, Stderr: []byte("boom")})
	src := RebuildSources{Runner: r, Opener: fixtures.Opener(), FS: fixtures.FS(), ScanELF: true}
	if _, err := LoadRebuild(context.Background(), src, fixtureTransaction(t)); err == nil {
		t.Error("a failing pacman -Qm should be an error")
	}
}
