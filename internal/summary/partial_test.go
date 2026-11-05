package summary

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/linkage"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/partial"
	"github.com/leNITHISH/cronen/internal/preflight"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/system"
)

// quietSources is a system with nothing pending and nothing flagged, so the
// exit code is decided by the partial-upgrade detector alone.
func quietSources(t *testing.T, qu system.FakeResult) Sources {
	t.Helper()
	src := fixtureSources()
	src.State.MarkRead(firmwareGUID, nvidiaGUID)
	r := fixturesRunner()
	r.Set("checkupdates", system.FakeResult{Code: 2})
	r.Set("pacman -Qu", qu)
	src.Runner = r
	src.Config.AUR = false
	src.Config.Preflight = false
	return src
}

func fixturesRunner() *system.FakeRunner { return fixtureSources().Runner.(*system.FakeRunner) }

func TestPartialExitCodes(t *testing.T) {
	synced := system.FakeResult{Stdout: []byte("zlib 1.3-1 -> 1.3.1-1\nbash 5.3.3-1 -> 5.3.3-2 [ignored]\n")}
	tests := []struct {
		name string
		qu   system.FakeResult
		tune func(*Sources)
		want int
	}{
		{"nothing pending", system.FakeResult{Code: 1}, nil, ExitNothing},
		{"only an ignored line", system.FakeResult{Stdout: []byte("bash 5.3.3-1 -> 5.3.3-2 [ignored]\n")}, nil, ExitNothing},
		{"database synced without upgrading is attention (20)", synced, nil, ExitAttention},
		{"switched off: no check, nothing to say", synced, func(s *Sources) { s.Config.PartialCheck = false }, ExitNothing},
		{"pacman -Qu fails: error (1)", system.FakeResult{Code: 1, Stderr: []byte("error: boom\n")}, nil, ExitError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := quietSources(t, tt.qu)
			if tt.tune != nil {
				tt.tune(&src)
			}
			s := Gather(context.Background(), src)
			if got := s.ExitCode(); got != tt.want {
				t.Errorf("exit code = %d, want %d (errors %v, preflight %v)", got, tt.want, s.Errors, s.Preflight)
			}
			if tt.want == ExitAttention {
				if len(s.Preflight) != 1 || s.Preflight[0].Severity != risk.Warn || s.Preflight[0].Check != preflight.CheckPartial {
					t.Fatalf("preflight = %+v", s.Preflight)
				}
				if !strings.HasPrefix(s.Preflight[0].Message, "Your package database was synced without upgrading. Installing anything now is a partial upgrade. Run the full upgrade first.") {
					t.Errorf("message = %q", s.Preflight[0].Message)
				}
				if !strings.Contains(s.Text(), "preflight: Your package database was synced") {
					t.Errorf("--check text lacks the warning:\n%s", s.Text())
				}
			}
		})
	}
}

func TestPartialMakesNoCallsWhenOff(t *testing.T) {
	src := quietSources(t, system.FakeResult{Code: 1})
	src.Config.PartialCheck = false
	r := src.Runner.(*system.FakeRunner)
	Gather(context.Background(), src)
	for _, c := range r.Calls() {
		if c == "pacman -Qu" || c == "pacman -Q" {
			t.Errorf("ran %q with partial_check = false", c)
		}
	}
}

func TestPartialIsCriticalWithDamage(t *testing.T) {
	src := quietSources(t, system.FakeResult{Stdout: []byte("zlib 1.3-1 -> 1.3.1-1\n")})
	src.CacheDir = "/cache"
	// A saved verdict for exactly the installed packages: one broken file.
	installed, err := pacman.AllPackages(context.Background(), src.Runner)
	if err != nil {
		t.Fatal(err)
	}
	c := &linkage.Cache{}
	c.Save(toLinkagePackages(installed), linkage.Result{Broken: []linkage.Broken{{Path: "/usr/bin/paru", Package: "paru", Version: "2.1.0-1", Lib: "libalpm.so.15"}}}, time.Now())
	fsys := src.FS.(*system.FakeFS)
	if err := fsys.WriteFile("/cache/"+linkage.CacheFile, c.Marshal(), 0o644); err != nil {
		t.Fatal(err)
	}

	s := Gather(context.Background(), src)
	if got := s.ExitCode(); got != ExitIntervention {
		t.Fatalf("exit code = %d, want 30 (critical)", got)
	}
	if p := s.Preflight[0]; p.Severity != risk.Critical || !strings.Contains(p.Message, "1 broken file") {
		t.Errorf("problem = %+v", p)
	}
	var j map[string]any
	if err := json.Unmarshal(s.JSON(), &j); err != nil {
		t.Fatal(err)
	}
	if j["partial_upgrade"] != true || j["broken_linkage"] != 1.0 {
		t.Errorf("json partial_upgrade %v, broken_linkage %v", j["partial_upgrade"], j["broken_linkage"])
	}

	// A package changed since the scan: the verdict no longer applies.
	pkgs := pacman.Package{Name: "zzz-new", Version: "1-1"}
	r := src.Runner.(*system.FakeRunner)
	r.Set("pacman -Q", system.FakeResult{Stdout: append(r.MustStdout("pacman -Q"), []byte(pkgs.Name+" "+pkgs.Version+"\n")...)})
	s = Gather(context.Background(), src)
	if got := s.ExitCode(); got != ExitAttention {
		t.Errorf("with a stale verdict: exit code = %d, want 20", got)
	}
	j = map[string]any{}
	if err := json.Unmarshal(s.JSON(), &j); err != nil {
		t.Fatal(err)
	}
	if _, ok := j["broken_linkage"]; ok {
		t.Error("broken_linkage reported from a stale scan")
	}
}

