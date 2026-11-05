package fixtures

import (
	"strings"

	"github.com/leNITHISH/cronen/internal/system"
)

// M18, the partial-upgrade detector. Hand-written, pending capture from a
// real system (see testdata/HAND_WRITTEN.txt).

// PacmanQRepoExtra are the installed repo packages the demo's `pacman -Q`
// adds to the foreign ones (pacman_Qm.txt): the ones with a -Ql fixture.
const pacmanQRepoExtra = "firefox 144.0-1\nripgrep 14.1.1-1\nvim 9.1.1800-1\n"

// setPartial registers the M18 commands on r. The real database has nothing
// pending (pacman -Qu exits 1 with no output), so the demo starts healthy;
// tests that want a synced database register their own result.
func setPartial(r *system.FakeRunner) {
	r.Set("pacman -Qu", system.FakeResult{Code: 1})
	r.Set("pacman -Q", system.FakeResult{Stdout: append(Bytes(PacmanQm), pacmanQRepoExtra...)})
}

// partialFiles are the libraries the demo's linkage scan finds. The fixture
// foreign packages link libc, libzstd, libuv, libluajit and libgbm, all
// present; paru links libalpm.so.15, which is not: what a partial upgrade
// leaves behind. /etc/ld.so.conf includes a directory with no libraries in
// it.
func partialFiles() map[string][]byte {
	files := map[string][]byte{
		"/etc/ld.so.conf":          []byte("include /etc/ld.so.conf.d/*.conf\n"),
		"/etc/ld.so.conf.d/x.conf": []byte("/opt/cronen-demo/lib\n"),
	}
	for _, lib := range []string{"libc.so.6", "libzstd.so.1", "libuv.so.1", "libluajit-5.1.so.2", "libgbm.so.1", "libalpm.so.16"} {
		files["/usr/lib/"+lib] = []byte{1}
	}
	return files
}

// qlAny answers pacman -Ql -- names from both -Ql fixtures, so a batch
// mixing repo and foreign packages works. It fails for a name in neither.
func qlAny(names []string) (system.FakeResult, bool) {
	var out strings.Builder
	for _, n := range names {
		found := false
		for _, file := range []string{PacmanQlRepo, PacmanQlForeign} {
			for l := range strings.Lines(string(Bytes(file))) {
				if strings.HasPrefix(l, n+" ") {
					out.WriteString(l)
					found = true
				}
			}
		}
		if !found {
			return system.FakeResult{Stderr: []byte("error: package '" + n + "' was not found\n"), Code: 1}, true
		}
	}
	return system.FakeResult{Stdout: []byte(out.String())}, true
}
