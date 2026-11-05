package linkage

import (
	"context"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/system"
)

func TestParseLdConf(t *testing.T) {
	dirs, inc := ParseLdConf([]byte(`# comment
include /etc/ld.so.conf.d/*.conf
/usr/lib/foo   # trailing
/opt/a:/opt/b
hwcap 1 nosegneg

relative/dir
`))
	if want := []string{"/usr/lib/foo", "/opt/a", "/opt/b"}; !slices.Equal(dirs, want) {
		t.Errorf("dirs = %v, want %v", dirs, want)
	}
	if want := []string{"/etc/ld.so.conf.d/*.conf"}; !slices.Equal(inc, want) {
		t.Errorf("includes = %v, want %v", inc, want)
	}
}

func TestLoadLdConf(t *testing.T) {
	fsys := system.NewFakeFS(map[string][]byte{
		"/etc/ld.so.conf":            []byte("include /etc/ld.so.conf.d/*.conf\n/usr/lib/first\n"),
		"/etc/ld.so.conf.d/a.conf":   []byte("/opt/a/lib\n"),
		"/etc/ld.so.conf.d/b.conf":   []byte("/opt/b/lib\ninclude /etc/loop.conf\n"),
		"/etc/loop.conf":             []byte("include /etc/ld.so.conf\n/opt/loop\n"),
		"/etc/ld.so.conf.d/notes.md": []byte("/not/included\n"),
	})
	got := LoadLdConf(fsys)
	want := []string{"/usr/lib/first", "/opt/a/lib", "/opt/b/lib", "/opt/loop"}
	if !slices.Equal(got, want) {
		t.Errorf("LoadLdConf = %v, want %v", got, want)
	}
	if got := LoadLdConf(system.NewFakeFS(nil)); len(got) != 0 {
		t.Errorf("without ld.so.conf = %v, want none", got)
	}
}

func exists(paths ...string) func(string) bool {
	return func(p string) bool { return slices.Contains(paths, p) }
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name  string
		file  File
		conf  []string
		have  []string
		lib   string
		found bool
	}{
		{"default dir", File{Path: "/usr/bin/foo"}, nil, []string{"/usr/lib/libbar.so.3"}, "libbar.so.3", true},
		{"missing", File{Path: "/usr/bin/foo"}, nil, []string{"/usr/lib/libbar.so.2"}, "libbar.so.3", false},
		{"runpath origin", File{Path: "/opt/app/bin/foo", RunPath: []string{"$ORIGIN/../lib"}}, nil, []string{"/opt/app/lib/libx.so.1"}, "libx.so.1", true},
		{"runpath braces origin", File{Path: "/opt/app/bin/foo", RunPath: []string{"${ORIGIN}/../lib"}}, nil, []string{"/opt/app/lib/libx.so.1"}, "libx.so.1", true},
		{"runpath absolute", File{Path: "/usr/bin/foo", RunPath: []string{"/opt/priv"}}, nil, []string{"/opt/priv/libx.so.1"}, "libx.so.1", true},
		{"runpath elsewhere not searched", File{Path: "/usr/bin/foo", RunPath: []string{"/opt/priv"}}, nil, []string{"/opt/other/libx.so.1"}, "libx.so.1", false},
		{"runpath unknown variable gets the benefit of the doubt", File{Path: "/usr/bin/foo", RunPath: []string{"/opt/$PLATFORM/lib"}}, nil, nil, "libx.so.1", true},
		{"ld.so.conf dir", File{Path: "/usr/bin/foo"}, []string{"/opt/conf/lib"}, []string{"/opt/conf/lib/libx.so.1"}, "libx.so.1", true},
		{"32-bit finds lib32", File{Path: "/usr/lib32/x.so", Class32: true}, nil, []string{"/usr/lib32/libc.so.6"}, "libc.so.6", true},
		{"64-bit does not search lib32", File{Path: "/usr/bin/foo"}, nil, []string{"/usr/lib32/libc.so.6"}, "libc.so.6", false},
		{"absolute needed", File{Path: "/usr/bin/foo"}, nil, []string{"/opt/x/libx.so"}, "/opt/x/libx.so", true},
		{"absolute needed missing", File{Path: "/usr/bin/foo"}, nil, nil, "/opt/x/libx.so", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Searcher{ConfDirs: tt.conf, Exists: exists(tt.have...)}
			if got := s.Resolve(tt.file, tt.lib); got != tt.found {
				t.Errorf("Resolve = %v, want %v", got, tt.found)
			}
		})
	}
}

