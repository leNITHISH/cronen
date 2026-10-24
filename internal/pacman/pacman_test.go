package pacman

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/system"
)

func TestParseCheckupdates(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []Update
		wantErr bool
	}{
		{name: "empty", in: ""},
		{
			name: "blank lines and stray whitespace",
			in:   "\n  foo 1.0-1 -> 1.1-1  \n\nbar 1:2-1 -> 1:3-1\n",
			want: []Update{
				{Name: "foo", OldVersion: "1.0-1", NewVersion: "1.1-1"},
				{Name: "bar", OldVersion: "1:2-1", NewVersion: "1:3-1"},
			},
		},
		{name: "missing arrow", in: "foo 1.0-1 1.1-1\n", wantErr: true},
		{name: "too few fields", in: "foo 1.0-1\n", wantErr: true},
		{name: "trailing junk", in: "foo 1.0-1 -> 1.1-1 [ignored]\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCheckupdates([]byte(tt.in))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCheckUpdates(t *testing.T) {
	ctx := context.Background()

	t.Run("updates", func(t *testing.T) {
		r := system.NewFakeRunner()
		r.Set("checkupdates", fixtures.CheckupdatesResult())
		got, err := CheckUpdates(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 16 {
			t.Fatalf("got %d updates, want 16", len(got))
		}
		byName := map[string]Update{}
		for _, u := range got {
			byName[u.Name] = u
		}
		for _, want := range []Update{
			{Name: "firefox", OldVersion: "144.0-1", NewVersion: "144.0.1-1"},
			{Name: "gnupg", OldVersion: "2.4.8-1", NewVersion: "1:2.4.8-2"},
			{Name: "glibc", OldVersion: "2.42+r17+g6f0cf3d0b5b0-1", NewVersion: "2.42+r33+gc7ce5b4c0b7f-1"},
			{Name: "nvidia-utils", OldVersion: "580.95.05-1", NewVersion: "590.44.01-1"},
		} {
			if byName[want.Name] != want {
				t.Errorf("%s = %+v, want %+v", want.Name, byName[want.Name], want)
			}
		}
	})

	t.Run("exit 0 without output", func(t *testing.T) {
		r := system.NewFakeRunner()
		r.Set("checkupdates", system.FakeResult{})
		got, err := CheckUpdates(ctx, r)
		if err != nil || len(got) != 0 {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("no updates (exit 2)", func(t *testing.T) {
		r := system.NewFakeRunner()
		r.Set("checkupdates", fixtures.CheckupdatesNoneResult())
		got, err := CheckUpdates(ctx, r)
		if err != nil || len(got) != 0 {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("error (exit 1)", func(t *testing.T) {
		r := system.NewFakeRunner()
		r.Set("checkupdates", fixtures.CheckupdatesErrorResult())
		_, err := CheckUpdates(ctx, r)
		var exitErr *system.ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != 1 {
			t.Fatalf("err = %v, want exit 1 *ExitError", err)
		}
		if !strings.Contains(err.Error(), "Cannot fetch updates") {
			t.Fatalf("err %q should carry stderr", err)
		}
	})

	t.Run("not installed", func(t *testing.T) {
		r := system.NewFakeRunner()
		r.Set("checkupdates", system.FakeResult{Err: system.ErrCommandNotFound})
		_, err := CheckUpdates(ctx, r)
		if !errors.Is(err, ErrCheckupdatesNotFound) {
			t.Fatalf("err = %v, want ErrCheckupdatesNotFound", err)
		}
		// The message is shown as is, so it must be one short line.
		if got, want := err.Error(), "checkupdates not found — install pacman-contrib"; got != want {
			t.Fatalf("err = %q, want %q", got, want)
		}
	})

	t.Run("not runnable", func(t *testing.T) {
		r := system.NewFakeRunner()
		r.Set("checkupdates", system.FakeResult{Err: errors.New("permission denied")})
		_, err := CheckUpdates(ctx, r)
		if err == nil || errors.Is(err, ErrCheckupdatesNotFound) {
			t.Fatalf("err = %v, want a plain error", err)
		}
	})

	t.Run("malformed output", func(t *testing.T) {
		r := system.NewFakeRunner()
		r.Set("checkupdates", system.FakeResult{Stdout: []byte("garbage\n")})
		if _, err := CheckUpdates(ctx, r); err == nil {
			t.Fatal("expected a parse error")
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := CheckUpdates(ctx, fixtures.Runner())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	})
}

func TestParseQm(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []Package
		wantErr bool
	}{
		{name: "empty", in: ""},
		{
			name: "blank lines",
			in:   "\nfoo 1.0-1\n\n  bar 1:2-3  \n",
			want: []Package{{Name: "foo", Version: "1.0-1"}, {Name: "bar", Version: "1:2-3"}},
		},
		{name: "missing version", in: "foo\n", wantErr: true},
		{name: "extra field", in: "foo 1.0-1 extra\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseQm([]byte(tt.in))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestForeignPackages(t *testing.T) {
	ctx := context.Background()

	t.Run("fixture", func(t *testing.T) {
		got, err := ForeignPackages(ctx, fixtures.Runner())
		if err != nil {
			t.Fatal(err)
		}
		want := []Package{
			{Name: "gnome-shell-extension-legacy", Version: "3.2.0-1"},
			{Name: "neovim-git", Version: "0.12.0.r1234.gabcdef0-1"},
			{Name: "paru", Version: "2.1.0-1"},
			{Name: "paru-debug", Version: "2.1.0-1"},
			{Name: "python-pywal", Version: "3.3.0-9"},
			{Name: "spotify", Version: "1:1.2.74.477-1"},
			{Name: "ttf-ms-fonts", Version: "2.0-12"},
			{Name: "visual-studio-code-bin", Version: "1.105.0-1"},
			{Name: "zstd-debug", Version: "1.5.7-2"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("none installed (exit 1, silent)", func(t *testing.T) {
		r := system.NewFakeRunner()
		r.Set("pacman -Qm", system.FakeResult{Code: 1})
		got, err := ForeignPackages(ctx, r)
		if err != nil || len(got) != 0 {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("real error (exit 1 with stderr)", func(t *testing.T) {
		r := system.NewFakeRunner()
		r.Set("pacman -Qm", system.FakeResult{Code: 1, Stderr: []byte("error: could not open database\n")})
		_, err := ForeignPackages(ctx, r)
		var exitErr *system.ExitError
		if !errors.As(err, &exitErr) || !strings.Contains(err.Error(), "could not open database") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("other exit code", func(t *testing.T) {
		r := system.NewFakeRunner()
		r.Set("pacman -Qm", system.FakeResult{Code: 3})
		if _, err := ForeignPackages(ctx, r); err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("not runnable", func(t *testing.T) {
		r := system.NewFakeRunner()
		r.Set("pacman -Qm", system.FakeResult{Err: errors.New("executable file not found")})
		if _, err := ForeignPackages(ctx, r); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestInstalledNames(t *testing.T) {
	ctx := context.Background()
	got, err := InstalledNames(ctx, fixtures.Runner())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"zstd", "paru", "paru-debug", "linux"} {
		if !got[name] {
			t.Errorf("%s missing from pacman -Qq", name)
		}
	}
	if got[""] {
		t.Error("blank line parsed as a package")
	}

	r := system.NewFakeRunner()
	r.Set("pacman -Qq", system.FakeResult{Code: 1, Stderr: []byte("error: could not open database\n")})
	if _, err := InstalledNames(ctx, r); err == nil {
		t.Fatal("expected an error")
	}
}

func TestVercmpFixtureTable(t *testing.T) {
	ctx := context.Background()
	r := fixtures.Runner()
	for _, c := range fixtures.VercmpTable() {
		for _, tc := range []struct {
			a, b string
			want int
		}{{c.A, c.B, c.Result}, {c.B, c.A, -c.Result}} {
			got, err := Vercmp(ctx, r, tc.a, tc.b)
			if err != nil {
				t.Errorf("Vercmp(%s, %s): %v", tc.a, tc.b, err)
				continue
			}
			if got != tc.want {
				t.Errorf("Vercmp(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		}
	}
}

func TestVercmpOutput(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		res     system.FakeResult
		want    int
		wantErr bool
	}{
		{name: "negative is normalized", res: system.FakeResult{Stdout: []byte("-5\n")}, want: -1},
		{name: "positive is normalized", res: system.FakeResult{Stdout: []byte("  7 \n")}, want: 1},
		{name: "zero", res: system.FakeResult{Stdout: []byte("0")}, want: 0},
		{name: "garbage", res: system.FakeResult{Stdout: []byte("what\n")}, wantErr: true},
		{name: "empty", res: system.FakeResult{}, wantErr: true},
		{name: "not runnable", res: system.FakeResult{Err: errors.New("not found")}, wantErr: true},
		{name: "non-zero exit", res: system.FakeResult{Stdout: []byte("0\n"), Code: 1}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := system.NewFakeRunner()
			r.Set("vercmp a b", tt.res)
			got, err := Vercmp(ctx, r, "a", "b")
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}
