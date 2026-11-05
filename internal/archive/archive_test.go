package archive

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/system"
)

var lastUpgrade = time.Date(2026, 10, 20, 13, 32, 11, 0, time.UTC)

func TestDays(t *testing.T) {
	// 23:30 on the 20th in UTC-5 is the 21st in UTC: the archive's days are UTC.
	late := time.Date(2026, 10, 20, 23, 30, 0, 0, time.FixedZone("x", -5*3600))
	for _, c := range []struct {
		in         time.Time
		first, snd string
	}{
		{lastUpgrade, "2026-10-20", "2026-10-21"},
		{late, "2026-10-21", "2026-10-22"},
		{time.Date(2026, 12, 31, 1, 0, 0, 0, time.UTC), "2026-12-31", "2027-01-01"},
	} {
		d := Days(c.in)
		if d[0].Format(DayFormat) != c.first || d[1].Format(DayFormat) != c.snd {
			t.Errorf("%v: got %v", c.in, d)
		}
	}
}

func TestServerURL(t *testing.T) {
	want := "https://archive.archlinux.org/repos/2026/09/08/$repo/os/$arch"
	if got := ServerURL(time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)); got != want {
		t.Fatalf("got %s", got)
	}
}

func TestGenerateConfDropsCustomReposAndIncludes(t *testing.T) {
	realConf := fixtures.Bytes(fixtures.PacmanConfArchive)
	conf, rep, err := GenerateConf(realConf, lastUpgrade)
	if err != nil {
		t.Fatal(err)
	}
	s := string(conf)
	for _, bad := range []string{"Include", "chaotic", "myrepo", "file://", "XferCommand", "aria2c", "DBPath", "[testing]", "[multilib]", "SigLevel = Required\n", "mirrorlist"} {
		if strings.Contains(s, bad) {
			t.Errorf("generated config has %q:\n%s", bad, s)
		}
	}
	for _, want := range []string{"[options]", "HoldPkg     = pacman glibc", "CacheDir    = /var/cache/pacman/pkg/", "LogFile     = /var/log/pacman.log", "IgnorePkg   = bash", "ParallelDownloads = 5", "SigLevel    = Required DatabaseOptional",
		"[core]\nServer = https://archive.archlinux.org/repos/2026/10/20/$repo/os/$arch\n", "[extra]\nServer = https://archive.archlinux.org/repos/2026/10/20/$repo/os/$arch\n"} {
		if !strings.Contains(s, want) {
			t.Errorf("generated config lacks %q:\n%s", want, s)
		}
	}
	if strings.Count(s, "Server =") != 2 {
		t.Errorf("want exactly two Server lines:\n%s", s)
	}
	if strings.Join(rep.Repos, ",") != "core,extra" || strings.Join(rep.Dropped, ",") != "chaotic-aur,myrepo" || rep.Includes != 4 ||
		strings.Join(rep.Options, ",") != "DBPath,XferCommand" {
		t.Errorf("report: %+v", rep)
	}
}

func TestGenerateConfMultilibAndErrors(t *testing.T) {
	conf, rep, err := GenerateConf([]byte("[options]\n[multilib]\nInclude = /x\n[core]\nServer = http://x\n"), lastUpgrade)
	if err != nil || strings.Join(rep.Repos, ",") != "core,multilib" || !strings.Contains(string(conf), "[multilib]\nServer = https://archive") {
		t.Fatalf("got %v, %+v\n%s", err, rep, conf)
	}
	if _, _, err := GenerateConf([]byte("[options]\n[myrepo]\nServer = file:///x\n"), lastUpgrade); err == nil {
		t.Fatal("no official repo should be an error")
	}
	if _, _, err := GenerateConf(nil, lastUpgrade); err == nil {
		t.Fatal("empty config should be an error")
	}
}

