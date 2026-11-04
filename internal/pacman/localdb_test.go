package pacman_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/system"
)

func TestLoadLocalDBFixture(t *testing.T) {
	db, err := pacman.LoadLocalDB(context.Background(), fixtures.Runner(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(db.Packages) < 40 {
		t.Fatalf("got %d packages, want the whole fixture", len(db.Packages))
	}
	for i := 1; i < len(db.Packages); i++ {
		if db.Packages[i-1].Name >= db.Packages[i].Name {
			t.Fatalf("not sorted: %s before %s", db.Packages[i-1].Name, db.Packages[i].Name)
		}
	}
	g, ok := db.Lookup("glibc")
	if !ok {
		t.Fatal("glibc missing")
	}
	if g.Explicit || g.Version != "2.42+r17+g6f0cf3d0b5b0-1" || g.InstalledSize != 49283072 {
		t.Errorf("glibc = %+v", g)
	}
	if got := strings.Join(g.DependsOn, ","); got != "linux-api-headers,tzdata,filesystem" {
		t.Errorf("glibc depends on %q", got)
	}
	if len(g.RequiredBy) == 0 || len(g.BackupFiles) != 1 || g.BackupFiles[0] != "/etc/gai.conf" {
		t.Errorf("glibc required by %v, backup %v", g.RequiredBy, g.BackupFiles)
	}
	if g.InstallDate.IsZero() {
		t.Error("install date not parsed")
	}
	if !db.Foreign["paru"] || db.Foreign["glibc"] {
		t.Errorf("foreign = %v", db.Foreign)
	}
	if !db.Orphans["old-codec-lib"] || db.Orphans["glibc"] {
		t.Errorf("orphans = %v", db.Orphans)
	}
	p, _ := db.Lookup("paru")
	if got := strings.Join(p.DependsOn, ","); got != "pacman,libalpm.so" {
		t.Errorf("constraint not cut: %q", got)
	}
}

func TestParseLocalDBFields(t *testing.T) {
	in := `Name            : bat
Version         : 0.25.0-1
Description     : Cat clone
Groups          : None
Provides        : libfoo.so=1-64
Depends On      : glibc>=2.40  libgit2
Optional Deps   : less: pager
                  git: diffs
Required By     : None
Optional For    : paru
Installed Size  : 1.50 MiB
Install Date    : Sat 05 Jul 2025 09:02:00 AM UTC
Install Reason  : Explicitly installed
Backup Files    : [modified] /etc/bat.conf
                  [unmodified] /etc/bat2.conf
`
	pkgs, err := pacman.ParseLocalDB([]byte(in), time.UTC)
	if err != nil || len(pkgs) != 1 {
		t.Fatalf("pkgs=%v err=%v", pkgs, err)
	}
	p := pkgs[0]
	if !p.Explicit || p.InstalledSize != 1572864 || len(p.RequiredBy) != 0 {
		t.Errorf("%+v", p)
	}
	if got := strings.Join(p.DependsOn, ","); got != "glibc,libgit2" {
		t.Errorf("depends %q", got)
	}
	if got := strings.Join(p.Provides, ","); got != "libfoo.so=1-64" {
		t.Errorf("provides %q", got)
	}
	if got := strings.Join(p.BackupFiles, ","); got != "/etc/bat.conf,/etc/bat2.conf" {
		t.Errorf("backup %q", got)
	}
	if want := time.Date(2025, 7, 5, 9, 2, 0, 0, time.UTC); !p.InstallDate.Equal(want) {
		t.Errorf("date %v", p.InstallDate)
	}
}

func TestParseLocalDBErrors(t *testing.T) {
	for name, in := range map[string]string{
		"no name":  "Version : 1\n\n",
		"bad size": "Name : a\nInstalled Size : lots\n",
		"no colon": "garbage\n",
	} {
		if _, err := pacman.ParseLocalDB([]byte(in), time.UTC); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	// An unreadable date only loses the date.
	pkgs, err := pacman.ParseLocalDB([]byte("Name : a\nInstall Date : someday\n"), time.UTC)
	if err != nil || !pkgs[0].InstallDate.IsZero() {
		t.Errorf("pkgs=%v err=%v", pkgs, err)
	}
}

func TestOrphansExitOne(t *testing.T) {
	r := system.NewFakeRunner()
	r.Set("pacman -Qdtq", system.FakeResult{Code: 1})
	names, err := pacman.Orphans(context.Background(), r)
	if err != nil || len(names) != 0 {
		t.Errorf("names=%v err=%v", names, err)
	}
	r.Set("pacman -Qdtq", system.FakeResult{Code: 1, Stderr: []byte("error: boom\n")})
	if _, err := pacman.Orphans(context.Background(), r); err == nil {
		t.Error("exit 1 with stderr should be an error")
	}
	r.Set("pacman -Qdtq", system.FakeResult{Code: 2})
	if _, err := pacman.Orphans(context.Background(), r); err == nil {
		t.Error("exit 2 should be an error")
	}
}

func TestLoadLocalDBErrors(t *testing.T) {
	r := fixtures.Runner()
	r.Set("pacman -Qi", system.FakeResult{Err: system.ErrCommandNotFound})
	if _, err := pacman.LoadLocalDB(context.Background(), r, time.UTC); err == nil {
		t.Error("want an error when pacman -Qi fails")
	}
}

func pkg(name string, explicit bool, requiredBy ...string) pacman.LocalPackage {
	return pacman.LocalPackage{Name: name, Explicit: explicit, RequiredBy: requiredBy}
}

func TestWhyInstalled(t *testing.T) {
	tests := []struct {
		name   string
		pkgs   []pacman.LocalPackage
		target string
		want   string
		ok     bool
	}{
		{"explicit itself", []pacman.LocalPackage{pkg("a", true, "b")}, "a", "a", true},
		{"one hop", []pacman.LocalPackage{pkg("lib", false, "app"), pkg("app", true)}, "lib", "lib←app", true},
		{"chain", []pacman.LocalPackage{
			pkg("libxyz", false, "gtk4"), pkg("gtk4", false, "nautilus"), pkg("nautilus", true),
		}, "libxyz", "libxyz←gtk4←nautilus", true},
		{"shortest wins", []pacman.LocalPackage{
			pkg("lib", false, "mid", "top"), pkg("mid", false, "far"), pkg("far", true), pkg("top", true),
		}, "lib", "lib←top", true},
		{"tie broken by name", []pacman.LocalPackage{
			pkg("lib", false, "zeta", "alpha"), pkg("alpha", true), pkg("zeta", true),
		}, "lib", "lib←alpha", true},
		{"cycle with an explicit exit", []pacman.LocalPackage{
			pkg("a", false, "b"), pkg("b", false, "a", "app"), pkg("app", true),
		}, "a", "a←b←app", true},
		{"cycle and no explicit root", []pacman.LocalPackage{
			pkg("a", false, "b"), pkg("b", false, "a"),
		}, "a", "", false},
		{"nothing requires it", []pacman.LocalPackage{pkg("lonely", false)}, "lonely", "", false},
		{"unknown required-by is skipped", []pacman.LocalPackage{pkg("a", false, "ghost")}, "a", "", false},
		{"unknown package", nil, "nope", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := pacman.NewLocalDB(tt.pkgs, nil, nil)
			chain, ok := db.WhyInstalled(tt.target)
			if ok != tt.ok || strings.Join(chain, "←") != tt.want {
				t.Errorf("got %v %v, want %q %v", chain, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestParseLocalDBLarge(t *testing.T) {
	pkgs, err := pacman.ParseLocalDB(fixtures.SyntheticQi(1500), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	db := pacman.NewLocalDB(pkgs, nil, nil)
	if len(db.Packages) != 1500 {
		t.Fatalf("got %d packages", len(db.Packages))
	}
	chain, ok := db.WhyInstalled("pkg1499")
	if !ok || chain[0] != "pkg1499" {
		t.Fatalf("chain = %v %v", chain, ok)
	}
	root, _ := db.Lookup(chain[len(chain)-1])
	if !root.Explicit {
		t.Errorf("chain %v doesn't end at an explicit package", chain)
	}
}
