package pacman

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/system"
)

func TestSyncDBPath(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		uid  int
		want string
	}{
		{"default", nil, 1000, "/tmp/checkup-db-1000"},
		{"TMPDIR", map[string]string{"TMPDIR": "/run/user/1000"}, 1000, "/run/user/1000/checkup-db-1000"},
		{"TMPDIR trailing slash", map[string]string{"TMPDIR": "/var/tmp/"}, 7, "/var/tmp/checkup-db-7"},
		{"CHECKUPDATES_DB wins", map[string]string{"CHECKUPDATES_DB": "/data/db/", "TMPDIR": "/x"}, 1000, "/data/db"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SyncDBPath(func(k string) string { return tt.env[k] }, tt.uid); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckSyncDB(t *testing.T) {
	tests := []struct {
		name  string
		files map[string][]byte
		ok    bool
	}{
		{"present", map[string][]byte{"/db/sync/core.db": nil}, true},
		{"nothing", map[string][]byte{"/other": nil}, false},
		{"no sync dir", map[string][]byte{"/db/local/x": nil}, false},
		{"sync is a file", map[string][]byte{"/db/sync": nil}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckSyncDB(system.NewFakeFS(tt.files), "/db")
			if tt.ok != (err == nil) {
				t.Fatalf("err = %v, want ok=%v", err, tt.ok)
			}
			if err != nil && !errors.Is(err, ErrPreviewUnavailable) {
				t.Errorf("err = %v, want ErrPreviewUnavailable", err)
			}
		})
	}
}

func TestParsePrint(t *testing.T) {
	got, err := ParsePrint([]byte("\nfoo 1.0-1 extra 1048576\nlibbar 1:2.0-3 core 0\n"))
	want := []Printed{
		{Name: "foo", Version: "1.0-1", Repo: "extra", DownloadSize: 1048576},
		{Name: "libbar", Version: "1:2.0-3", Repo: "core"},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, %v; want %v", got, err, want)
	}
	for _, bad := range []string{"foo 1.0-1 extra\n", "foo 1.0-1 extra x\n", "foo 1.0-1 extra -3\n", "foo 1 extra 2 3\n"} {
		if _, err := ParsePrint([]byte(bad)); err == nil {
			t.Errorf("ParsePrint(%q) succeeded", bad)
		}
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"", 0, false},
		{"0.00 B", 0, false},
		{"512.00 B", 512, false},
		{"1.50 KiB", 1536, false},
		{"12.34 MiB", 12939428, false},
		{"2.00 GiB", 2 << 30, false},
		{"12.34", 0, true},
		{"12.34 MB", 0, true},
		{"abc MiB", 0, true},
		{"-1.00 MiB", 0, true},
	}
	for _, tt := range tests {
		got, err := parseSize(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("parseSize(%q) = %d, %v; want %d, err=%v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

const infoSample = `Repository      : extra
Name            : foo
Version         : 1:1.2-3
Description     : A: tricky description
Provides        : libfoo.so=1-64  foo-compat
Depends On      : glibc  libbar>=2
Optional Deps   : baz: for baz
                  qux: for qux
Replaces        : None
Download Size   : 1.50 KiB
Installed Size  : 12.34 MiB
Build Date      : Tue Oct  7 04:05:06 2025

Name            : bar
Version         : 2-1
Depends On      : a  b
                  c
Installed Size  : 10.00 B
Build Date      : not a date
`

func TestParseInfo(t *testing.T) {
	loc := time.FixedZone("test", 3600)
	got, err := ParseInfo([]byte(infoSample), loc)
	if err != nil {
		t.Fatal(err)
	}
	want := []Info{
		{
			Name: "foo", Version: "1:1.2-3", Repository: "extra",
			InstalledSize: 12939428, DownloadSize: 1536,
			DependsOn: []string{"glibc", "libbar>=2"},
			Provides:  []string{"libfoo.so=1-64", "foo-compat"},
			BuildDate: time.Date(2025, 10, 7, 4, 5, 6, 0, loc),
		},
		{Name: "bar", Version: "2-1", InstalledSize: 10, DependsOn: []string{"a", "b", "c"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}

	for _, bad := range []string{"no colon here\n", "Version : 1\n", "Name : x\nInstalled Size : lots\n", "Name : x\nDownload Size : 1 MB\n"} {
		if _, err := ParseInfo([]byte(bad), loc); err == nil {
			t.Errorf("ParseInfo(%q) succeeded", bad)
		}
	}
	if got, err := ParseInfo(nil, loc); err != nil || got != nil {
		t.Errorf("empty: %v, %v", got, err)
	}
}

func TestParseHeld(t *testing.T) {
	got, err := ParseHeld([]byte("foo 1-1 -> 2-1\nbash 5.3.3-1 -> 5.3.3-2 [ignored]\n\n"))
	want := []Held{{Name: "bash", OldVersion: "5.3.3-1", NewVersion: "5.3.3-2"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, %v; want %v", got, err, want)
	}
	for _, bad := range []string{"foo 1-1 2-1\n", "foo 1-1 -> 2-1 [x] y\n", "foo\n"} {
		if _, err := ParseHeld([]byte(bad)); err == nil {
			t.Errorf("ParseHeld(%q) succeeded", bad)
		}
	}
}

func TestHeldPackagesExitCodes(t *testing.T) {
	const line = "pacman -Qu --dbpath /db --logfile /dev/null"
	tests := []struct {
		name    string
		res     system.FakeResult
		want    int
		wantErr bool
	}{
		{"none pending: exit 1, no output", system.FakeResult{Code: 1}, 0, false},
		{"real error", system.FakeResult{Code: 1, Stderr: []byte("error: database file missing")}, 0, true},
		{"could not run", system.FakeResult{Err: system.ErrCommandNotFound}, 0, true},
		{"held", system.FakeResult{Stdout: []byte("a 1-1 -> 2-1 [ignored]\n")}, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := system.NewFakeRunner()
			r.Set(line, tt.res)
			got, err := HeldPackages(context.Background(), r, "/db")
			if (err != nil) != tt.wantErr || len(got) != tt.want {
				t.Errorf("got %v, %v", got, err)
			}
		})
	}
}

// The calls into the checkupdates database must always carry --dbpath and
// --logfile /dev/null, and package names must come after "--".
func TestCommandLines(t *testing.T) {
	ctx := context.Background()
	r := fixtures.Runner()
	loc := time.UTC
	if _, err := SyncPrint(ctx, r, fixtures.SyncDBPath); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncInfo(ctx, r, fixtures.SyncDBPath, []string{"zstd", "mesa", "zstd"}, loc); err != nil {
		t.Fatal(err)
	}
	if _, err := LocalInfo(ctx, r, []string{"zstd", "mesa"}, loc); err != nil {
		t.Fatal(err)
	}
	if _, err := HeldPackages(ctx, r, fixtures.SyncDBPath); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"pacman -Sup --dbpath /tmp/checkup-db-1000 --logfile /dev/null --noconfirm --print-format %n %v %r %s",
		"pacman -Si --dbpath /tmp/checkup-db-1000 --logfile /dev/null -- mesa zstd",
		"pacman -Qi -- mesa zstd",
		"pacman -Qu --dbpath /tmp/checkup-db-1000 --logfile /dev/null",
	}
	if got := r.Calls(); !slices.Equal(got, want) {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestInfoRefusesBadNames(t *testing.T) {
	r := system.NewFakeRunner()
	for _, name := range []string{"-Rns", "foo bar", "Foo", "a;b", "", "$(x)", "../x"} {
		if _, err := SyncInfo(context.Background(), r, "/db", []string{"ok", name}, time.UTC); err == nil {
			t.Errorf("SyncInfo accepted %q", name)
		}
		if _, err := LocalInfo(context.Background(), r, []string{name}, time.UTC); err == nil {
			t.Errorf("LocalInfo accepted %q", name)
		}
	}
	if calls := r.Calls(); len(calls) != 0 {
		t.Errorf("ran %v", calls)
	}
}

func TestInfoBatches(t *testing.T) {
	var names []string
	for i := range 250 {
		names = append(names, "pkg"+string(rune('a'+i/26/26%26))+string(rune('a'+i/26%26))+string(rune('a'+i%26)))
	}
	r := system.NewFakeRunner()
	r.Handler = func(string) (system.FakeResult, bool) { return system.FakeResult{}, true }
	if _, err := LocalInfo(context.Background(), r, names, time.UTC); err != nil {
		t.Fatal(err)
	}
	if got := len(r.Calls()); got != 3 {
		t.Errorf("%d calls for 250 names, want 3", got)
	}
}

// The expected totals are worked out by hand from the hand-written fixtures.
func TestPreviewTransactionFixtures(t *testing.T) {
	ctx := context.Background()
	updates, err := CheckUpdates(ctx, fixtures.Runner())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := PreviewTransaction(ctx, fixtures.Runner(), fixtures.FS(), fixtures.SyncDBPath, updates, time.UTC)
	if err != nil {
		t.Fatal(err)
	}

	if len(tx.Updates) != 16 {
		t.Errorf("%d updates, want 16", len(tx.Updates))
	}
	if len(tx.New) != 2 || tx.New[0].Name != "egl-wayland2" || tx.New[1].Name != "iptables-nft" {
		t.Fatalf("new = %+v", tx.New)
	}
	if got := tx.New[0].NeededBy; !reflect.DeepEqual(got, []string{"nvidia-utils"}) {
		t.Errorf("egl-wayland2 needed by %v, want [nvidia-utils]", got)
	}
	if len(tx.New[1].NeededBy) != 0 || !reflect.DeepEqual(tx.New[1].Replaces, []string{"iptables"}) {
		t.Errorf("iptables-nft = %+v", tx.New[1])
	}
	wantRemoved := []RemovedPackage{{Name: "iptables", Version: "1:1.8.11-2", Size: 1992294, ReplacedBy: "iptables-nft"}}
	if !reflect.DeepEqual(tx.Removed, wantRemoved) {
		t.Errorf("removed = %+v, want %+v", tx.Removed, wantRemoved)
	}
	wantHeld := []Held{{Name: "bash", OldVersion: "5.3.3-1", NewVersion: "5.3.3-2"}}
	if !reflect.DeepEqual(tx.Held, wantHeld) {
		t.Errorf("held = %+v", tx.Held)
	}

	// The updates keep checkupdates' versions and gain sizes and ages.
	byName := map[string]UpdateDetail{}
	for _, d := range tx.Updates {
		byName[d.Name] = d
	}
	nv := byName["nvidia-utils"]
	if nv.OldVersion != "580.95.05-1" || nv.NewVersion != "590.44.01-1" || nv.Repo != "extra" ||
		nv.DownloadSize != 167877018 || nv.NewSize-nv.OldSize != 16252928 {
		t.Errorf("nvidia-utils = %+v", nv)
	}
	if want := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC); !nv.BuildDate.Equal(want) {
		t.Errorf("nvidia-utils built %v, want %v", nv.BuildDate, want)
	}
	if z := byName["zstd"]; z.DownloadSize != 0 {
		t.Errorf("zstd is cached, download = %d", z.DownloadSize)
	}

	// Totals: the download is the sum of the print; the size change is the
	// sum of every installed-size change, with the replaced package gone.
	const wantDownload, wantDelta = 929069795, 27053259
	if tx.DownloadBytes != wantDownload || tx.SizeDelta != wantDelta {
		t.Errorf("download %d, delta %d; want %d, %d", tx.DownloadBytes, tx.SizeDelta, wantDownload, wantDelta)
	}
	var dl, delta int64
	for _, d := range tx.Updates {
		dl += d.DownloadSize
		delta += d.NewSize - d.OldSize
	}
	for _, n := range tx.New {
		dl += n.DownloadSize
		delta += n.Size
	}
	for _, r := range tx.Removed {
		delta -= r.Size
	}
	if dl != tx.DownloadBytes || delta != tx.SizeDelta {
		t.Errorf("rows add up to %d and %d, totals say %d and %d", dl, delta, tx.DownloadBytes, tx.SizeDelta)
	}
}

func TestBuildTransactionEdgeCases(t *testing.T) {
	in := TransactionInput{
		Updates: []Update{{Name: "a", OldVersion: "1-1", NewVersion: "2-1"}},
		Printed: []Printed{
			{Name: "a", Version: "2-1", Repo: "core", DownloadSize: 100},
			// Replaces a package that is itself being upgraded, and one that
			// isn't installed: neither is removed.
			{Name: "n", Version: "1-1", Repo: "extra", DownloadSize: 10},
			// Provides what a depends on, with a version.
			{Name: "p", Version: "1-1", Repo: "extra"},
			// Printed and installed but not in checkupdates (a race).
			{Name: "z", Version: "3-1", Repo: "core", DownloadSize: 1},
		},
		Installed: map[string]bool{"a": true, "z": true, "old": true},
		Sync: map[string]Info{
			"a": {InstalledSize: 50, DependsOn: []string{"libp.so>=2"}},
			"n": {InstalledSize: 7, Replaces: []string{"a", "ghost", "old<3"}},
			"p": {InstalledSize: 3, Provides: []string{"libp.so=2-64"}},
			"z": {InstalledSize: 9},
		},
		Local: map[string]Info{
			"a": {Version: "1-1", InstalledSize: 40}, "z": {Version: "2-1", InstalledSize: 8},
			"old": {Version: "2-1", InstalledSize: 5},
		},
	}
	tx := BuildTransaction(in)
	if len(tx.Updates) != 2 || tx.Updates[1].OldVersion != "2-1" || tx.Updates[1].NewVersion != "3-1" {
		t.Errorf("updates = %+v", tx.Updates)
	}
	if len(tx.New) != 2 || tx.New[0].Name != "n" || !reflect.DeepEqual(tx.New[0].Replaces, []string{"old"}) {
		t.Errorf("new = %+v", tx.New)
	}
	if !reflect.DeepEqual(tx.New[1].NeededBy, []string{"a"}) {
		t.Errorf("p needed by %v, want [a]", tx.New[1].NeededBy)
	}
	if len(tx.Removed) != 1 || tx.Removed[0].Name != "old" || tx.Removed[0].ReplacedBy != "n" {
		t.Errorf("removed = %+v", tx.Removed)
	}
	// +50-40 +7 +3 +9-8 -5
	if tx.SizeDelta != 16 || tx.DownloadBytes != 111 {
		t.Errorf("delta %d download %d, want 16 and 111", tx.SizeDelta, tx.DownloadBytes)
	}
}

func TestPreviewTransactionFailures(t *testing.T) {
	ctx := context.Background()
	const sup = "pacman -Sup --dbpath /tmp/checkup-db-1000 --logfile /dev/null --noconfirm --print-format %n %v %r %s"

	t.Run("no database", func(t *testing.T) {
		_, err := PreviewTransaction(ctx, fixtures.Runner(), system.NewFakeFS(nil), fixtures.SyncDBPath, nil, time.UTC)
		if !errors.Is(err, ErrPreviewUnavailable) {
			t.Errorf("err = %v, want ErrPreviewUnavailable", err)
		}
	})
	t.Run("pacman fails", func(t *testing.T) {
		r := fixtures.Runner()
		r.Set(sup, system.FakeResult{Code: 1, Stderr: []byte("error: could not satisfy dependencies")})
		_, err := PreviewTransaction(ctx, r, fixtures.FS(), fixtures.SyncDBPath, nil, time.UTC)
		if err == nil || !strings.Contains(err.Error(), "could not satisfy dependencies") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("nothing to do", func(t *testing.T) {
		r := fixtures.Runner()
		r.Set(sup, system.FakeResult{})
		r.Set("pacman -Qu --dbpath /tmp/checkup-db-1000 --logfile /dev/null", system.FakeResult{Code: 1})
		tx, err := PreviewTransaction(ctx, r, fixtures.FS(), fixtures.SyncDBPath, nil, time.UTC)
		if err != nil || len(tx.Updates)+len(tx.New)+len(tx.Held) != 0 || tx.DownloadBytes != 0 {
			t.Errorf("tx = %+v, %v", tx, err)
		}
	})
}

func TestTransactionPrefetchedBytes(t *testing.T) {
	tx := BuildTransaction(TransactionInput{
		Updates: []Update{{Name: "a", OldVersion: "1", NewVersion: "2"}, {Name: "b", OldVersion: "1", NewVersion: "2"}},
		Printed: []Printed{
			{Name: "a", Version: "2", Repo: "core", DownloadSize: 100 << 20}, // still to download
			{Name: "b", Version: "2", Repo: "core", DownloadSize: 0},         // already in the cache
		},
		Installed: map[string]bool{"a": true, "b": true},
		Sync:      map[string]Info{"a": {DownloadSize: 100 << 20}, "b": {DownloadSize: 70 << 20}},
	})
	if tx.DownloadBytes != 100<<20 || tx.PrefetchedBytes != 70<<20 || tx.TotalBytes() != 170<<20 {
		t.Errorf("download %d, prefetched %d, total %d", tx.DownloadBytes, tx.PrefetchedBytes, tx.TotalBytes())
	}
	if tx.Packages() != 2 {
		t.Errorf("Packages = %d, want 2", tx.Packages())
	}
}
