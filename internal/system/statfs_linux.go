package system

import (
	"errors"
	"io/fs"
	"strconv"
	"syscall"
)

func statfs(path string) (DiskUsage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		if errors.Is(err, syscall.ENOENT) {
			err = fs.ErrNotExist
		}
		return DiskUsage{}, &fs.PathError{Op: "statfs", Path: path, Err: err}
	}
	bsize := uint64(st.Bsize)
	u := DiskUsage{
		FreeBytes:  int64(st.Bavail * bsize),
		TotalBytes: int64(st.Blocks * bsize),
	}
	// Some filesystems report a zero fsid; that says nothing.
	if a, b := uint32(st.Fsid.X__val[0]), uint32(st.Fsid.X__val[1]); a != 0 || b != 0 {
		u.ID = strconv.FormatUint(uint64(a), 16) + ":" + strconv.FormatUint(uint64(b), 16)
	}
	return u, nil
}