func TestSkipped(t *testing.T) {
	pats := SkipPatterns([]string{"/opt/mine"})
	for path, want := range map[string]bool{
		"/usr/lib/gstreamer-1.0/libgstfoo.so": true,
		"/usr/lib/qt6/plugins/a/b.so":         true,
		"/opt/mine/deep/x.so":                 true,
		"/usr/lib/libfoo.so.1":                false,
		"/usr/bin/foo":                        false,
		"/usr/lib/gstreamer-extra.so":         true, // matches the glob as a name too
	} {
		if got := Skipped(path, pats); got != want {
			t.Errorf("Skipped(%q) = %v, want %v", path, got, want)
		}
	}
}

// system builds a small installed system: libs, a healthy program, a broken
// one, and the awkward files a real system has.
func testSystem() (Options, []Package) {
	files := map[string][]byte{
		"/usr/bin/good":               fixtures.ELF(true, "libc.so.6", "libalpm.so.16"),
		"/usr/bin/broken":             fixtures.ELF(true, "libc.so.6", "libgone.so.3"),
		"/opt/app/bin/app":            fixtures.ELFWith(fixtures.ELFOptions{Class64: true, RunPath: "$ORIGIN/../lib"}, "libpriv.so.1"),
		"/opt/app/lib/libpriv.so.1":   fixtures.ELF(true),
		"/usr/lib32/lib32tool":        fixtures.ELF(false, "libc.so.6"),
		"/usr/lib/gstreamer-1.0/g.so": fixtures.ELF(true, "libnotthere.so.9"),
		"/usr/bin/script":             []byte("#!/bin/sh\necho hi\n"),
		"/usr/bin/static":             fixtures.ELF(true),
		"/usr/bin/truncated":          fixtures.ELF(true, "libc.so.6")[:40],
		"/usr/bin/conf":               fixtures.ELF(true, "libconf.so.2"),
		// An optional feature: its library is installed in no version.
		"/usr/lib/opt/plugin.so": fixtures.ELF(true, "libnowhere.so.5"),
		// A private library found through the executable's RUNPATH.
		"/usr/lib/priv/libcore-1.so":   fixtures.ELF(true, "libshared-1.so"),
		"/usr/lib/priv/libshared-1.so": fixtures.ELF(true),
	}
	opener := system.NewFakeOpener(files)
	fsys := system.NewFakeFS(map[string][]byte{
		"/etc/ld.so.conf":              []byte("include /etc/ld.so.conf.d/*.conf\n"),
		"/etc/ld.so.conf.d/x.conf":     []byte("/opt/conf/lib\n"),
		"/usr/lib/libc.so.6":           {1},
		"/usr/lib/libalpm.so.16":       {1},
		"/usr/lib/libgone.so.2":        {1}, // foo needs .so.3: a soname bump
		"/usr/lib32/libc.so.6":         {1},
		"/opt/app/lib/libpriv.so.1":    {1},
		"/opt/conf/lib/libconf.so.2":   {1},
		"/usr/lib/priv/libshared-1.so": {1},
	})
	pkgs := []Package{
		{Name: "pacman", Version: "7.0-1", Files: []string{"/usr/bin/good", "/usr/bin/script", "/usr/bin/static", "/usr/bin/truncated", "/usr/bin/dangling", "/usr/share/doc/x.txt"}},
		{Name: "foo", Version: "1.2-1", Files: []string{"/usr/bin/broken", "/usr/lib/gstreamer-1.0/g.so"}},
		{Name: "app", Version: "1-1", Files: []string{"/opt/app/bin/app", "/opt/app/lib/libpriv.so.1"}},
		{Name: "lib32-tool", Version: "1-1", Files: []string{"/usr/lib32/lib32tool"}},
		{Name: "confuser", Version: "1-1", Files: []string{"/usr/bin/conf"}},
		{Name: "optional", Version: "1-1", Files: []string{"/usr/lib/opt/plugin.so"}, OptDeps: []string{"libnowhere"}},
		{Name: "private", Version: "1-1", Files: []string{"/usr/lib/priv/libcore-1.so", "/usr/lib/priv/libshared-1.so"}},
	}
	return Options{Opener: opener, FS: fsys, Skip: SkipPatterns(nil), Workers: 3}, pkgs
}