func TestChooseAndConfidence(t *testing.T) {
	d := func(n int) []pacman.Differing {
		out := make([]pacman.Differing, n)
		for i := range out {
			out[i] = pacman.Differing{Name: "p", OldVersion: "1", NewVersion: "2"}
		}
		return out
	}
	if _, ok := Choose(nil); ok {
		t.Fatal("empty")
	}
	for _, c := range []struct {
		a, b int
		want string
	}{{3, 1, "b"}, {1, 3, "a"}, {2, 2, "a"}, {0, 0, "a"}, {5, 0, "b"}} {
		got, _ := Choose([]Candidate{{Day: "a", Differ: d(c.a)}, {Day: "b", Differ: d(c.b)}})
		if got.Day != c.want {
			t.Errorf("%d vs %d: got %s, want %s", c.a, c.b, got.Day, c.want)
		}
	}
	for n, want := range map[int]string{
		0: "your system matches 2026-09-18 exactly",
		1: "your system doesn't exactly match 2026-09-18: 1 package differs",
		3: "your system doesn't exactly match 2026-09-18: 3 packages differ",
	} {
		if got := (Candidate{Day: "2026-09-18", Differ: d(n)}).Confidence(); got != want {
			t.Errorf("%d: %q", n, got)
		}
	}
}

func input() Input {
	return Input{
		Base:        fixtures.ArchiveBase,
		RealDB:      fixtures.RealDBPath,
		RealConf:    fixtures.Bytes(fixtures.PacmanConfArchive),
		LastUpgrade: lastUpgrade,
		Now:         fixtures.Now,
		Nonce:       "abc123",
		Names:       []string{"htop"},
	}
}

func TestPrepareChoosesTheBestDay(t *testing.T) {
	r, fsys := fixtures.Runner(), fixtures.FS()
	p, err := Prepare(context.Background(), r, fsys, input())
	if err != nil {
		t.Fatal(err)
	}
	// Day A: 3 packages differ, day B: 1.
	if p.Day != "2026-10-21" || len(p.Candidate.Differ) != 1 || len(p.Tried) != 2 {
		t.Fatalf("got %+v", p)
	}
	root := fixtures.ArchiveBase + "/20261023T120000Z-abc123"
	if p.Dir != root+"/2026-10-21" {
		t.Fatalf("dir %s", p.Dir)
	}
	// Every directory is new and 0700, the config 0600, local links to the real database.
	for _, d := range []string{root, root + "/2026-10-20", root + "/2026-10-20/sync", root + "/2026-10-21", root + "/2026-10-21/sync"} {
		if m, ok := fsys.DirMode(d); !ok || m != 0o700 {
			t.Errorf("%s: mode %v, made %v", d, m, ok)
		}
	}
	for _, d := range []string{root + "/2026-10-20", root + "/2026-10-21"} {
		if tgt, ok := fsys.LinkTarget(d + "/local"); !ok || tgt != "/var/lib/pacman/local" {
			t.Errorf("%s/local -> %q", d, tgt)
		}
		if m, ok := fsys.FileMode(d + "/pacman.conf"); !ok || m != 0o600 {
			t.Errorf("%s/pacman.conf mode %v", d, m)
		}
	}
	conf, _ := fsys.ReadFile(p.Dir + "/pacman.conf")
	if string(conf) != string(p.Conf) || !strings.Contains(string(conf), "/2026/10/21/") || strings.Contains(string(conf), "Include") {
		t.Errorf("conf:\n%s", conf)
	}
	// The dry run is -Sp against the chosen directory, and succeeded.
	if p.DryFailed || len(p.DryOut) == 0 || p.DryRun[1] != "-Sp" {
		t.Errorf("dry run: %+v", p)
	}
	// What ran: nothing but the allowed commands, never a -Sy outside fakeroot.
	for _, c := range r.Calls() {
		if strings.Contains(c, " -Sy ") && !strings.HasPrefix(c, "fakeroot -- pacman -Sy --dbpath "+fixtures.ArchiveBase+"/") {
			t.Errorf("bad call %s", c)
		}
		if strings.HasPrefix(c, "pacman -S ") || strings.HasPrefix(c, "sudo") {
			t.Errorf("bad call %s", c)
		}
	}
	// A second install gets a new directory; the same one can't be reused.
	in := input()
	in.Nonce = "def456"
	if _, err := Prepare(context.Background(), r, fsys, in); err != nil {
		t.Fatal(err)
	}
	if len(fsys.Dirs()) != 10 {
		t.Errorf("dirs: %v", fsys.Dirs())
	}
	in.Nonce = "abc123"
	if _, err := Prepare(context.Background(), r, fsys, in); !errors.Is(err, fs.ErrExist) {
		t.Errorf("reusing a directory: %v", err)
	}
}

