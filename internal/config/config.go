package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/leNITHISH/cronen/internal/snapshot"
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
	// PackageBrowser enables the Packages screen: every installed package,
	// read once with `pacman -Qi`. With it off, cronen doesn't run that
	// command.
	PackageBrowser bool `toml:"package_browser"`
	// AURReview is AURReviewLazy or AURReviewEager: whether the PKGBUILD
	// diff of a pending AUR update is fetched when it is opened, or for
	// every pending AUR update.
	AURReview string `toml:"aur_review"`

	// PrivilegeCommand is how write actions gain root: "sudo", "doas" or
	// "run0".
	PrivilegeCommand string `toml:"privilege_command"`
	// Protected holds package globs that can't be removed with a single
	// y: the confirm modal asks for the package name. See
	// EffectiveProtected.
	Protected []string `toml:"protected"`
	// ProtectedMode is WatchlistExtend or WatchlistReplace, like
	// watchlist_mode.
	ProtectedMode string `toml:"protected_mode"`
	// ExtraCacheDirs are globs of package cache directories besides
	// pacman's CacheDir, for example ~/.cache/paru/clone/*.
	ExtraCacheDirs []string `toml:"extra_cache_dirs"`
	// PacdiffCommand, if set, replaces "<privilege_command> pacdiff". It
	// is split on whitespace; quoting is not supported.
	PacdiffCommand string `toml:"pacdiff_command"`
	// Housekeeping enables the Housekeeping screen's own data sources:
	// the .pacnew diffs, the package cache size and the failed units.
	Housekeeping bool `toml:"housekeeping"`
	// History enables the History screen's data sources: pacman.log read
	// into transactions, and the package cache listing that shows which old
	// versions can still be installed. Downgrades need the cache listing,
	// so with it off the D key is unavailable.
	History bool `toml:"history"`

	// MeteredWarnMiB raises a preflight warning when the connection is
	// metered and the download is bigger than this. 0 turns the metered
	// check off, and with it the nmcli call.
	MeteredWarnMiB int `toml:"metered_warn_mib"`
	// PrefetchCommand, if set, replaces "checkupdates -d" for the F key.
	// It is split on whitespace; quoting is not supported, and it is run
	// exactly as written (add sudo yourself if it needs it).
	PrefetchCommand string `toml:"prefetch_command"`
	// ArchiveInstall turns on the install helper's option B (experimental):
	// installing a package from the Arch Linux Archive snapshot of the day of
	// the last full upgrade. It is off unless set to true.
	ArchiveInstall bool `toml:"archive_install"`

	// PartialCheck enables the partial-upgrade detector: `pacman -Qu`
	// against the real database (a database synced without upgrading), and
	// the History and Packages marks for `-Sy X` transactions. With it off,
	// cronen runs neither.
	PartialCheck bool `toml:"partial_check"`
	// LinkageSkip are extra directory globs the broken-linkage scan leaves
	// out, on top of the built-in list of dlopen-only plugin directories
	// (see internal/linkage).
	LinkageSkip []string `toml:"linkage_skip"`

	// Snapshots is the snapshot helper's switch: with it off cronen looks
	// for no snapshot tool, shows no snapshot UI and reports no snapshot
	// age.
	Snapshots bool `toml:"snapshots"`
	// SnapshotTool is "", "timeshift" or "snapper"; "" uses whichever is
	// installed, Timeshift first.
	SnapshotTool string `toml:"snapshot_tool"`
	// SnapshotBeforeUpgrade is "ask" (the box starts ticked when a tool is
	// found and no pacman hook already takes snapshots), "always" or
	// "never".
	SnapshotBeforeUpgrade string `toml:"snapshot_before_upgrade"`
	// SnapshotMaxAgeDays is how old the newest known snapshot may be
	// before preflight warns.
	SnapshotMaxAgeDays int `toml:"snapshot_max_age_days"`
	// SnapshotListSudo allows `sudo -n timeshift --list`, the one
	// read-only command cronen runs with a privilege command. Without it
	// only the snapshots cronen itself recorded are listed for Timeshift.
	SnapshotListSudo bool `toml:"snapshot_list_sudo"`
	// SnapperConfig is the snapper config cronen takes snapshots of.
	SnapperConfig string `toml:"snapper_config"`
}

// Values of Config.SnapshotBeforeUpgrade.
const (
	SnapshotAsk    = "ask"
	SnapshotAlways = "always"
	SnapshotNever  = "never"
)

// Privilege commands cronen knows how to prefix a write action with.
var privilegeCommands = []string{"sudo", "doas", "run0"}

// DefaultProtected is the set of package globs that need the name typed
// before they are removed by default.
var DefaultProtected = []string{
	"base", "linux*", "glibc", "systemd", "pacman", "sudo", "grub", "systemd-boot*",
}

// Values of Config.AURReview.
const (
	AURReviewLazy  = "lazy"
	AURReviewEager = "eager"
)

