package mirror

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/system"
)

func TestRoot(t *testing.T) {
	tests := []struct {
		name    string
		list    string
		want    string
		wantErr bool
	}{
		{"plain", "Server = https://a.example/archlinux/$repo/os/$arch\n", "https://a.example/archlinux", false},
		{"no spaces", "Server=https://a.example/$repo/os/$arch", "https://a.example", false},
		{"comments skipped", "# Server = https://x.example/$repo/os/$arch\n## Score\n\nServer = https://b.example/$repo/os/$arch\n", "https://b.example", false},
		{"file mirror skipped", "Server = file:///srv/mirror/$repo/os/$arch\nServer = http://c.example/arch/$repo/os/$arch\n", "http://c.example/arch", false},
		{"ftp skipped", "Server = ftp://f.example/$repo/os/$arch\nServer = https://d.example/$repo/os/$arch\n", "https://d.example", false},
		{"no $repo", "Server = https://e.example/arch/\n", "https://e.example/arch", false},
		{"only file", "Server = file:///srv/$repo/os/$arch\n", "", true},
		{"empty", "", "", true},
		{"all commented", "#Server = https://a.example/$repo/os/$arch\n", "", true},
		{"other keys", "Include = /x\n", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Root([]byte(tt.list))
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("got %q, %v; want %q (err %v)", got, err, tt.want, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrNoMirror) {
				t.Errorf("err = %v, want ErrNoMirror", err)
			}
		})
	}
}

func TestParseLastUpdate(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Time
		wantErr bool
	}{
		{"1761220800\n", time.Unix(1761220800, 0).UTC(), false},
		{"  1761220800  ", time.Unix(1761220800, 0).UTC(), false},
		{"", time.Time{}, true},
		{"yesterday", time.Time{}, true},
		{"-5", time.Time{}, true},
		{"0", time.Time{}, true},
		{"<html>" + strings.Repeat("x", 200), time.Time{}, true},
	}
	for _, tt := range tests {
		got, err := ParseLastUpdate([]byte(tt.in))
		if (err != nil) != tt.wantErr || !got.Equal(tt.want) {
			t.Errorf("ParseLastUpdate(%.20q) = %v, %v", tt.in, got, err)
		}
	}
}

func TestResultLag(t *testing.T) {
	base := time.Unix(1761220800, 0)
	tests := []struct {
		name        string
		mirror, ref time.Time
		want        time.Duration
	}{
		{"behind", base.Add(-5 * time.Hour), base, 5 * time.Hour},
		{"same", base, base, 0},
		{"ahead", base.Add(time.Hour), base, 0},
	}
	for _, tt := range tests {
		if got := (Result{Mirror: tt.mirror, Reference: tt.ref}).Lag(); got != tt.want {
			t.Errorf("%s: lag %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestCheck(t *testing.T) {
	const list = "Server = https://m.example/arch/$repo/os/$arch\n"
	fsys := system.NewFakeFS(map[string][]byte{MirrorlistPath: []byte(list)})
	f := system.NewFakeFetcher()
	f.Set("https://m.example/arch/lastupdate", system.FakeResponse{Body: []byte("1761200000\n")})
	f.Set(ReferenceRoot+"/lastupdate", system.FakeResponse{Body: []byte("1761220800\n")})

	res, err := Check(context.Background(), fsys, f, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Root != "https://m.example/arch" || res.Lag() != 20800*time.Second {
		t.Errorf("got %+v, lag %v", res, res.Lag())
	}
	for _, c := range f.Calls() {
		if !c.Fresh {
			t.Errorf("fresh not passed on: %+v", c)
		}
	}

	t.Run("no mirrorlist", func(t *testing.T) {
		_, err := Check(context.Background(), system.NewFakeFS(nil), f, false)
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("err = %v, want ErrNotExist", err)
		}
	})
	t.Run("mirror down", func(t *testing.T) {
		f := system.NewFakeFetcher()
		f.Set(ReferenceRoot+"/lastupdate", system.FakeResponse{Body: []byte("1761220800")})
		if _, err := Check(context.Background(), fsys, f, false); err == nil {
			t.Error("want an error when the mirror can't be fetched")
		}
	})
	t.Run("garbage", func(t *testing.T) {
		f := system.NewFakeFetcher()
		f.Set("https://m.example/arch/lastupdate", system.FakeResponse{Body: []byte("<html>404</html>")})
		_, err := Check(context.Background(), fsys, f, false)
		if err == nil || !strings.Contains(err.Error(), "lastupdate") {
			t.Errorf("err = %v", err)
		}
	})
}