func TestPrepareStopsBeforeDoingAnything(t *testing.T) {
	cases := map[string]func(*Input, *system.FakeRunner){
		"no fakeroot": func(_ *Input, r *system.FakeRunner) {
			r.Set("fakeroot --version", system.FakeResult{Err: system.ErrCommandNotFound})
		},
		"unknown last upgrade": func(in *Input, _ *system.FakeRunner) { in.LastUpgrade = time.Time{} },
		"bad name":             func(in *Input, _ *system.FakeRunner) { in.Names = []string{"-Syu"} },
		"no names":             func(in *Input, _ *system.FakeRunner) { in.Names = nil },
		"no official repo": func(in *Input, _ *system.FakeRunner) {
			in.RealConf = []byte("[options]\n[mine]\nServer = file:///x\n")
		},
		"no nonce": func(in *Input, _ *system.FakeRunner) { in.Nonce = "" },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			r, fsys := fixtures.Runner(), fixtures.FS()
			in := input()
			mod(&in, r)
			if _, err := Prepare(context.Background(), r, fsys, in); err == nil {
				t.Fatal("expected an error")
			}
			if len(fsys.Dirs()) != 0 {
				t.Errorf("created %v", fsys.Dirs())
			}
			for _, c := range r.Calls() {
				if c != "fakeroot --version" {
					t.Errorf("ran %s", c)
				}
			}
		})
	}
}

func TestPrepareNoFakerootIsNamed(t *testing.T) {
	r := fixtures.Runner()
	r.Set("fakeroot --version", system.FakeResult{Err: system.ErrCommandNotFound})
	_, err := Prepare(context.Background(), r, fixtures.FS(), input())
	if !errors.Is(err, ErrNoFakeroot) {
		t.Fatalf("got %v", err)
	}
	if _, err := Prepare(context.Background(), r, fixtures.FS(), func() Input { i := input(); i.LastUpgrade = time.Time{}; return i }()); !errors.Is(err, ErrNoLastUpgrade) {
		t.Fatalf("got %v", err)
	}
}

func TestPrepareOneDayFailsToSync(t *testing.T) {
	r, fsys := fixtures.Runner(), fixtures.FS()
	prev := r.Handler
	r.Handler = func(l string) (system.FakeResult, bool) {
		if strings.HasPrefix(l, "fakeroot -- pacman -Sy") && strings.Contains(l, "/2026-10-21 ") {
			return system.FakeResult{Stderr: []byte("error: failed retrieving file 'core.db' from archive.archlinux.org : The requested URL returned error: 404\n"), Code: 1}, true
		}
		return prev(l)
	}
	p, err := Prepare(context.Background(), r, fsys, input())
	if err != nil {
		t.Fatal(err)
	}
	if p.Day != "2026-10-20" || len(p.Candidate.Differ) != 3 || p.SyncErrors["2026-10-21"] == nil {
		t.Fatalf("got %+v", p)
	}
	if got := p.Candidate.Confidence(); got != "your system doesn't exactly match 2026-10-20: 3 packages differ" {
		t.Error(got)
	}
	// Both fail: an error, and no -Qu or -Sp was tried.
	r.Handler = func(l string) (system.FakeResult, bool) {
		if strings.HasPrefix(l, "fakeroot -- pacman -Sy") {
			return system.FakeResult{Stderr: []byte("error: 404\n"), Code: 1}, true
		}
		return prev(l)
	}
	in := input()
	in.Nonce = "zzz"
	if _, err := Prepare(context.Background(), r, fsys, in); err == nil {
		t.Fatal("expected an error")
	}
	for _, c := range r.Calls()[len(r.Calls())-3:] {
		if strings.HasPrefix(c, "pacman -Sp") {
			t.Errorf("dry run after failed syncs: %s", c)
		}
	}
}

