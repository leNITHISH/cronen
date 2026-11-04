package preflight

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/risk"
)

const (
	mib = 1 << 20
	gib = 1 << 30
)

var now = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

// healthy is a system where nothing is wrong and a kernel is updating.
func healthy() Facts {
	return Facts{
		Now:          now,
		Updated:      []string{"linux", "linux-headers", "firefox"},
		Download:     500 * mib,
		SizeDelta:    100 * mib,
		LastUpgrade:  now.Add(-3 * 24 * time.Hour),
		Root:         Space{Path: "/", Free: 20 * gib, ID: "root"},
		Boot:         Space{Path: "/boot", Free: 300 * mib, ID: "esp"},
		Caches:       []Space{{Path: "/var/cache/pacman/pkg", Free: 20 * gib, ID: "root"}},
		BootDeclared: true,
		BootMounted:  true,
		BootBytes:    map[string]int64{"linux": 60 * mib},
		MirrorKnown:  true,
		MirrorLag:    time.Hour,
		MirrorLimit:  6 * time.Hour,
	}
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Facts)
		want    []ID
		wantSev []risk.Severity
		// contains are substrings the messages must hold, in order.
		contains []string
	}{
		{name: "healthy", mutate: func(*Facts) {}},

		// boot unmounted
		{
			name:     "boot declared but unmounted while the kernel updates",
			mutate:   func(f *Facts) { f.BootMounted = false },
			want:     []ID{CheckBootUnmounted},
			wantSev:  []risk.Severity{risk.Critical},
			contains: []string{"/boot is in fstab but not mounted"},
		},
		{
			name: "boot unmounted but no kernel updating",
			mutate: func(f *Facts) {
				f.BootMounted = false
				f.Updated = []string{"firefox", "linux-firmware", "linux-api-headers"}
			},
		},
		{
			name:   "boot not declared, so not mounted is fine",
			mutate: func(f *Facts) { f.BootDeclared, f.BootMounted = false, false },
		},

		// space
		{
			name:     "boot full",
			mutate:   func(f *Facts) { f.Boot.Free = 59 * mib },
			want:     []ID{CheckSpace},
			wantSev:  []risk.Severity{risk.Critical},
			contains: []string{"/boot", "kernel images 60 MiB", "59 MiB is free"},
		},
		{
			name:   "boot exactly enough",
			mutate: func(f *Facts) { f.Boot.Free = 60 * mib },
		},
		{
			name: "boot full but no kernel updating",
			mutate: func(f *Facts) {
				f.Boot.Free = 1
				f.Updated = []string{"firefox"}
			},
		},
		{
			name:   "kernel not found in /boot, nothing to compare",
			mutate: func(f *Facts) { f.Boot.Free = 1; f.BootBytes = nil },
		},
		{
			name:     "root too small for the installed size",
			mutate:   func(f *Facts) { f.Root.Free = 99 * mib; f.Caches[0].ID = "other" },
			want:     []ID{CheckSpace},
			wantSev:  []risk.Severity{risk.Critical},
			contains: []string{"Not enough free space on /:", "installed size +100 MiB"},
		},
		{
			name:   "installed size shrinks, nothing needed on root",
			mutate: func(f *Facts) { f.SizeDelta = -300 * mib; f.Root.Free = 0; f.Caches[0].ID = "other" },
		},
		{
			name:     "cache too small for the download",
			mutate:   func(f *Facts) { f.Caches[0].Free = 499 * mib; f.Caches[0].ID = "other" },
			want:     []ID{CheckSpace},
			wantSev:  []risk.Severity{risk.Critical},
			contains: []string{"/var/cache/pacman/pkg", "download 500 MiB"},
		},
		{
			name:   "no download, empty cache dir space irrelevant",
			mutate: func(f *Facts) { f.Download = 0; f.Caches[0].Free = 0; f.Caches[0].ID = "other" },
		},
		{
			name: "cache and root share a filesystem: needs add up",
			mutate: func(f *Facts) {
				f.Root.Free = 550 * mib
				f.Caches[0].Free = 550 * mib
			},
			want:     []ID{CheckSpace},
			wantSev:  []risk.Severity{risk.Critical},
			contains: []string{"/var/cache/pacman/pkg and /", "600 MiB", "download 500 MiB, installed size +100 MiB"},
		},
		{
			name: "cache and root share a filesystem with room for both",
			mutate: func(f *Facts) {
				f.Root.Free = 600 * mib
				f.Caches[0].Free = 600 * mib
			},
		},
		{
			name: "boot on the root filesystem adds to its needs",
			mutate: func(f *Facts) {
				f.Boot = Space{Path: "/boot", Free: 640 * mib, ID: "root"}
				f.Root.Free = 640 * mib
				f.Caches[0].Free = 640 * mib
			},
			want:     []ID{CheckSpace},
			wantSev:  []risk.Severity{risk.Critical},
			contains: []string{"/var/cache/pacman/pkg and / and /boot", "660 MiB"},
		},
		{
			name: "filesystems without an ID are never merged",
			mutate: func(f *Facts) {
				f.Root.ID, f.Caches[0].ID = "", ""
				f.Root.Free = 100 * mib
				f.Caches[0].Free = 500 * mib
			},
		},
		{
			name: "preview unavailable: only the boot check can run",
			mutate: func(f *Facts) {
				f.Download, f.SizeDelta = 0, 0
				f.Root.Free, f.Caches[0].Free = 0, 0
			},
		},
		{
			name: "boot unmounted: no second report about /boot space",
			mutate: func(f *Facts) {
				f.BootMounted = false
				f.Boot = Space{Path: "/boot", Free: 1, ID: "root"}
			},
			want:    []ID{CheckBootUnmounted},
			wantSev: []risk.Severity{risk.Critical},
		},
		{
			name:   "unknown spaces are skipped",
			mutate: func(f *Facts) { f.Root, f.Boot, f.Caches = Space{}, Space{}, nil },
		},

		// lock
		{
			name:     "db lock exists",
			mutate:   func(f *Facts) { f.LockExists = true },
			want:     []ID{CheckLock},
			wantSev:  []risk.Severity{risk.Warn},
			contains: []string{"db.lck", "pgrep -x pacman", "stale"},
		},

		// mirror
		{
			name:     "mirror behind",
			mutate:   func(f *Facts) { f.MirrorLag = 9 * time.Hour; f.MirrorRoot = "https://m.example/arch" },
			want:     []ID{CheckMirror},
			wantSev:  []risk.Severity{risk.Warn},
			contains: []string{"https://m.example/arch", "9 hours behind", "checkupdates may be missing updates"},
		},
		{
			name:   "mirror exactly at the limit",
			mutate: func(f *Facts) { f.MirrorLag = 6 * time.Hour },
		},
		{
			name:   "mirror lag unknown",
			mutate: func(f *Facts) { f.MirrorKnown = false; f.MirrorLag = 100 * time.Hour },
		},
		{
			name:   "mirror check without a limit",
			mutate: func(f *Facts) { f.MirrorLimit = 0; f.MirrorLag = 100 * time.Hour },
		},

		// reboot
		{
			name:     "reboot pending",
			mutate:   func(f *Facts) { f.RebootPending = true; f.RunningKernel = "6.17.4-arch1-1" },
			want:     []ID{CheckReboot},
			wantSev:  []risk.Severity{risk.Warn},
			contains: []string{"reboot is pending", "6.17.4-arch1-1"},
		},

		// keyring
		{
			name: "keyring pending after a long gap",
			mutate: func(f *Facts) {
				f.Updated = append(f.Updated, "archlinux-keyring")
				f.LastUpgrade = now.Add(-61 * 24 * time.Hour)
			},
			want:     []ID{CheckKeyring},
			wantSev:  []risk.Severity{risk.Info},
			contains: []string{"61 days ago", "pacman -Sy archlinux-keyring && pacman -Su"},
		},
		{
			name: "keyring pending, recent upgrade",
			mutate: func(f *Facts) {
				f.Updated = append(f.Updated, "archlinux-keyring")
				f.LastUpgrade = now.Add(-59 * 24 * time.Hour)
			},
		},
		{
			name: "keyring pending, last upgrade unknown",
			mutate: func(f *Facts) {
				f.Updated = append(f.Updated, "archlinux-keyring")
				f.LastUpgrade = time.Time{}
			},
		},
		{
			name:   "long gap but no keyring pending",
			mutate: func(f *Facts) { f.LastUpgrade = now.Add(-400 * 24 * time.Hour) },
		},

		// ordering
		{
			name: "most severe first, ties in check order",
			mutate: func(f *Facts) {
				f.Updated = append(f.Updated, "archlinux-keyring")
				f.LastUpgrade = now.Add(-90 * 24 * time.Hour)
				f.RebootPending = true
				f.LockExists = true
				f.MirrorLag = 12 * time.Hour
				f.BootMounted = false
				f.Caches[0].Free = 1
				f.Caches[0].ID = "other"
			},
			want:    []ID{CheckBootUnmounted, CheckSpace, CheckLock, CheckMirror, CheckReboot, CheckKeyring},
			wantSev: []risk.Severity{risk.Critical, risk.Critical, risk.Warn, risk.Warn, risk.Warn, risk.Info},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := healthy()
			tt.mutate(&f)
			got := Check(f)
			var ids []ID
			var sevs []risk.Severity
			for _, p := range got {
				ids = append(ids, p.Check)
				sevs = append(sevs, p.Severity)
			}
			if !slices.Equal(ids, tt.want) {
				t.Fatalf("checks = %v, want %v\n%+v", ids, tt.want, got)
			}
			if tt.wantSev != nil && !slices.Equal(sevs, tt.wantSev) {
				t.Errorf("severities = %v, want %v", sevs, tt.wantSev)
			}
			if len(tt.contains) > 0 {
				msg := got[0].Message
				for _, c := range tt.contains {
					if !strings.Contains(msg, c) {
						t.Errorf("message %q lacks %q", msg, c)
					}
				}
			}
		})
	}
}

func TestKernels(t *testing.T) {
	f := Facts{Updated: []string{"linux", "linux-lts", "linux-zen-headers", "linux-firmware", "linux-api-headers", "vim"}}
	got := f.Kernels()
	for _, want := range []string{"linux", "linux-lts"} {
		if !slices.Contains(got, want) {
			t.Errorf("Kernels() = %v, missing %s", got, want)
		}
	}
	for _, not := range []string{"linux-firmware", "linux-api-headers", "vim"} {
		if slices.Contains(got, not) {
			t.Errorf("Kernels() = %v, wrongly has %s", got, not)
		}
	}
}
