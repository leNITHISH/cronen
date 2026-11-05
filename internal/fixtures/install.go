package fixtures

import (
	"strings"

	"github.com/leNITHISH/cronen/internal/system"
)

// The M17 fixtures are HAND-WRITTEN, pending capture (see HAND_WRITTEN.txt).
const (
	PacmanConfArchive = "pacman_conf_archive.txt" // /etc/pacman.conf with custom repos and Include lines
	PacmanSupInstall  = "pacman_Sup_install.txt"  // pacman -Sup ... -- htop
	PacmanSpArchive   = "pacman_Sp_archive.txt"   // pacman -Sp --dbpath <archive dir> ... -- htop
	PacmanQuArchiveA  = "pacman_Qu_archive_a.txt" // pacman -Qu against the archive day of the upgrade
	PacmanQuArchiveB  = "pacman_Qu_archive_b.txt" // ... and against the day after
	PacmanSiInstall   = "pacman_Si_install.txt"   // pacman -Si for htop and libnl
	InstallPackage    = "htop"                    // a repo package the fixtures can install
	RealDBPath        = "/var/lib/pacman"         // the real database directory
	ArchiveBase       = "/home/demo/.cache/cronen/archive"
	// ArchiveDayA and ArchiveDayB are the two archive days of the fixture
	// last full upgrade (2026-10-20 19:02 +05:30 is 13:32 UTC).
	ArchiveDayA = "2026-10-20"
	ArchiveDayB = "2026-10-21"
)

// installHandler answers the install helper's commands. The archive
// directory's last element is its day.
func installHandler(line string) (system.FakeResult, bool) {
	switch {
	case line == "fakeroot --version":
		return system.FakeResult{Stdout: []byte("fakeroot version 1.37.1\n")}, true
	case strings.HasPrefix(line, "pacman -Si --dbpath "+SyncDBPath+" --logfile /dev/null -- "):
		names := strings.Fields(strings.TrimPrefix(line, "pacman -Si --dbpath "+SyncDBPath+" --logfile /dev/null -- "))
		return siInstall(names), true
	case strings.HasPrefix(line, "pacman -Sup --dbpath "+SyncDBPath+" --logfile /dev/null --noconfirm --print-format %n %v %r %s -- "):
		names := strings.Fields(strings.TrimPrefix(line, "pacman -Sup --dbpath "+SyncDBPath+" --logfile /dev/null --noconfirm --print-format %n %v %r %s -- "))
		if len(names) == 1 && names[0] == InstallPackage {
			return system.FakeResult{Stdout: Bytes(PacmanSupInstall)}, true
		}
		return system.FakeResult{Stderr: []byte("error: target not found: " + names[0] + "\n"), Code: 1}, true
	case strings.HasPrefix(line, "fakeroot -- pacman -Sy --dbpath "+ArchiveBase+"/"):
		return system.FakeResult{}, true
	case strings.HasPrefix(line, "pacman -Qu --dbpath "+ArchiveBase+"/"):
		switch {
		case strings.Contains(line, "/"+ArchiveDayA+" "):
			return system.FakeResult{Stdout: Bytes(PacmanQuArchiveA)}, true
		case strings.Contains(line, "/"+ArchiveDayB+" "):
			return system.FakeResult{Stdout: Bytes(PacmanQuArchiveB)}, true
		}
	case strings.HasPrefix(line, "pacman -Sp --dbpath "+ArchiveBase+"/"):
		if strings.HasSuffix(line, " -- "+InstallPackage) {
			return system.FakeResult{Stdout: Bytes(PacmanSpArchive)}, true
		}
		return system.FakeResult{Stderr: []byte("error: target not found\n"), Code: 1}, true
	}
	return system.FakeResult{}, false
}

// siInstall answers pacman -Si for the install fixtures: the blocks of the
// names it knows, or pacman's "was not found" error for the first unknown.
func siInstall(names []string) system.FakeResult {
	blocks := infoBlocks(PacmanSiInstall)
	for n, b := range infoBlocks(PacmanSi) {
		if _, ok := blocks[n]; !ok {
			blocks[n] = b
		}
	}
	var out []string
	for _, n := range names {
		b, ok := blocks[n]
		if !ok {
			return system.FakeResult{Stderr: []byte("error: package '" + n + "' was not found\n"), Code: 1}
		}
		out = append(out, b)
	}
	return system.FakeResult{Stdout: []byte(strings.Join(out, "\n"))}
}
