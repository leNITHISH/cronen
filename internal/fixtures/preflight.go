package fixtures

import (
	"github.com/leNITHISH/cronen/internal/system"
)

// The preflight fixtures are HAND-WRITTEN, pending capture from a real Arch
// system. See HAND_WRITTEN.txt in testdata/. They describe a machine whose
// /boot is declared in fstab but not mounted, with a kernel update pending:
// the situation preflight exists to catch. Everything else is healthy.
const (
	Fstab               = "fstab.txt"                // /etc/fstab
	ProcSelfMounts      = "proc_self_mounts.txt"     // /proc/self/mounts, without /boot
	Mirrorlist          = "mirrorlist.txt"           // /etc/pacman.d/mirrorlist
	LastUpdateMirror    = "lastupdate_mirror.txt"    // <mirror>/lastupdate
	LastUpdateReference = "lastupdate_reference.txt" // geo.mirror.pkgbuild.com/lastupdate
)

// URLs the fake fetcher answers for the mirror check.
const (
	MirrorLastUpdateURL    = "https://mirror.example.org/archlinux/lastupdate"
	ReferenceLastUpdateURL = "https://geo.mirror.pkgbuild.com/lastupdate"
)

// RunningKernel is the release the fixture system booted.
const RunningKernel = "6.17.4-arch1-1"

// preflightFiles are the files preflight reads, for FS.
func preflightFiles() map[string][]byte {
	return map[string][]byte{
		"/etc/fstab":                 Bytes(Fstab),
		"/proc/self/mounts":          Bytes(ProcSelfMounts),
		"/etc/pacman.d/mirrorlist":   Bytes(Mirrorlist),
		"/proc/sys/kernel/osrelease": []byte(RunningKernel + "\n"),
		"/usr/lib/modules/" + RunningKernel + "/modules.dep": []byte("kernel/fs/fat/fat.ko.zst:\n"),
		"/boot/vmlinuz-linux":                                make([]byte, 1<<10),
		"/boot/initramfs-linux.img":                          make([]byte, 2<<10),
		"/boot/initramfs-linux-fallback.img":                 make([]byte, 4<<10),
	}
}

func preflightResponse(url string) (system.FakeResponse, bool) {
	switch url {
	case MirrorLastUpdateURL:
		return system.FakeResponse{Body: Bytes(LastUpdateMirror)}, true
	case ReferenceLastUpdateURL:
		return system.FakeResponse{Body: Bytes(LastUpdateReference)}, true
	}
	return system.FakeResponse{}, false
}

// Disk returns a FakeDisk for the fixture system: one filesystem with
// plenty of room, since /boot isn't mounted and so lies on it too.
func Disk() *system.FakeDisk {
	return system.NewFakeDisk(map[string]system.DiskUsage{
		"/": {FreeBytes: 40 << 30, TotalBytes: 100 << 30, ID: "root"},
	})
}
