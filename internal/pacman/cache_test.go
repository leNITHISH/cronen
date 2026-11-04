package pacman

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/system"
)

func TestParseCacheName(t *testing.T) {
	tests := []struct {
		file                string
		name, version, arch string
		ok                  bool
	}{
		{"linux-6.17.4.arch1-1-x86_64.pkg.tar.zst", "linux", "6.17.4.arch1-1", "x86_64", true},
		{"python-pywal-3.3.0-9-any.pkg.tar.zst", "python-pywal", "3.3.0-9", "any", true},
		{"mesa-1:25.2.3-1-x86_64.pkg.tar.xz", "mesa", "1:25.2.3-1", "x86_64", true},
		{"libsigc++-3.6.0-1-x86_64.pkg.tar.zst", "libsigc++", "3.6.0-1", "x86_64", true},
		{"linux-headers-6.17.4.arch1-1.1-x86_64_v3.pkg.tar.zst", "linux-headers", "6.17.4.arch1-1.1", "x86_64_v3", true},
		{"emacs-nox-30.2-1-x86_64.pkg.tar", "emacs-nox", "30.2-1", "x86_64", true},
		{"linux-6.17.4.arch1-1-x86_64.pkg.tar.zst.sig", "", "", "", false},
		{"linux-6.17.4.arch1-1-x86_64.pkg.tar.zst.part", "", "", "", false},
		{"download-linux-6.17.4.arch1-1-x86_64.pkg.tar.zst", "download-linux", "6.17.4.arch1-1", "x86_64", true},
		{"foo-1-x86_64.pkg.tar.zst", "", "", "", false},
		{"-foo-1.0-1-x86_64.pkg.tar.zst", "", "", "", false},
		{"Foo-1.0-1-x86_64.pkg.tar.zst", "", "", "", false},
		{"foo-1.0-x-x86_64.pkg.tar.zst", "", "", "", false},
		{"foo-1 0-1-x86_64.pkg.tar.zst", "", "", "", false},
		{"README", "", "", "", false},
	}
	for _, tt := range tests {
		name, version, arch, ok := ParseCacheName(tt.file)
		if ok != tt.ok || name != tt.name || version != tt.version || arch != tt.arch {
			t.Errorf("ParseCacheName(%q) = %q, %q, %q, %v; want %q, %q, %q, %v",
				tt.file, name, version, arch, ok, tt.name, tt.version, tt.arch, tt.ok)
		}
	}
}

func TestSameVersion(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{"1.0-1", "1.0-1", true},
		{"1:1.0-1", "1.0-1", true},
		{"1.0-1", "1:1.0-1", true},
		{"1:1.0-1", "2:1.0-1", false},
		{"1.0-1", "1.0-2", false},
		{"1.0-1", "1:1.0-2", false},
	} {
		if got := SameVersion(tt.a, tt.b); got != tt.want {
			t.Errorf("SameVersion(%q, %q) = %v", tt.a, tt.b, got)
		}
	}
}

func cacheFS() *system.FakeFS {
	return system.NewFakeFS(map[string][]byte{
		"/var/cache/pacman/pkg/linux-6.17.4.arch1-1-x86_64.pkg.tar.zst":      []byte("x"),
		"/var/cache/pacman/pkg/linux-6.17.4.arch1-1-x86_64.pkg.tar.zst.sig":  []byte("s"),
		"/var/cache/pacman/pkg/linux-6.17.3.arch1-1-x86_64.pkg.tar.zst":      []byte("x"),
		"/var/cache/pacman/pkg/linux-6.17.2.arch1-1-x86_64.pkg.tar.zst":      []byte("x"),
		"/var/cache/pacman/pkg/download-linux.part":                          []byte("x"),
		"/var/cache/pacman/pkg/linux-6.17.1.arch1-1-x86_64.pkg.tar.zst.part": []byte("x"),
		"/home/u/.cache/paru/clone/foo/foo-1.0-1-x86_64.pkg.tar.zst":         []byte("x"),
		"/etc/pacman.conf": []byte("[options]\nCacheDir = /var/cache/pacman/pkg/\n"),
	})
}

