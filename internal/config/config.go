package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/leNITHISH/cronen/internal/system"
)

// Watchlist modes.
const (
	WatchlistExtend  = "extend"
	WatchlistReplace = "replace"
)

// DefaultWatchlist is the set of package globs flagged as risky by default.
var DefaultWatchlist = []string{
	"linux*", "nvidia*", "mesa", "vulkan-*", "glibc", "systemd",
	"grub", "pacman", "openssl", "python", "gcc*",
}

// Config is the user configuration from config.toml.
type Config struct {
	// AUR enables checking AUR packages for updates.
	AUR bool `toml:"aur"`
	// AURHelper is "", "paru" or "yay".
	AURHelper string `toml:"aur_helper"`
	// UpgradeCommand, if set, overrides the upgrade command entirely. It
	// is split on whitespace; quoting is not supported.
	UpgradeCommand string `toml:"upgrade_command"`
	// Watchlist holds the user's package globs. See EffectiveWatchlist.
	Watchlist []string `toml:"watchlist"`
	// WatchlistMode is WatchlistExtend or WatchlistReplace.
	WatchlistMode string `toml:"watchlist_mode"`
	// NewsDays is how far back news is considered when the last upgrade
	// time is unknown.
	NewsDays int `toml:"news_days"`
	// IgnoreMajor holds package globs whose major version bumps are never
	// flagged.
	IgnoreMajor []string `toml:"ignore_major"`
	// ShowDebug lists makepkg debug splits (<name>-debug) among the AUR
	// packages. They are hidden by default.
	ShowDebug bool `toml:"show_debug"`
}

// Default returns the configuration used when there is no config file.
func Default() Config {
	return Config{
		AUR:           true,
		WatchlistMode: WatchlistExtend,
		NewsDays:      30,
	}
}

// Load reads the config file at path. It always returns a usable Config:
// a missing file yields Default() and a nil error. Otherwise a non-nil
// error describes what was wrong; if the file can't be read or parsed the
// Config is Default(), and if individual settings are invalid or unknown
// only those fall back to their defaults.
func Load(fsys system.FS, path string) (Config, error) {
	data, err := fsys.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Default(), fmt.Errorf("reading config: %w", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return cfg, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

// Parse parses config TOML with the same fallback rules as Load.
func Parse(data []byte) (Config, error) {
	cfg := Default()
	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return Default(), err
	}

	var errs []error
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		errs = append(errs, fmt.Errorf("unknown keys ignored: %s", strings.Join(keys, ", ")))
	}

	def := Default()
	switch cfg.AURHelper {
	case "", "paru", "yay":
	default:
		errs = append(errs, fmt.Errorf("aur_helper %q: want \"\", \"paru\" or \"yay\"", cfg.AURHelper))
		cfg.AURHelper = def.AURHelper
	}
	switch cfg.WatchlistMode {
	case WatchlistExtend, WatchlistReplace:
	default:
		errs = append(errs, fmt.Errorf("watchlist_mode %q: want %q or %q", cfg.WatchlistMode, WatchlistExtend, WatchlistReplace))
		cfg.WatchlistMode = def.WatchlistMode
	}
	if cfg.NewsDays <= 0 {
		errs = append(errs, fmt.Errorf("news_days %d: must be positive", cfg.NewsDays))
		cfg.NewsDays = def.NewsDays
	}

	var err1, err2 error
	cfg.Watchlist, err1 = validGlobs("watchlist", cfg.Watchlist)
	cfg.IgnoreMajor, err2 = validGlobs("ignore_major", cfg.IgnoreMajor)
	errs = append(errs, err1, err2)

	return cfg, errors.Join(errs...)
}

// validGlobs returns the well-formed, non-blank patterns, and an error
// naming the others.
func validGlobs(key string, patterns []string) ([]string, error) {
	var ok []string
	var errs []error
	for _, p := range patterns {
		if _, err := path.Match(p, ""); err != nil || strings.TrimSpace(p) == "" {
			errs = append(errs, fmt.Errorf("%s pattern %q is invalid and was ignored", key, p))
			continue
		}
		ok = append(ok, p)
	}
	return ok, errors.Join(errs...)
}

// EffectiveWatchlist returns the watchlist globs to apply: the user's
// patterns added to DefaultWatchlist in extend mode, or just the user's
// patterns in replace mode. Duplicates are removed.
func (c Config) EffectiveWatchlist() []string {
	var all []string
	if c.WatchlistMode != WatchlistReplace {
		all = append(all, DefaultWatchlist...)
	}
	all = append(all, c.Watchlist...)

	seen := map[string]bool{}
	out := []string{}
	for _, p := range all {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// UpgradeArgv returns the command line used to upgrade the system:
// upgrade_command if set, else the AUR helper's -Syu, else
// sudo pacman -Syu.
func (c Config) UpgradeArgv() []string {
	if fields := strings.Fields(c.UpgradeCommand); len(fields) > 0 {
		return fields
	}
	if c.AURHelper != "" {
		return []string{c.AURHelper, "-Syu"}
	}
	return []string{"sudo", "pacman", "-Syu"}
}