// Default returns the configuration used when there is no config file.
func Default() Config {
	return Config{
		AUR:            true,
		Preview:        true,
		Security:       true,
		KnownIssues:    true,
		Preflight:      true,
		PackageBrowser: true,
		MirrorCheck:    true,
		RebuildScanELF: true,
		WatchlistMode:  WatchlistExtend,
		NewsDays:       30,
		MirrorLagHours: 6,
		AURReview:      AURReviewLazy,

		PrivilegeCommand: "sudo",
		ProtectedMode:    WatchlistExtend,
		Housekeeping:     true,
		History:          true,
		MeteredWarnMiB:   100,
		PartialCheck:     true,

		Snapshots:             true,
		SnapshotBeforeUpgrade: SnapshotAsk,
		SnapshotMaxAgeDays:    7,
		SnapperConfig:         "root",
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
	switch cfg.AURReview {
	case AURReviewLazy, AURReviewEager:
	default:
		errs = append(errs, fmt.Errorf("aur_review %q: want %q or %q", cfg.AURReview, AURReviewLazy, AURReviewEager))
		cfg.AURReview = def.AURReview
	}
	if !slices.Contains(privilegeCommands, cfg.PrivilegeCommand) {
		errs = append(errs, fmt.Errorf("privilege_command %q: want \"sudo\", \"doas\" or \"run0\"", cfg.PrivilegeCommand))
		cfg.PrivilegeCommand = def.PrivilegeCommand
	}
	switch cfg.ProtectedMode {
	case WatchlistExtend, WatchlistReplace:
	default:
		errs = append(errs, fmt.Errorf("protected_mode %q: want %q or %q", cfg.ProtectedMode, WatchlistExtend, WatchlistReplace))
		cfg.ProtectedMode = def.ProtectedMode
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

	switch cfg.SnapshotTool {
	case "", string(snapshot.Timeshift), string(snapshot.Snapper):
	default:
		errs = append(errs, fmt.Errorf("snapshot_tool %q: want \"\", \"timeshift\" or \"snapper\"", cfg.SnapshotTool))
		cfg.SnapshotTool = def.SnapshotTool
	}
	switch cfg.SnapshotBeforeUpgrade {
	case SnapshotAsk, SnapshotAlways, SnapshotNever:
	default:
		errs = append(errs, fmt.Errorf("snapshot_before_upgrade %q: want %q, %q or %q", cfg.SnapshotBeforeUpgrade, SnapshotAsk, SnapshotAlways, SnapshotNever))
		cfg.SnapshotBeforeUpgrade = def.SnapshotBeforeUpgrade
	}
	if cfg.SnapshotMaxAgeDays <= 0 {
		errs = append(errs, fmt.Errorf("snapshot_max_age_days %d: must be positive", cfg.SnapshotMaxAgeDays))
		cfg.SnapshotMaxAgeDays = def.SnapshotMaxAgeDays
	}
	if !snapshot.ValidConfig(cfg.SnapperConfig) {
		errs = append(errs, fmt.Errorf("snapper_config %q: not a usable snapper config name", cfg.SnapperConfig))
		cfg.SnapperConfig = def.SnapperConfig
	}

	if cfg.MeteredWarnMiB < 0 {
		errs = append(errs, fmt.Errorf("metered_warn_mib %d: must not be negative", cfg.MeteredWarnMiB))
		cfg.MeteredWarnMiB = def.MeteredWarnMiB
	}

	var err1, err2, err3, err4, err5 error
	cfg.Watchlist, err1 = validGlobs("watchlist", cfg.Watchlist)
	cfg.IgnoreMajor, err2 = validGlobs("ignore_major", cfg.IgnoreMajor)
	cfg.Protected, err3 = validGlobs("protected", cfg.Protected)
	cfg.ExtraCacheDirs, err4 = validGlobs("extra_cache_dirs", cfg.ExtraCacheDirs)
	cfg.LinkageSkip, err5 = validGlobs("linkage_skip", cfg.LinkageSkip)
	errs = append(errs, err1, err2, err3, err4, err5)

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

	return dedupe(all)
}

// EffectiveProtected returns the protected package globs: the user's
// patterns added to DefaultProtected in extend mode, or just the user's
// patterns in replace mode. Duplicates are removed.
func (c Config) EffectiveProtected() []string {
	var all []string
	if c.ProtectedMode != WatchlistReplace {
		all = append(all, DefaultProtected...)
	}
	all = append(all, c.Protected...)
	return dedupe(all)
}

func dedupe(all []string) []string {
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

// PrivilegeArgv is the command that prefixes a write action: the
// privilege_command, "sudo" if it's unset.
func (c Config) PrivilegeArgv() []string {
	if c.PrivilegeCommand == "" {
		return []string{"sudo"}
	}
	return []string{c.PrivilegeCommand}
}

// PacdiffArgv is the command that merges .pacnew files:
// pacdiff_command if set, else <privilege_command> pacdiff.
func (c Config) PacdiffArgv() []string {
	if fields := strings.Fields(c.PacdiffCommand); len(fields) > 0 {
		return fields
	}
	return append(c.PrivilegeArgv(), "pacdiff")
}

// UpgradeArgv returns the command line used to upgrade the system:
// upgrade_command if set, else the AUR helper's -Syu, else
// <privilege_command> pacman -Syu.
func (c Config) UpgradeArgv() []string {
	if fields := strings.Fields(c.UpgradeCommand); len(fields) > 0 {
		return fields
	}
	if c.AURHelper != "" {
		return []string{c.AURHelper, "-Syu"}
	}
	return append(c.PrivilegeArgv(), "pacman", "-Syu")
}
