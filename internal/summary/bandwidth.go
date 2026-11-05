package summary

import (
	"context"
	"strings"
	"time"

	"github.com/leNITHISH/cronen/internal/bandwidth"
	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/ops"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/system"
)

// RoutePath is the kernel routing table, where the default route is read.
const RoutePath = "/proc/net/route"

// MeteredInfo is what NetworkManager says about the connection carrying the
// default route.
type MeteredInfo struct {
	Metered bool
	Device  string
}

// LoadMetered asks NetworkManager whether the connection carrying the
// default route is metered: the device comes from /proc/net/route, and
// `nmcli -t -g GENERAL.METERED device show <dev>` says the rest. Anything
// that goes wrong (no default route, no nmcli, NetworkManager not running)
// means "not known to be metered": the check is skipped without a word,
// because most systems that fail it simply don't use NetworkManager.
//
// Needs checking on Arch: the nmcli output format and /proc/net/route. See
// the M16 pull request.
func LoadMetered(ctx context.Context, r system.Runner, fsys system.FS) MeteredInfo {
	route, err := fsys.ReadFile(RoutePath)
	if err != nil {
		return MeteredInfo{}
	}
	dev, ok := bandwidth.DefaultInterface(route)
	if !ok {
		return MeteredInfo{}
	}
	out, _, err := r.Run(ctx, "nmcli", "-t", "-g", "GENERAL.METERED", "device", "show", dev)
	if err != nil {
		return MeteredInfo{Device: dev}
	}
	return MeteredInfo{Metered: bandwidth.ParseMetered(out) == bandwidth.MeteredYes, Device: dev}
}

// MeteredEnabled reports whether the metered check runs at all.
func MeteredEnabled(cfg config.Config) bool {
	return cfg.MeteredWarnMiB > 0
}

// BandwidthInfo is what the upgrade costs in bytes and time, as the header
// shows it.
type BandwidthInfo struct {
	// DaysBehind is the days since the last full upgrade; DaysKnown is
	// false when that time is unknown.
	DaysBehind int
	DaysKnown  bool
	// Packages is how many packages the upgrade installs.
	Packages int
	// Remaining is what is still to download, Prefetched what is already in
	// the cache, and Total both.
	Remaining, Prefetched, Total int64
	// Trend is the growth of the pending download in bytes per week, when
	// TrendOK.
	Trend   int64
	TrendOK bool
	// Estimate is the time the upgrade is predicted to take.
	Estimate bandwidth.Estimate
}

// Bandwidth works out the cost of the previewed upgrade. last is the time
// of the last full upgrade (zero if unknown).
func Bandwidth(tx pacman.Transaction, last, now time.Time, st config.State) BandwidthInfo {
	b := BandwidthInfo{
		Packages:   tx.Packages(),
		Remaining:  tx.DownloadBytes,
		Prefetched: tx.PrefetchedBytes,
		Total:      tx.TotalBytes(),
	}
	b.DaysBehind, b.DaysKnown = bandwidth.Behind(last, now)
	b.Trend, b.TrendOK = bandwidth.Trend(st.PendingSizes())
	b.Estimate = bandwidth.EstimateTime(st.PrefetchSamples(), st.UpgradeSamples(), b.Remaining, b.Packages)
	return b
}

// Shown reports whether there is an upgrade to describe.
func (b BandwidthInfo) Shown() bool { return b.Packages > 0 }

// Parts are the pieces of the header line, most important first:
// "47 days behind", "312 packages", "1.8 GiB", "+~250 MiB/week", and the
// time estimate.
func (b BandwidthInfo) Parts() []string {
	if !b.Shown() {
		return nil
	}
	var parts []string
	if b.DaysKnown && b.DaysBehind >= 1 {
		parts = append(parts, plural(b.DaysBehind, "day", "days")+" behind")
	}
	parts = append(parts, plural(b.Packages, "package", "packages"))
	if b.Prefetched > 0 {
		parts = append(parts, sizeOf(b.Prefetched, b.Total)+" already downloaded")
	} else {
		parts = append(parts, pacman.FormatSize(b.Total))
	}
	if b.TrendOK {
		parts = append(parts, "+~"+pacman.FormatSize(b.Trend)+"/week")
	}
	return append(parts, b.Estimate.Text())
}

// Line is Parts on one line.
func (b BandwidthInfo) Line() string {
	return strings.Join(b.Parts(), " · ")
}

// sizeOf is "1.7 of 1.8 GiB": the unit is not repeated when it is the same.
func sizeOf(part, total int64) string {
	p, t := pacman.FormatSize(part), pacman.FormatSize(total)
	if i := strings.LastIndexByte(p, ' '); i >= 0 && strings.HasSuffix(t, p[i:]) {
		p = p[:i]
	}
	return p + " of " + t
}

