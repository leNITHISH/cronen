package system

import (
	"io/fs"
	"syscall"
	"time"
)

func sysAccessTime(fi fs.FileInfo) (time.Time, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(int64(st.Atim.Sec), int64(st.Atim.Nsec)), true
}

func sysWritable(path string) bool {
	// access(2) with W_OK checks the real user's rights, which is who
	// runs checkupdates.
	return syscall.Access(path, 2) == nil
}
