// Package preflight finds the problems that make an upgrade fail halfway or
// leave the system in a bad state: a full or unmounted /boot, a stale lock,
// a stale mirror, a pending reboot, an old keyring. Check is pure; Gather
// reads the facts it needs through the interfaces of internal/system.
package preflight

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/risk"
)

// KeyringAge is how long after the last full upgrade a pending
// archlinux-keyring is worth a note.
const KeyringAge = 60 * 24 * time.Hour

// ID names which check raised a problem.
type ID string

// The checks, in the order problems of equal severity are listed.
const (
	CheckBootUnmounted ID = "boot-unmounted"
	CheckSpace         ID = "space"
	CheckLock          ID = "db-lock"
	CheckMirror        ID = "mirror-lag"
	CheckReboot        ID = "reboot-pending"
	CheckKeyring       ID = "keyring"
)

var checkOrder = []ID{CheckBootUnmounted, CheckSpace, CheckLock, CheckMirror, CheckReboot, CheckKeyring}

// Problem is one thing wrong with the system, shown as a global banner.
type Problem struct {
	Severity risk.Severity
	Check    ID
	Message  string
}

// Space is the free space on the filesystem holding a path.
type Space struct {
	Path string
	// Free is in bytes.
	Free int64
	// ID identifies the filesystem; paths with the same non-empty ID share
	// their free space. See system.DiskUsage.
	ID string
}

// Facts is everything Check looks at. Gather fills the system side;
// the caller adds what the pending upgrade needs.
type Facts struct {
	// Now is the current time.
	Now time.Time

	// Updated are the names of the pending repo updates.
	Updated []string
	// Download is the bytes the upgrade downloads, and SizeDelta the change
	// in installed size, both from the transaction preview. Zero when the
	// preview is unavailable, which then skips the space checks that need
	// them.
	Download, SizeDelta int64
	// LastUpgrade is the time of the last full upgrade, zero if unknown.
	LastUpgrade time.Time

	// Root is / and Boot is /boot (the same filesystem as / when /boot is
	// not a mount point). Caches are the pacman cache directories. A Space
	// with an empty Path is unknown and its checks are skipped.
	Root, Boot Space
	Caches     []Space
	// BootDeclared reports a /boot entry in fstab, and BootMounted that
	// /boot is a mount point right now.
	BootDeclared, BootMounted bool
	// BootBytes is, per kernel package, the current size of its vmlinuz and
	// initramfs images in /boot. Kernels not found in /boot have no entry.
	BootBytes map[string]int64

	// LockExists reports /var/lib/pacman/db.lck.
	LockExists bool
	// RebootPending reports that the running kernel's modules are gone.
	RebootPending bool
	// RunningKernel is the release string, for the message.
	RunningKernel string

	// MirrorKnown reports that the mirror was checked. MirrorLag is how
	// far it is behind, and MirrorLimit when that counts as a problem.
	MirrorKnown bool
	MirrorRoot  string
	MirrorLag   time.Duration
	MirrorLimit time.Duration
}

// Kernels returns the pending updates that are kernels.
func (f Facts) Kernels() []string {
	var ks []string
	for _, n := range f.Updated {
		if risk.IsKernel(n) {
			ks = append(ks, n)
		}
	}
	return ks
}

// need is what the upgrade asks of one filesystem.
type need struct {
	path   string
	id     string
	free   int64
	amount int64
	what   string
}