func TestScanCache(t *testing.T) {
	fsys := cacheFS()
	dirs := ResolveCacheDirs(fsys, []string{"~/.cache/paru/clone/*", "/nowhere/*"}, "/home/u")
	want := []string{"/var/cache/pacman/pkg", "/home/u/.cache/paru/clone/foo"}
	if strings.Join(dirs, " ") != strings.Join(want, " ") {
		t.Fatalf("dirs = %q, want %q", dirs, want)
	}
	pkgs, err := ScanCache(fsys, append(dirs, "/missing"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range pkgs {
		got = append(got, p.Name+"@"+p.Version)
		if p.Name == "linux" && p.Version == "6.17.4.arch1-1" != p.HasSig {
			t.Errorf("%s: HasSig = %v", p.File, p.HasSig)
		}
	}
	wantPkgs := "foo@1.0-1 linux@6.17.2.arch1-1 linux@6.17.3.arch1-1 linux@6.17.4.arch1-1"
	if strings.Join(got, " ") != wantPkgs {
		t.Errorf("packages = %q, want %q", got, wantPkgs)
	}
}

func TestCheckCachePath(t *testing.T) {
	dirs := []string{"/var/cache/pacman/pkg/", "/home/u/.cache/paru/clone/foo"}
	const good = "/var/cache/pacman/pkg/linux-6.17.3.arch1-1-x86_64.pkg.tar.zst"
	for _, p := range []string{good, "/home/u/.cache/paru/clone/foo/foo-1.0-1-x86_64.pkg.tar.zst"} {
		if err := CheckCachePath(p, dirs); err != nil {
			t.Errorf("%s refused: %v", p, err)
		}
	}
	for name, p := range map[string]string{
		"empty":             "",
		"relative":          "linux-6.17.3.arch1-1-x86_64.pkg.tar.zst",
		"outside the cache": "/tmp/linux-6.17.3.arch1-1-x86_64.pkg.tar.zst",
		"dotdot":            "/var/cache/pacman/pkg/../../../tmp/linux-6.17.3.arch1-1-x86_64.pkg.tar.zst",
		"unclean":           "/var/cache/pacman/pkg//linux-6.17.3.arch1-1-x86_64.pkg.tar.zst",
		"subdirectory":      "/var/cache/pacman/pkg/sub/linux-6.17.3.arch1-1-x86_64.pkg.tar.zst",
		"parent dir":        "/var/cache/pacman/linux-6.17.3.arch1-1-x86_64.pkg.tar.zst",
		"prefix of a dir":   "/var/cache/pacman/pkg-evil/linux-6.17.3.arch1-1-x86_64.pkg.tar.zst",
		"a signature":       good + ".sig",
		"not a package":     "/var/cache/pacman/pkg/passwd",
		"a newline":         "/var/cache/pacman/pkg/linux-6.17.3.arch1-1-x86_64.pkg.tar.zst\n",
		"a NUL":             "/var/cache/pacman/pkg/linux\x00-6.17.3.arch1-1-x86_64.pkg.tar.zst",
		"an option":         "--root=/tmp",
		"a URL":             "https://example.org/linux-6.17.3.arch1-1-x86_64.pkg.tar.zst",
	} {
		if err := CheckCachePath(p, dirs); err == nil {
			t.Errorf("%s: %q accepted", name, p)
		}
	}
	if err := CheckCachePath(good, nil); err == nil {
		t.Error("accepted with no cache directories")
	}
}

func TestCheckCacheFile(t *testing.T) {
	fsys := cacheFS()
	dirs := []string{"/var/cache/pacman/pkg"}
	const f = "/var/cache/pacman/pkg/linux-6.17.3.arch1-1-x86_64.pkg.tar.zst"
	if err := CheckCacheFile(fsys, f, dirs); err != nil {
		t.Errorf("regular file refused: %v", err)
	}
	if err := CheckCacheFile(fsys, "/var/cache/pacman/pkg/linux-9.9-1-x86_64.pkg.tar.zst", dirs); err == nil {
		t.Error("a missing file was accepted")
	}
	fsys.SetSymlink(f)
	if err := CheckCacheFile(fsys, f, dirs); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Errorf("a symlink was accepted: %v", err)
	}
}

func vercmpRunner(t *testing.T) *system.FakeRunner {
	t.Helper()
	r := system.NewFakeRunner()
	r.Handler = func(line string) (system.FakeResult, bool) {
		f := strings.Fields(line)
		if len(f) != 3 || f[0] != "vercmp" {
			return system.FakeResult{}, false
		}
		// A stand-in for vercmp good enough for dotted numbers.
		cmp := func(a, b string) int {
			as, bs := strings.FieldsFunc(a, func(r rune) bool { return r == '.' || r == '-' }), strings.FieldsFunc(b, func(r rune) bool { return r == '.' || r == '-' })
			for i := 0; i < len(as) && i < len(bs); i++ {
				if as[i] != bs[i] {
					if len(as[i]) != len(bs[i]) {
						return len(as[i]) - len(bs[i])
					}
					return strings.Compare(as[i], bs[i])
				}
			}
			return len(as) - len(bs)
		}
		n := cmp(f[1], f[2])
		out := "0\n"
		switch {
		case n < 0:
			out = "-1\n"
		case n > 0:
			out = "1\n"
		}
		return system.FakeResult{Stdout: []byte(out)}, true
	}
	return r
}

func TestOlderCached(t *testing.T) {
	pkgs, _ := ScanCache(cacheFS(), []string{"/var/cache/pacman/pkg"})
	r := vercmpRunner(t)
	got, err := OlderCached(context.Background(), r, pkgs, "linux", "6.17.4.arch1-1")
	if err != nil {
		t.Fatal(err)
	}
	var vs []string
	for _, p := range got {
		vs = append(vs, p.Version)
	}
	if strings.Join(vs, " ") != "6.17.3.arch1-1 6.17.2.arch1-1" {
		t.Errorf("older = %q: the installed version must not be listed, newest first", vs)
	}
	// A file name that leaves out the epoch is still the installed version.
	got, err = OlderCached(context.Background(), r, pkgs, "linux", "1:6.17.4.arch1-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got {
		if p.Version == "6.17.4.arch1-1" {
			t.Errorf("the installed version (epoch left out of the file name) is listed as older")
		}
	}
	if got, _ := OlderCached(context.Background(), r, pkgs, "nothing", "1-1"); len(got) != 0 {
		t.Errorf("older of an unknown package = %v", got)
	}
}

func TestReadBuildDates(t *testing.T) {
	pkgs, _ := ScanCache(cacheFS(), []string{"/var/cache/pacman/pkg"})
	var linux []CachedPackage
	for _, p := range pkgs {
		if p.Version == "6.17.3.arch1-1" || p.Version == "6.17.2.arch1-1" {
			linux = append(linux, p)
		}
	}
	r := system.NewFakeRunner()
	r.Set("pacman -Qip -- /var/cache/pacman/pkg/linux-6.17.2.arch1-1-x86_64.pkg.tar.zst /var/cache/pacman/pkg/linux-6.17.3.arch1-1-x86_64.pkg.tar.zst",
		system.FakeResult{Stdout: []byte(`Name            : linux
Version         : 6.17.2.arch1-1
Build Date      : Sat Oct 10 19:18:51 2026
Installed Size  : 140.00 MiB

Name            : linux
Version         : 6.17.3.arch1-1
Build Date      : Sat Oct 17 08:00:00 2026
Installed Size  : 140.00 MiB
`)})
	got, err := ReadBuildDates(context.Background(), r, []string{"/var/cache/pacman/pkg"}, linux, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].BuildDate.Equal(time.Date(2026, 10, 10, 19, 18, 51, 0, time.UTC)) || got[1].BuildDate.Day() != 17 {
		t.Errorf("dates = %v, %v", got[0].BuildDate, got[1].BuildDate)
	}
	// A path outside the cache is refused before any command runs.
	bad := []CachedPackage{{Name: "x", Version: "1-1", Path: "/tmp/x-1-1-any.pkg.tar.zst"}}
	n := len(r.Calls())
	if _, err := ReadBuildDates(context.Background(), r, []string{"/var/cache/pacman/pkg"}, bad, time.UTC); err == nil || len(r.Calls()) != n {
		t.Errorf("outside path: err = %v, calls = %v", err, r.Calls())
	}
	// A failure leaves the dates zero and says why.
	r2 := system.NewFakeRunner()
	got, err = ReadBuildDates(context.Background(), r2, []string{"/var/cache/pacman/pkg"}, linux, time.UTC)
	if err == nil || !got[0].BuildDate.IsZero() {
		t.Errorf("err = %v, date = %v", err, got[0].BuildDate)
	}
}