// RecordPending records today (call it only with a successful preview)'s pending download size in the state, once a
// day, for the growth trend. It records the whole download, cached
// packages included, so a prefetch doesn't look like the backlog shrinking.
// It reports whether st changed, which means it needs saving.
func RecordPending(st *config.State, tx pacman.Transaction, now time.Time, loc *time.Location) bool {
	// No pending packages is a real sample too (0): it ends the growth run.
	return st.RecordPending(now.In(loc).Format(bandwidth.DayFormat), tx.TotalBytes())
}

// RecordUpgrade adds a finished upgrade hand-off to the state: the bytes it
// had to download and the packages it installed come from the preview made
// before it started.
func RecordUpgrade(st *config.State, tx pacman.Transaction, started, finished time.Time) {
	st.AddUpgradeSample(bandwidth.Sample{
		Time: finished, Bytes: tx.DownloadBytes, Packages: tx.Packages(),
		Seconds: finished.Sub(started).Seconds(),
	})
}

// RecordPrefetch adds a finished prefetch to the state.
func RecordPrefetch(st *config.State, tx pacman.Transaction, started, finished time.Time) {
	st.AddPrefetchSample(bandwidth.Sample{
		Time: finished, Bytes: tx.DownloadBytes, Packages: tx.Packages(),
		Seconds: finished.Sub(started).Seconds(),
	})
}

// ParallelDownloadsHint is the info hint shown in housekeeping and the
// upgrade modal when pacman downloads one file at a time, or "" when
// ParallelDownloads is already set above 1 (or pacman.conf can't be read).
func ParallelDownloadsHint(fsys system.FS) string {
	conf, err := fsys.ReadFile(pacman.ConfPath)
	if err != nil {
		return ""
	}
	if n, _ := pacman.ParallelDownloads(conf); n > 1 {
		return ""
	}
	return "pacman downloads one package at a time: set `ParallelDownloads = 5` in " + pacman.ConfPath + " to speed it up (cronen doesn't edit the file)."
}

// cacheNeedsRoot reports that the current user can't write to any package
// cache directory, which is when checkupdates -d would fail without the
// privilege command. An unreadable pacman.conf means the default cache
// directory.
//
// Needs checking on Arch: whether checkupdates -d needs root at all, and so
// whether this is the right test; see the M16 pull request.
func cacheNeedsRoot(fsys system.FS) bool {
	conf, _ := fsys.ReadFile(pacman.ConfPath)
	for _, dir := range pacman.CacheDirs(conf) {
		if fsys.Writable(dir) {
			return false
		}
	}
	return true
}

// PrefetchArgv builds the prefetch hand-off for the current system: the
// privilege command is added only when it is needed.
func PrefetchArgv(fsys system.FS, cfg config.Config) ([]string, error) {
	return ops.PrefetchArgv(cfg.PrivilegeArgv(), cacheNeedsRoot(fsys), cfg.PrefetchCommand)
}

// PrefetchPlan is the plan of the prefetch hand-off. previewed and download
// come from the transaction preview, which is its dry run: with no preview,
// or nothing left to download, there is no plan.
func PrefetchPlan(fsys system.FS, cfg config.Config, previewed bool, download int64) (ops.Plan, error) {
	return ops.PrefetchPlan(ops.PrefetchInput{
		Priv: cfg.PrivilegeArgv(), NeedsRoot: cacheNeedsRoot(fsys), Override: cfg.PrefetchCommand,
		Previewed: previewed, Download: download,
	})
}

// MountsPath is where the mounts are listed.
const MountsPath = "/proc/self/mounts"

// maxUsageFiles caps how many files of one package are statted.
const maxUsageFiles = 200

// LoadLastUsed finds when an installed package was last used: the newest
// access time among its files in bin directories and its .desktop files.
// ok is false when nothing says: no such files, files on a noatime mount
// (where access times are never updated and any date would be a lie), or
// anything unreadable.
//
// Needs checking on Arch: that /proc/self/mounts lists relatime for the
// root filesystem, and that access times of /usr/bin files are real. See
// the M16 pull request.
func LoadLastUsed(ctx context.Context, r system.Runner, fsys system.FS, name string) (t time.Time, ok bool) {
	mounts, err := fsys.ReadFile(MountsPath)
	if err != nil {
		return time.Time{}, false
	}
	files, err := pacman.PackageFiles(ctx, r, []string{name})
	if err != nil {
		return time.Time{}, false
	}
	var accesses []bandwidth.Access
	for _, f := range bandwidth.UsageFiles(files[name]) {
		if len(accesses) >= maxUsageFiles {
			break
		}
		fi, err := fsys.Stat(f)
		if err != nil {
			continue
		}
		if at, ok := system.AccessTime(fi); ok {
			accesses = append(accesses, bandwidth.Access{Path: f, Time: at})
		}
	}
	return bandwidth.LastUsed(accesses, bandwidth.ParseMounts(mounts))
}