// Check returns the problems, most severe first.
func Check(f Facts) []Problem {
	var ps []Problem
	kernels := f.Kernels()

	bootBroken := len(kernels) > 0 && f.BootDeclared && !f.BootMounted
	if bootBroken {
		ps = append(ps, Problem{risk.Critical, CheckBootUnmounted, fmt.Sprintf(
			"/boot is in fstab but not mounted, and %s is updating: the kernel would be installed on the root filesystem, where the bootloader won't find it. Mount /boot before upgrading.",
			strings.Join(kernels, ", "))})
	}

	ps = append(ps, spaceProblems(f, kernels, bootBroken)...)

	if f.LockExists {
		ps = append(ps, Problem{risk.Warn, CheckLock,
			"pacman's database lock (/var/lib/pacman/db.lck) exists. If pacman or an AUR helper is running, wait for it. If `pgrep -x pacman` finds nothing, the lock is stale and can be removed."})
	}
	if f.MirrorKnown && f.MirrorLimit > 0 && f.MirrorLag > f.MirrorLimit {
		where := "Your mirror"
		if f.MirrorRoot != "" {
			where += " (" + f.MirrorRoot + ")"
		}
		ps = append(ps, Problem{risk.Warn, CheckMirror, fmt.Sprintf(
			"%s is %s behind the reference mirror: checkupdates may be missing updates.",
			where, hours(f.MirrorLag))})
	}
	if f.RebootPending {
		msg := "A reboot is pending: an earlier upgrade replaced the running kernel"
		if f.RunningKernel != "" {
			msg += " (" + f.RunningKernel + ")"
		}
		ps = append(ps, Problem{risk.Warn, CheckReboot, msg + ", and the system hasn't rebooted since."})
	}
	if slices.Contains(f.Updated, "archlinux-keyring") && !f.LastUpgrade.IsZero() && f.Now.Sub(f.LastUpgrade) > KeyringAge {
		ps = append(ps, Problem{risk.Info, CheckKeyring, fmt.Sprintf(
			"archlinux-keyring is pending and the last full upgrade was %d days ago. If pacman reports signature errors, the wiki suggests `pacman -Sy archlinux-keyring && pacman -Su`. cronen never runs it.",
			int(f.Now.Sub(f.LastUpgrade)/(24*time.Hour)))})
	}

	slices.SortStableFunc(ps, func(a, b Problem) int {
		return cmp.Or(cmp.Compare(b.Severity, a.Severity),
			cmp.Compare(slices.Index(checkOrder, a.Check), slices.Index(checkOrder, b.Check)))
	})
	return ps
}

// spaceProblems compares what the upgrade needs with the free space of each
// filesystem it touches. Paths on one filesystem share its space, so their
// needs add up.
func spaceProblems(f Facts, kernels []string, bootBroken bool) []Problem {
	var needs []need
	for _, c := range f.Caches {
		if c.Path != "" && f.Download > 0 {
			needs = append(needs, need{c.Path, c.ID, c.Free, f.Download, "download " + pacman.FormatSize(f.Download)})
		}
	}
	if f.Root.Path != "" && f.SizeDelta > 0 {
		needs = append(needs, need{f.Root.Path, f.Root.ID, f.Root.Free, f.SizeDelta, "installed size +" + pacman.FormatSize(f.SizeDelta)})
	}
	// With /boot unmounted its numbers are the root's, and the problem is
	// already reported.
	if f.Boot.Path != "" && !bootBroken {
		var b int64
		for _, k := range kernels {
			b += f.BootBytes[k]
		}
		if b > 0 {
			needs = append(needs, need{f.Boot.Path, f.Boot.ID, f.Boot.Free, b, "kernel images " + pacman.FormatSize(b)})
		}
	}

	var ps []Problem
	done := make([]bool, len(needs))
	for i, n := range needs {
		if done[i] {
			continue
		}
		var paths, whats []string
		var total int64
		for j := i; j < len(needs); j++ {
			if done[j] || (j != i && (n.id == "" || needs[j].id != n.id)) {
				continue
			}
			done[j] = true
			if !slices.Contains(paths, needs[j].path) {
				paths = append(paths, needs[j].path)
			}
			whats = append(whats, needs[j].what)
			total += needs[j].amount
		}
		if total <= n.free {
			continue
		}
		sev := risk.Critical
		ps = append(ps, Problem{sev, CheckSpace, fmt.Sprintf(
			"Not enough free space on %s: the upgrade needs %s (%s) and %s is free.",
			strings.Join(paths, " and "), pacman.FormatSize(total), strings.Join(whats, ", "), pacman.FormatSize(n.free))})
	}
	return ps
}

func hours(d time.Duration) string {
	h := int(d.Round(time.Hour) / time.Hour)
	if h == 1 {
		return "1 hour"
	}
	return fmt.Sprintf("%d hours", h)
}
