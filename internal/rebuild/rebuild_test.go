package rebuild

import (
	"context"
	"reflect"
	"testing"

	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/system"
)

func TestParseSoname(t *testing.T) {
	tests := []struct {
		dep  string
		want Soname
		ok   bool
	}{
		{"libicuuc.so=75-64", Soname{"libicuuc.so", "75", "64"}, true},
		{"libfoo.so=0-32", Soname{"libfoo.so", "0", "32"}, true},
		{"libfoo.so=6.0.1-64", Soname{"libfoo.so", "6.0.1", "64"}, true},
		{"lib:libicuuc.so.75", Soname{"libicuuc.so", "75", ""}, true},
		{"lib:libfoo.so", Soname{"libfoo.so", "", ""}, true},
		{"lib:libstdc++.so.6.0.33", Soname{"libstdc++.so", "6.0.33", ""}, true},
		{"lib32:libz.so.1", Soname{"libz.so", "1", "32"}, true},
		{"lib:libsodium.so.26", Soname{"libsodium.so", "26", ""}, true},
		{"foo=1.0-1", Soname{}, false},
		{"bar>=2", Soname{}, false},
		{"libfoo.so", Soname{}, false},
		{"libfoo.so=75", Soname{}, false},
		{"libfoo.so=75-arm", Soname{}, false},
		{"lib:notalib", Soname{}, false},
		{"lib:", Soname{}, false},
		{"", Soname{}, false},
	}
	for _, tt := range tests {
		got, ok := ParseSoname(tt.dep)
		if ok != tt.ok || got != tt.want {
			t.Errorf("ParseSoname(%q) = %+v, %v; want %+v, %v", tt.dep, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSonameFileAndSame(t *testing.T) {
	if got := (Soname{"libicuuc.so", "75", "64"}).File(); got != "libicuuc.so.75" {
		t.Errorf("File = %q", got)
	}
	if got := (Soname{"libfoo.so", "0", "64"}).File(); got != "libfoo.so" {
		t.Errorf("File = %q", got)
	}
	// Both spellings of one library are the same.
	a, _ := ParseSoname("libicuuc.so=75-64")
	b, _ := ParseSoname("lib:libicuuc.so.75")
	if !a.Same(b) || !b.Same(a) {
		t.Error("the two spellings should name the same soname")
	}
	c, _ := ParseSoname("lib32:libicuuc.so.75")
	if a.Same(c) {
		t.Error("a 32-bit soname is not the 64-bit one")
	}
	d, _ := ParseSoname("libicuuc.so=76-64")
	if a.Same(d) {
		t.Error("another version is another soname")
	}
}

func TestLost(t *testing.T) {
	tests := []struct {
		name     string
		old, new []string
		want     []string // File() of lost sonames
	}{
		{"soname bump", []string{"libicuuc.so=75-64", "libicui18n.so=75-64"}, []string{"libicuuc.so=76-64", "libicui18n.so=76-64"}, []string{"libicuuc.so.75", "libicui18n.so.75"}},
		{"unchanged", []string{"libgbm.so=1-64"}, []string{"libgbm.so=1-64"}, nil},
		{"unchanged across spellings", []string{"libgbm.so=1-64"}, []string{"lib:libgbm.so.1"}, nil},
		{"one of two", []string{"liba.so=1-64", "libb.so=2-64"}, []string{"liba.so=1-64", "libb.so=3-64"}, []string{"libb.so.2"}},
		{"dropped entirely", []string{"liba.so=1-64"}, nil, []string{"liba.so.1"}},
		{"non-sonames ignored", []string{"foo=1.0", "libx.so=1-64"}, []string{"foo=2.0", "libx.so=1-64"}, nil},
		{"new only", nil, []string{"liba.so=1-64"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, s := range Lost(Update{OldProvides: tt.old, NewProvides: tt.new}) {
				got = append(got, s.File())
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Lost = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMoved(t *testing.T) {
	tests := []struct {
		name, old, new string
		want           []string
	}{
		{"python minor", "3.13.7-1", "3.14.0-1", []string{"/usr/lib/python3.13/"}},
		{"python patch", "3.13.7-1", "3.13.8-1", nil},
		{"python epoch", "1:3.13.7-1", "1:3.14.0-2", []string{"/usr/lib/python3.13/"}},
		{"perl", "5.40.2-1", "5.42.0-1", []string{"/usr/lib/perl5/5.40/"}},
		{"ruby", "3.3.5-1", "3.4.1-1", []string{"/usr/lib/ruby/3.3.0/", "/usr/lib/ruby/gems/3.3.0/", "/usr/lib/ruby/vendor_ruby/3.3.0/"}},
		{"unparsable", "git", "3.14.0-1", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := "python"
			switch tt.name {
			case "perl", "ruby":
				name = tt.name
			}
			got, ok := Moved(Update{Name: name, OldVersion: tt.old, NewVersion: tt.new})
			if ok != (tt.want != nil) || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Moved = %v, %v; want %v", got, ok, tt.want)
			}
		})
	}
	if _, ok := Moved(Update{Name: "vim", OldVersion: "9.1-1", NewVersion: "9.2-1"}); ok {
		t.Error("vim is not an interpreter")
	}
}

var icu = Update{
	Name: "icu", OldVersion: "75.1-1", NewVersion: "76.1-1",
	OldProvides: []string{"libicuuc.so=75-64", "libicui18n.so=75-64"},
	NewProvides: []string{"libicuuc.so=76-64", "libicui18n.so=76-64"},
}

func TestDetect(t *testing.T) {
	py := Update{Name: "python", OldVersion: "3.13.7-1", NewVersion: "3.14.0-1"}
	tests := []struct {
		name    string
		updates []Update
		foreign []Foreign
		want    []Finding
	}{
		{
			name: "metadata, both spellings", updates: []Update{icu},
			foreign: []Foreign{
				{Name: "a", Depends: []string{"libicuuc.so=75-64"}},
				{Name: "b", Depends: []string{"lib:libicui18n.so.75"}},
			},
			want: []Finding{
				{Package: "a", Signal: SignalMetadata, Culprit: "icu", CulpritOld: "75.1-1", CulpritNew: "76.1-1", Detail: "libicuuc.so.75"},
				{Package: "b", Signal: SignalMetadata, Culprit: "icu", CulpritOld: "75.1-1", CulpritNew: "76.1-1", Detail: "libicui18n.so.75"},
			},
		},
		{
			name: "elf", updates: []Update{icu},
			foreign: []Foreign{{Name: "bundled", Needs: []Need{{"libc.so.6", "64"}, {"libicuuc.so.75", "64"}}}},
			want:    []Finding{{Package: "bundled", Signal: SignalELF, Culprit: "icu", CulpritOld: "75.1-1", CulpritNew: "76.1-1", Detail: "libicuuc.so.75"}},
		},
		{
			name: "both signals are both reported", updates: []Update{icu},
			foreign: []Foreign{{Name: "x", Depends: []string{"libicuuc.so=75-64"}, Needs: []Need{{"libicuuc.so.75", "64"}}}},
			want: []Finding{
				{Package: "x", Signal: SignalMetadata, Culprit: "icu", CulpritOld: "75.1-1", CulpritNew: "76.1-1", Detail: "libicuuc.so.75"},
				{Package: "x", Signal: SignalELF, Culprit: "icu", CulpritOld: "75.1-1", CulpritNew: "76.1-1", Detail: "libicuuc.so.75"},
			},
		},
		{
			name: "module dir", updates: []Update{py},
			foreign: []Foreign{{Name: "python-pywal", Files: []string{"/usr/bin/wal", "/usr/lib/python3.13/site-packages/pywal/__init__.py"}}},
			want:    []Finding{{Package: "python-pywal", Signal: SignalModuleDir, Culprit: "python", CulpritOld: "3.13.7-1", CulpritNew: "3.14.0-1", Detail: "/usr/lib/python3.13/"}},
		},
		{
			name: "unchanged soname", updates: []Update{{Name: "mesa", OldProvides: []string{"libgbm.so=1-64"}, NewProvides: []string{"libgbm.so=1-64"}}},
			foreign: []Foreign{{Name: "spotify", Depends: []string{"libgbm.so=1-64"}, Needs: []Need{{"libgbm.so.1", "64"}}}},
		},
		{
			name: "debug split", updates: []Update{icu},
			foreign: []Foreign{{Name: "foo-debug", Debug: true, Depends: []string{"libicuuc.so=75-64"}, Needs: []Need{{"libicuuc.so.75", "64"}}}},
		},
		{
			name: "vcs package that doesn't link the library", updates: []Update{icu},
			foreign: []Foreign{{Name: "neovim-git", Needs: []Need{{"libc.so.6", "64"}, {"libicuuc.so.76", "64"}}}},
		},
		{
			name: "32-bit binary doesn't need the 64-bit library", updates: []Update{icu},
			foreign: []Foreign{{Name: "wine-thing", Needs: []Need{{"libicuuc.so.75", "32"}}}},
		},
		{
			name: "other version of the dependency", updates: []Update{icu},
			foreign: []Foreign{{Name: "new", Depends: []string{"libicuuc.so=76-64"}}},
		},
		{
			name: "python patch update", updates: []Update{{Name: "python", OldVersion: "3.13.7-1", NewVersion: "3.13.8-1"}},
			foreign: []Foreign{{Name: "python-pywal", Files: []string{"/usr/lib/python3.13/site-packages/x.py"}}},
		},
		{
			name: "files only under the new dir", updates: []Update{py},
			foreign: []Foreign{{Name: "p", Files: []string{"/usr/lib/python3.14/site-packages/x.py", "/usr/lib/python3.130/x"}}},
		},
		{name: "no updates", foreign: []Foreign{{Name: "a", Depends: []string{"libicuuc.so=75-64"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Detect(tt.updates, tt.foreign)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Detect =\n %+v\nwant\n %+v", got, tt.want)
			}
		})
	}
}

func TestCandidate(t *testing.T) {
	for path, want := range map[string]bool{
		"/usr/bin/foo":                           true,
		"/usr/lib/libfoo.so.1":                   true,
		"/opt/spotify/spotify":                   true,
		"/usr/lib/python3.13/site-packages/x.py": false,
		"/usr/lib/debug/usr/bin/foo.debug":       false,
		"/usr/share/doc/foo/README":              false,
		"/usr/lib/foo/":                          false,
		"/usr/lib/foo/data.json":                 false,
	} {
		if got := Candidate(path); got != want {
			t.Errorf("Candidate(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestScanNeeds(t *testing.T) {
	op := system.NewFakeOpener(map[string][]byte{
		"/usr/bin/a":         fixtures.ELF(true, "libc.so.6", "libicuuc.so.75"),
		"/usr/lib/libb.so.1": fixtures.ELF(true, "libc.so.6"),
		"/usr/lib/lib32.so":  fixtures.ELF(false, "libc.so.6"),
		"/usr/bin/script":    []byte("#!/bin/sh\necho hi\n"),
		"/usr/bin/empty":     nil,
		"/usr/bin/truncated": fixtures.ELF(true, "libx.so.1")[:70],
		"/usr/share/doc/x":   fixtures.ELF(true, "libshould-not-open.so"),
	})
	got, err := ScanNeeds(context.Background(), op, []string{
		"/usr/bin/a", "/usr/lib/libb.so.1", "/usr/lib/lib32.so", "/usr/bin/script",
		"/usr/bin/empty", "/usr/bin/truncated", "/usr/bin/missing", "/usr/share/doc/x", "/usr/bin/",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Need{{"libc.so.6", "32"}, {"libc.so.6", "64"}, {"libicuuc.so.75", "64"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ScanNeeds = %v, want %v", got, want)
	}
	for _, p := range op.Opened() {
		if p == "/usr/share/doc/x" || p == "/usr/bin/" {
			t.Errorf("opened %s", p)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ScanNeeds(ctx, op, []string{"/usr/bin/a"}); err == nil {
		t.Error("a cancelled scan should fail")
	}
}

func TestCache(t *testing.T) {
	op := system.NewFakeOpener(map[string][]byte{"/usr/bin/a": fixtures.ELF(true, "libx.so.1")})
	files := []string{"/usr/bin/a"}
	ctx := context.Background()

	c := ParseCache(nil)
	got, err := c.Needs(ctx, op, "a", "1-1", files, false)
	if err != nil || len(got) != 1 || !c.Dirty {
		t.Fatalf("first = %v, %v, dirty %v", got, err, c.Dirty)
	}
	if n := len(op.Opened()); n != 1 {
		t.Fatalf("opened %d files", n)
	}

	// It round-trips, and the same version isn't scanned again.
	c2 := ParseCache(c.Marshal())
	if c2.Dirty {
		t.Error("a parsed cache is clean")
	}
	if got, _ := c2.Needs(ctx, op, "a", "1-1", files, false); len(got) != 1 {
		t.Errorf("cached = %v", got)
	}
	if n := len(op.Opened()); n != 1 {
		t.Errorf("a cache hit opened files: %d opens", n)
	}

	// A new version, or fresh, scans again.
	_, _ = c2.Needs(ctx, op, "a", "1-2", files, false)
	_, _ = c2.Needs(ctx, op, "a", "1-2", files, true)
	if n := len(op.Opened()); n != 3 {
		t.Errorf("opens = %d, want 3", n)
	}

	c2.Keep(map[string]string{"other": "1"})
	if _, ok := c2.Get("a", "1-2"); ok {
		t.Error("Keep should drop uninstalled packages")
	}
}

func TestParseCacheCorrupt(t *testing.T) {
	for _, data := range []string{"", "not json", `{"version":99,"packages":{"a":{"version":"1"}}}`, `[]`} {
		c := ParseCache([]byte(data))
		if _, ok := c.Get("a", "1"); ok {
			t.Errorf("%q: should be an empty cache", data)
		}
		c.Put("a", "1", nil) // must not panic
	}
}