func TestPartialJSONOmitsBrokenLinkageWithoutAScan(t *testing.T) {
	s := Gather(context.Background(), quietSources(t, system.FakeResult{Code: 1}))
	var j map[string]any
	if err := json.Unmarshal(s.JSON(), &j); err != nil {
		t.Fatal(err)
	}
	if j["partial_upgrade"] != false {
		t.Errorf("partial_upgrade = %v", j["partial_upgrade"])
	}
	if _, ok := j["broken_linkage"]; ok {
		t.Error("broken_linkage present without a cached scan")
	}
}

func TestPartialLine(t *testing.T) {
	loc := time.UTC
	at := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 12, 0, 0, 0, loc) }
	if got := PartialLine(nil, loc); got != "" {
		t.Errorf("PartialLine(nil) = %q", got)
	}
	one := []partial.Transaction{{When: at(10, 3), Command: "pacman -Sy discord"}}
	if got, want := PartialLine(one, loc), "1 partial upgrade since your last full upgrade (Oct 3: pacman -Sy discord)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	two := append([]partial.Transaction{{When: at(9, 28), Command: "yay -Sy vlc"}}, one...)
	if got, want := PartialLine(two, loc), "2 partial upgrades since your last full upgrade (Oct 3: pacman -Sy discord)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPartialHistoryFromLog(t *testing.T) {
	log := `[2026-10-01T10:00:00+0000] [PACMAN] Running 'pacman -Syu'
[2026-10-01T10:00:01+0000] [PACMAN] starting full system upgrade
[2026-10-01T10:00:02+0000] [ALPM] transaction started
[2026-10-01T10:00:03+0000] [ALPM] upgraded zlib (1.3-1 -> 1.3.1-1)
[2026-10-01T10:00:04+0000] [ALPM] transaction completed
[2026-10-03T09:00:00+0000] [PACMAN] Running 'pacman -Sy discord'
[2026-10-03T09:00:02+0000] [ALPM] transaction started
[2026-10-03T09:00:03+0000] [ALPM] installed discord (0.0.1-1)
[2026-10-03T09:00:04+0000] [ALPM] transaction completed
`
	src := quietSources(t, system.FakeResult{Code: 1})
	fsys := src.FS.(*system.FakeFS)
	if err := fsys.WriteFile(pacman.LogPath, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Gather(context.Background(), src)
	if len(s.PartialUpgrades) != 1 || s.PartialUpgrades[0].Command != "pacman -Sy discord" {
		t.Fatalf("PartialUpgrades = %+v", s.PartialUpgrades)
	}
	if !slices.Contains(strings.Split(s.Text(), "\n"), "ℹ 1 partial upgrade since your last full upgrade (Oct 3: pacman -Sy discord)") {
		t.Errorf("text lacks the partial-upgrade line:\n%s", s.Text())
	}
	// A full upgrade after it clears the line.
	log += `[2026-10-05T09:00:00+0000] [PACMAN] Running 'pacman -Syu'
[2026-10-05T09:00:01+0000] [PACMAN] starting full system upgrade
`
	_ = fsys.WriteFile(pacman.LogPath, []byte(log), 0o644)
	s = Gather(context.Background(), src)
	if len(s.PartialUpgrades) != 0 {
		t.Errorf("PartialUpgrades after a full upgrade = %+v", s.PartialUpgrades)
	}
}