func TestPrepareDryRunFailureIsReportedNotRun(t *testing.T) {
	in := input()
	in.Names = []string{"nosuchpkg"}
	p, err := Prepare(context.Background(), fixtures.Runner(), fixtures.FS(), in)
	if err != nil || !p.DryFailed || !strings.Contains(string(p.DryStderr), "target not found") {
		t.Fatalf("got %v, %+v", err, p)
	}
}

func TestPrepareNoDifferenceExitsOne(t *testing.T) {
	r := fixtures.Runner()
	prev := r.Handler
	r.Handler = func(l string) (system.FakeResult, bool) {
		if strings.HasPrefix(l, "pacman -Qu --dbpath "+fixtures.ArchiveBase+"/") {
			return system.FakeResult{Code: 1}, true
		}
		return prev(l)
	}
	p, err := Prepare(context.Background(), r, fixtures.FS(), input())
	if err != nil || len(p.Candidate.Differ) != 0 || p.Day != "2026-10-20" {
		t.Fatalf("got %v, %+v", err, p)
	}
}

func TestPrepareRefusesSymlinkedBase(t *testing.T) {
	fsys := fixtures.FS()
	_ = fsys.Symlink("/elsewhere", fixtures.ArchiveBase)
	if _, err := Prepare(context.Background(), fixtures.Runner(), fsys, input()); err == nil {
		t.Fatal("a symlinked base must be refused")
	}
}

func TestRecheck(t *testing.T) {
	r, fsys := fixtures.Runner(), fixtures.FS()
	p, err := Prepare(context.Background(), r, fsys, input())
	if err != nil {
		t.Fatal(err)
	}
	if err := Recheck(fsys, p); err != nil {
		t.Fatalf("clean: %v", err)
	}
	// The lock exists: refuse.
	_ = fsys.WriteFile("/var/lib/pacman/db.lck", nil, 0o644)
	if err := Recheck(fsys, p); err == nil || !strings.Contains(err.Error(), "db.lck") {
		t.Fatalf("lock: %v", err)
	}
	fsys2 := fixtures.FS()
	p2, _ := Prepare(context.Background(), fixtures.Runner(), fsys2, input())
	// The config changed under us: refuse.
	_ = fsys2.WriteFile(p2.Dir+"/pacman.conf", append(p2.Conf, "XferCommand = /bin/evil\n"...), 0o600)
	if err := Recheck(fsys2, p2); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("conf: %v", err)
	}
	// The config is gone: refuse.
	fsys3 := fixtures.FS()
	p3, _ := Prepare(context.Background(), fixtures.Runner(), fsys3, input())
	p3.Dir = fixtures.ArchiveBase + "/nowhere"
	if err := Recheck(fsys3, p3); err == nil {
		t.Fatal("missing dir")
	}
}

func TestPreparedInstallArgv(t *testing.T) {
	p, err := Prepare(context.Background(), fixtures.Runner(), fixtures.FS(), input())
	if err != nil {
		t.Fatal(err)
	}
	argv, err := p.InstallArgv([]string{"sudo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Guard().Verify(argv); err != nil {
		t.Fatalf("guard: %v", err)
	}
	if want := "sudo pacman -S --dbpath " + p.Dir + " --config " + p.Dir + "/pacman.conf -- htop"; strings.Join(argv, " ") != want {
		t.Errorf("got %v", argv)
	}
}