func TestScanFindsExactlyTheBrokenFile(t *testing.T) {
	opt, pkgs := testSystem()
	res, err := Scan(context.Background(), opt, pkgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Broken) != 1 {
		t.Fatalf("Broken = %+v, want exactly one", res.Broken)
	}
	b := res.Broken[0]
	if b.Path != "/usr/bin/broken" || b.Package != "foo" || b.Version != "1.2-1" || b.Lib != "libgone.so.3" {
		t.Errorf("Broken = %+v", b)
	}
	if got := b.String(); got != "/usr/bin/broken (foo 1.2-1) needs libgone.so.3, which no installed package provides" {
		t.Errorf("String = %q", got)
	}
	// An unresolved library with no other version installed is not damage.
	if len(res.Optional) != 1 || res.Optional[0].Lib != "libnowhere.so.5" || !slices.Equal(res.Optional[0].OptDeps, []string{"libnowhere"}) {
		t.Errorf("Optional = %+v, want the plugin's libnowhere.so.5", res.Optional)
	}
	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want the gstreamer plugin", res.Skipped)
	}
	if want := []string{"foo"}; !slices.Equal(res.BrokenPackages(), want) {
		t.Errorf("BrokenPackages = %v", res.BrokenPackages())
	}
}

func TestScanUnresolvedWithoutSiblingIsOptional(t *testing.T) {
	opt, pkgs := testSystem()
	// Only libc is left: nothing has another version of its library, so
	// nothing is a soname mismatch.
	opt.FS = system.NewFakeFS(map[string][]byte{"/usr/lib/libc.so.6": {1}})
	res, err := Scan(context.Background(), opt, pkgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Broken) != 0 {
		t.Errorf("Broken = %+v, want none", res.Broken)
	}
	var libs []string
	for _, b := range res.Optional {
		libs = append(libs, b.Package+":"+b.Lib)
	}
	for _, want := range []string{"pacman:libalpm.so.16", "foo:libgone.so.3", "app:libpriv.so.1", "lib32-tool:libc.so.6", "confuser:libconf.so.2"} {
		if !slices.Contains(libs, want) {
			t.Errorf("missing %s in %v", want, libs)
		}
	}
}

