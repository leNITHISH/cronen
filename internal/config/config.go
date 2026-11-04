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
	// Preview enables the transaction preview: what -Syu would pull in,
	// replace and download, from the database checkupdates synced.
	Preview bool `toml:"preview"`
	// FlagFreshHours flags updates whose new build is younger than this
	// many hours. 0 never flags.
	FlagFreshHours int `toml:"flag_fresh_hours"`
	// Preflight enables the preflight checks: free space, /boot, the
	// database lock, a pending reboot and the keyring.
	Preflight bool `toml:"preflight"`
	// MirrorCheck compares the first mirror in the mirrorlist with the
	// reference mirror, as part of the preflight checks.
	MirrorCheck bool `toml:"mirror_check"`
	// MirrorLagHours is how far behind the reference mirror counts as
	// stale.
	MirrorLagHours int `toml:"mirror_lag_hours"`
	// RebuildScanELF also looks inside the ELF files of foreign packages
	// for libraries an upgrade drops. It is the slow part of rebuild
	// detection, so its results are cached.
	RebuildScanELF bool `toml:"rebuild_scan_elf"`
	// Security enables the security.archlinux.org advisories: updates that
	// fix one, and installed packages that are vulnerable with no fix yet.
	Security bool `toml:"security"`
	// KnownIssues enables the GitLab lookup of open issues for pending repo
	// updates. With it off, cronen makes no GitLab requests at all.
	KnownIssues bool `toml:"known_issues"`
}

// Default returns the configuration used when there is no config file.
func Default() Config {
	return Config{
		AUR:            true,
		Preview:        true,
		Security:       true,
		KnownIssues:    true,
		Preflight:      true,
		MirrorCheck:    true,
		RebuildScanELF: true,
		WatchlistMode:  WatchlistExtend,
		NewsDays:       30,
		MirrorLagHours: 6,
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

	if cfg.FlagFreshHours < 0 {
		errs = append(errs, fmt.Errorf("flag_fresh_hours %d: must not be negative", cfg.FlagFreshHours))
		cfg.FlagFreshHours = def.FlagFreshHours
	}

	if cfg.MirrorLagHours <= 0 {
		errs = append(errs, fmt.Errorf("mirror_lag_hours %d: must be positive", cfg.MirrorLagHours))
		cfg.MirrorLagHours = def.MirrorLagHours
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