func TestScanLinkageDriver(t *testing.T) {
	src := fixtureSources()
	ls := LinkageSources{
		Runner: src.Runner, Opener: src.Opener, FS: src.FS, CacheDir: "/cache",
		Now: func() time.Time { return time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC) },
	}
	var last linkage.Progress
	ls.OnProgress = func(p linkage.Progress) { last = p }
	res, err := ScanLinkage(context.Background(), ls)
	if err != nil {
		t.Fatal(err)
	}
	// The demo system: paru links libalpm.so.15, which nothing provides.
	if len(res.Broken) != 1 || res.Broken[0].Package != "paru" || res.Broken[0].Lib != "libalpm.so.15" {
		t.Fatalf("Broken = %+v", res.Broken)
	}
	if last.Total == 0 || last.Done != last.Total {
		t.Errorf("progress ended at %+v", last)
	}
	if got := LinkageText(res); !strings.Contains(got, "/usr/bin/paru (paru 2.1.0-1) needs libalpm.so.15, which no installed package provides") {
		t.Errorf("text:\n%s", got)
	}

	// The verdict is saved for the installed packages, and a repeat scan
	// asks pacman for no file lists.
	if _, ok := LoadLinkageVerdict(context.Background(), ls.Runner, ls.FS, "/cache"); !ok {
		t.Error("no saved verdict")
	}
	r := ls.Runner.(*system.FakeRunner)
	before := len(r.Calls())
	if _, err := ScanLinkage(context.Background(), ls); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Calls()[before:] {
		if strings.HasPrefix(c, "pacman -Ql") {
			t.Errorf("repeat scan ran %q", c)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ScanLinkage(ctx, ls); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled scan: %v", err)
	}
	ls.Opener = nil
	if _, err := ScanLinkage(context.Background(), ls); err == nil {
		t.Error("scan without an opener succeeded")
	}
	if got := LinkageText(linkage.Result{Packages: 2, Files: 5}); !strings.Contains(got, "no broken linkage found in 5 ELF files of 2 packages") {
		t.Errorf("clean text = %q", got)
	}
}

func TestLinkageFixes(t *testing.T) {
	src := fixtureSources()
	cacheFile := "/var/cache/pacman/pkg/firefox-144.0-1-x86_64.pkg.tar.zst"
	fs := system.NewFakeFS(map[string][]byte{
		cacheFile: {1},
		// An older build, which must not be picked.
		"/var/cache/pacman/pkg/firefox-143.0-1-x86_64.pkg.tar.zst": {1},
	})
	res := linkage.Result{Broken: []linkage.Broken{
		{Path: "/usr/lib/firefox/firefox", Package: "firefox", Version: "144.0-1", Lib: "libgone.so.1"},
		{Path: "/usr/bin/paru", Package: "paru", Version: "2.1.0-1", Lib: "libalpm.so.15"},
	}}
	fixSrc := FixSources{Runner: src.Runner, FS: fs, Priv: []string{"sudo"}, AURHelper: "paru"}
	ctx := context.Background()

	fixes, err := LinkageFixes(ctx, fixSrc, res, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []linkage.FixKind
	for _, f := range fixes {
		kinds = append(kinds, f.Kind)
	}
	if want := []linkage.FixKind{linkage.FixUpgrade, linkage.FixReinstall, linkage.FixRebuild}; !slices.Equal(kinds, want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	if want := []string{"sudo", "pacman", "-U", "--", cacheFile}; !slices.Equal(fixes[1].Argv, want) {
		t.Errorf("reinstall argv = %v, want %v", fixes[1].Argv, want)
	}
	if want := []string{"paru", "-S", "--", "paru"}; !slices.Equal(fixes[2].Argv, want) {
		t.Errorf("rebuild argv = %v, want %v", fixes[2].Argv, want)
	}

	// Pending repo updates, or unknown ones: no rebuild command at all.
	for _, tc := range []struct {
		pending int
		known   bool
	}{{3, true}, {0, false}} {
		fixes, err = LinkageFixes(ctx, fixSrc, res, tc.pending, tc.known)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range fixes {
			if f.Kind == linkage.FixRebuild || (f.Kind == linkage.FixUpgradeFirst && f.Argv != nil) {
				t.Errorf("pending %d known %v: offered a rebuild: %+v", tc.pending, tc.known, f)
			}
		}
	}

	if fixes, err := LinkageFixes(ctx, fixSrc, linkage.Result{}, 0, true); err != nil || fixes != nil {
		t.Errorf("nothing broken: %v, %v", fixes, err)
	}
}
