//go:build !linux

package system

import "errors"

func statfs(string) (DiskUsage, error) {
	return DiskUsage{}, errors.New("statfs: unsupported platform")
}
