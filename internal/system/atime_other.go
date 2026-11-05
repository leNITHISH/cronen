//go:build !linux

package system

import (
	"io/fs"
	"time"
)

func sysAccessTime(fs.FileInfo) (time.Time, bool) { return time.Time{}, false }

func sysWritable(string) bool { return false }
