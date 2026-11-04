package fixtures

import (
	"fmt"
	"strings"
)

// SyntheticQi builds a `pacman -Qi` dump of n made-up packages for size
// tests. The packages form a tree: pkg0001 to pkg0003 are required by
// pkg0000, and so on, so a chain leads from every package up to
// pkg0000. Every seventh package is installed explicitly, the others as
// dependencies. Names are pkg0000, pkg0001, …
func SyntheticQi(n int) []byte {
	var b strings.Builder
	for i := range n {
		reason := "Installed as a dependency for another package"
		if i%7 == 0 {
			reason = "Explicitly installed"
		}
		req := "None"
		if i > 0 {
			req = fmt.Sprintf("pkg%04d", (i-1)/3)
		}
		fmt.Fprintf(&b, `Name            : pkg%04d
Version         : 1.%d-1
Description     : Synthetic package number %d
Architecture    : x86_64
URL             : https://example.org/%d
Licenses        : GPL-2.0-only
Groups          : None
Provides        : None
Depends On      : glibc  pkg%04d
Optional Deps   : None
Required By     : %s
Optional For    : None
Conflicts With  : None
Replaces        : None
Installed Size  : %d.50 MiB
Packager        : Unknown Packager
Build Date      : Mon Oct  6 12:00:00 2025
Install Date    : Wed Oct 15 09:30:00 2025
Install Reason  : %s
Install Script  : No
Validated By    : Signature

`, i, i, i, i, i+1, req, i%300, reason)
	}
	return []byte(b.String())
}
