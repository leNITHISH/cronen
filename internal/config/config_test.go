package config

import (
	"errors"
	"io/fs"
	"path"
	"reflect"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/system"
)

func TestResolvePaths(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    Paths
		wantErr bool
	}{
		{
			name: "defaults under HOME",
			env:  map[string]string{"HOME": "/home/u"},
			want: Paths{
				Config:   "/home/u/.config/cronen/config.toml",
				State:    "/home/u/.local/state/cronen/state.json",
				CacheDir: "/home/u/.cache/cronen",
			},
		},
		{
			name: "XDG variables",
			env: map[string]string{
				"HOME":            "/home/u",
				"XDG_CONFIG_HOME": "/cfg",
				"XDG_STATE_HOME":  "/st/",
				"XDG_CACHE_HOME":  "/c",
			},
			want: Paths{
				Config:   "/cfg/cronen/config.toml",
				State:    "/st/cronen/state.json",
				CacheDir: "/c/cronen",
			},
		},
		{
			name: "relative XDG values are ignored",
			env:  map[string]string{"HOME": "/home/u", "XDG_CONFIG_HOME": "rel/cfg", "XDG_CACHE_HOME": "./c"},
			want: Paths{
				Config:   "/home/u/.config/cronen/config.toml",
				State:    "/home/u/.local/state/cronen/state.json",
				CacheDir: "/home/u/.cache/cronen",
			},
		},
		{
			name: "no HOME but all XDG set",
			env:  map[string]string{"XDG_CONFIG_HOME": "/cfg", "XDG_STATE_HOME": "/st", "XDG_CACHE_HOME": "/c"},
			want: Paths{Config: "/cfg/cronen/config.toml", State: "/st/cronen/state.json", CacheDir: "/c/cronen"},
		},
		{name: "no HOME", env: map[string]string{}, wantErr: true},
		{name: "relative HOME", env: map[string]string{"HOME": "home"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolvePaths(func(k string) string { return tt.env[k] })
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParse(t *testing.T) {
	def := Default()
	tests := []struct {
		name    string
		toml    string
		want    Config
		wantErr string // substring; empty means no error
	}{
		{name: "empty", toml: "", want: def},
		{name: "comments only", toml: "# nothing here\n", want: def},
		{
			name: "full example from PLAN",
			toml: `
aur = false
aur_helper = "paru"
upgrade_command = ""
watchlist = ["linux*", "nvidia*", "mesa", "glibc", "systemd"]
watchlist_mode = "replace"
news_days = 14
ignore_major = ["chromium", "electron*"]
show_debug = true
`,
			want: Config{
				AUR:           false,
				AURHelper:     "paru",
				Watchlist:     []string{"linux*", "nvidia*", "mesa", "glibc", "systemd"},
				WatchlistMode: WatchlistReplace,
				NewsDays:      14,
				IgnoreMajor:   []string{"chromium", "electron*"},
				ShowDebug:     true,
			},
		},
		{
			name: "absent keys keep defaults",
			toml: `aur_helper = "yay"`,
			want: Config{AUR: true, AURHelper: "yay", WatchlistMode: WatchlistExtend, NewsDays: 30},
		},
		{name: "syntax error", toml: "aur = \n[[[", want: def, wantErr: "toml"},
		{name: "binary garbage", toml: "\x00\xff\xfe garbage", want: def, wantErr: "toml"},
		{name: "wrong type", toml: `news_days = "x"`, want: def, wantErr: "news_days"},
		{name: "wrong type for list", toml: `watchlist = "linux"`, want: def, wantErr: "watchlist"},
		{
			name:    "unknown key keeps valid settings",
			toml:    "watchlst = [\"foo\"]\nnews_days = 7\n",
			want:    Config{AUR: true, WatchlistMode: WatchlistExtend, NewsDays: 7},
			wantErr: "unknown keys ignored: watchlst",
		},
		{
			name:    "invalid aur_helper",
			toml:    "aur_helper = \"pikaur\"\nnews_days = 7\n",
			want:    Config{AUR: true, WatchlistMode: WatchlistExtend, NewsDays: 7},
			wantErr: "aur_helper",
		},
		{
			name:    "invalid watchlist_mode",
			toml:    `watchlist_mode = "merge"`,
			want:    def,
			wantErr: "watchlist_mode",
		},
		{name: "zero news_days", toml: "news_days = 0", want: def, wantErr: "news_days"},
		{name: "negative news_days", toml: "news_days = -3", want: def, wantErr: "news_days"},
		{
			name:    "bad glob dropped, good kept",
			toml:    `watchlist = ["[a-", "firefox", ""]`,
			want:    Config{AUR: true, Watchlist: []string{"firefox"}, WatchlistMode: WatchlistExtend, NewsDays: 30},
			wantErr: `"[a-"`,
		},
		{
			name:    "bad ignore_major glob dropped, good kept",
			toml:    `ignore_major = ["polkit", "[x"]`,
			want:    Config{AUR: true, IgnoreMajor: []string{"polkit"}, WatchlistMode: WatchlistExtend, NewsDays: 30},
			wantErr: `ignore_major pattern "[x"`,
		},
		{name: "wrong type for show_debug", toml: `show_debug = "yes"`, want: def, wantErr: "show_debug"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse([]byte(tt.toml))
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestBadGlobIsBad(t *testing.T) {
	// The watchlist validation relies on path.Match rejecting malformed
	// patterns even when they don't match the name.
	if _, err := path.Match("[a-", ""); !errors.Is(err, path.ErrBadPattern) {
		t.Fatalf("path.Match(\"[a-\") = %v, want ErrBadPattern", err)
	}
}

func TestLoad(t *testing.T) {
	const p = "/home/u/.config/cronen/config.toml"

	t.Run("missing file", func(t *testing.T) {
		got, err := Load(system.NewFakeFS(nil), p)
		if err != nil || !reflect.DeepEqual(got, Default()) {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("unreadable file", func(t *testing.T) {
		fsys := system.NewFakeFS(map[string][]byte{p: []byte("aur = false")})
		fsys.ReadErr = fs.ErrPermission
		got, err := Load(fsys, p)
		if !errors.Is(err, fs.ErrPermission) || !reflect.DeepEqual(got, Default()) {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("valid file", func(t *testing.T) {
		fsys := system.NewFakeFS(map[string][]byte{p: []byte("aur = false")})
		got, err := Load(fsys, p)
		if err != nil || got.AUR {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("corrupt file names path", func(t *testing.T) {
		fsys := system.NewFakeFS(map[string][]byte{p: []byte("[[[")})
		got, err := Load(fsys, p)
		if err == nil || !strings.Contains(err.Error(), p) || !reflect.DeepEqual(got, Default()) {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
}

func TestEffectiveWatchlist(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want []string
	}{
		{name: "default", cfg: Default(), want: DefaultWatchlist},
		{
			name: "extend dedups",
			cfg:  Config{WatchlistMode: WatchlistExtend, Watchlist: []string{"mesa", "firefox"}},
			want: append(append([]string{}, DefaultWatchlist...), "firefox"),
		},
		{
			name: "replace",
			cfg:  Config{WatchlistMode: WatchlistReplace, Watchlist: []string{"firefox", "firefox"}},
			want: []string{"firefox"},
		},
		{name: "replace with nothing", cfg: Config{WatchlistMode: WatchlistReplace}, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.EffectiveWatchlist(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}

	// The result must not alias DefaultWatchlist.
	got := Default().EffectiveWatchlist()
	got[0] = "changed"
	if DefaultWatchlist[0] == "changed" {
		t.Fatal("EffectiveWatchlist aliases DefaultWatchlist")
	}
}

func TestUpgradeArgv(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want []string
	}{
		{name: "default", cfg: Default(), want: []string{"sudo", "pacman", "-Syu"}},
		{name: "paru", cfg: Config{AURHelper: "paru"}, want: []string{"paru", "-Syu"}},
		{name: "yay", cfg: Config{AURHelper: "yay"}, want: []string{"yay", "-Syu"}},
		{
			name: "override wins",
			cfg:  Config{AURHelper: "paru", UpgradeCommand: "  doas  pacman -Syu "},
			want: []string{"doas", "pacman", "-Syu"},
		},
		{name: "blank override ignored", cfg: Config{UpgradeCommand: "   "}, want: []string{"sudo", "pacman", "-Syu"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.UpgradeArgv(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