func TestScanPrivateLibraryInOwnDirectory(t *testing.T) {
	opt, pkgs := testSystem()
	res, err := Scan(context.Background(), opt, pkgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range append(slices.Clone(res.Broken), res.Optional...) {
		if b.Package == "private" {
			t.Errorf("%+v reported, but the library is in the same directory", b)
		}
	}
}

func TestClassify(t *testing.T) {
	dir := map[string][]string{"/usr/lib": {"libfoo.so.2", "libbar.so", "libbaz-1.0.so.4"}}
	s := Searcher{
		Exists: exists("/usr/lib/libfoo.so.2", "/usr/lib/libbar.so", "/usr/lib/libbaz-1.0.so.4"),
		List:   func(d string) []string { return dir[d] },
	}
	f := File{Path: "/usr/bin/x"}
	for lib, want := range map[string]Status{
		"libfoo.so.2":     Found,
		"libfoo.so.3":     Mismatch,
		"libfoo.so":       Mismatch,
		"libbar.so.1":     Mismatch,
		"libbaz-1.0.so.3": Mismatch,
		"libbaz-2.0.so.4": Missing, // a different library, not a sibling
		"libqux.so.1":     Missing,
		"libfoobar.so.1":  Missing, // libfoo.so is not a prefix of it
		"/opt/gone.so":    Missing,
	} {
		if got := s.Classify(f, lib); got != want {
			t.Errorf("Classify(%q) = %v, want %v", lib, got, want)
		}
	}
	// A library of the same package found through another file's RUNPATH.
	s.Extra = []string{"/opt/p/lib"}
	s.Exists = exists("/opt/p/lib/libpriv.so.1")
	if got := s.Classify(f, "libpriv.so.1"); got != Found {
		t.Errorf("Classify via Extra = %v, want Found", got)
	}
}

func TestScanCache(t *testing.T) {
	opt, pkgs := testSystem()
	opt.Cache = &Cache{}
	fo := opt.Opener.(*system.FakeOpener)
	if _, err := Scan(context.Background(), opt, pkgs); err != nil {
		t.Fatal(err)
	}
	first := len(fo.Opened())
	if first == 0 {
		t.Fatal("first scan opened nothing")
	}

	// Through a marshal round trip, as the next run would see it.
	opt.Cache = ParseCache(opt.Cache.Marshal())
	res, err := Scan(context.Background(), opt, pkgs)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(fo.Opened()); got != first {
		t.Errorf("cached scan opened %d more files", got-first)
	}
	if len(res.Broken) != 1 || len(res.Optional) != 1 {
		t.Errorf("cached scan Broken = %+v", res.Broken)
	}

	// A new version of one package is read again, and only that one.
	pkgs[1].Version = "1.3-1"
	if _, err := Scan(context.Background(), opt, pkgs); err != nil {
		t.Fatal(err)
	}
	if got := len(fo.Opened()) - first; got != 2 { // broken + the gstreamer plugin
		t.Errorf("new version opened %d files, want 2", got)
	}

	// Fresh ignores the cache.
	before := len(fo.Opened())
	opt.Fresh = true
	if _, err := Scan(context.Background(), opt, pkgs); err != nil {
		t.Fatal(err)
	}
	if len(fo.Opened()) <= before {
		t.Error("Fresh didn't re-read")
	}

	// Garbage and other versions make an empty cache, never an error.
	for _, in := range []string{"", "{", `{"version":99,"packages":{"a":{}}}`} {
		if c := ParseCache([]byte(in)); len(c.packages) != 0 {
			t.Errorf("ParseCache(%q) has entries", in)
		}
	}
}

func TestScanOpensOnlyCandidates(t *testing.T) {
	opt, pkgs := testSystem()
	fo := opt.Opener.(*system.FakeOpener)
	if _, err := Scan(context.Background(), opt, pkgs); err != nil {
		t.Fatal(err)
	}
	for _, p := range fo.Opened() {
		if strings.HasPrefix(p, "/usr/share/") {
			t.Errorf("opened %s", p)
		}
	}
}

func TestScanCancelAndProgress(t *testing.T) {
	opt, pkgs := testSystem()
	var last Progress
	var calls atomic.Int32
	opt.OnProgress = func(p Progress) { last = p; calls.Add(1) }
	if _, err := Scan(context.Background(), opt, pkgs); err != nil {
		t.Fatal(err)
	}
	if int(calls.Load()) != len(pkgs) || last.Done != len(pkgs) || last.Total != len(pkgs) {
		t.Errorf("progress calls = %d, last = %+v", calls.Load(), last)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opt.Cache = &Cache{}
	if _, err := Scan(ctx, opt, pkgs); err == nil {
		t.Error("cancelled scan returned no error")
	}

	// Cancelling part-way keeps what was read.
	ctx, cancel = context.WithCancel(context.Background())
	opt.Workers = 1
	opt.OnProgress = func(p Progress) {
		if p.Done == 2 {
			cancel()
		}
	}
	opt.Cache = &Cache{}
	if _, err := Scan(ctx, opt, pkgs); err == nil {
		t.Error("part-way cancel returned no error")
	}
	if _, ok := opt.Cache.Get(pkgs[0].Name, pkgs[0].Version); !ok {
		t.Error("finished package not cached after cancel")
	}
	if _, ok := opt.Cache.Get(pkgs[4].Name, pkgs[4].Version); ok {
		t.Error("unfinished package cached")
	}
}

func TestVerdict(t *testing.T) {
	_, pkgs := testSystem()
	c := &Cache{}
	c.Save(pkgs, Result{Broken: []Broken{{Path: "/x", Package: "foo", Lib: "libx.so"}}}, fixtures.Now)
	c = ParseCache(c.Marshal())
	v, ok := c.Verdict(pkgs)
	if !ok || len(v.Broken) != 1 {
		t.Fatalf("Verdict = %+v, %v", v, ok)
	}
	pkgs[0].Version = "9-9"
	if _, ok := c.Verdict(pkgs); ok {
		t.Error("verdict survived a package change")
	}
}
