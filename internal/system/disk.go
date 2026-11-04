package system

import (
	"io/fs"
	"path/filepath"
	"sync"
)

// DiskUsage is what statfs reports for the filesystem holding a path.
type DiskUsage struct {
	// FreeBytes is the space available to an unprivileged user.
	FreeBytes int64
	// TotalBytes is the size of the filesystem.
	TotalBytes int64
	// ID identifies the filesystem, so two paths on the same one can be told
	// apart from two on different ones. Empty when the system doesn't say.
	ID string
}

// Disk reports free space.
type Disk interface {
	// Statfs reports on the filesystem holding path. Missing paths report
	// an error matching fs.ErrNotExist.
	Statfs(path string) (DiskUsage, error)
}

// OSDisk is the real Disk, backed by statfs(2).
type OSDisk struct{}

// Statfs implements Disk.
func (OSDisk) Statfs(path string) (DiskUsage, error) {
	return statfs(path)
}

// FakeDisk is a Disk with canned answers keyed by the path they were set
// for. A path with no answer of its own is answered like the filesystem
// that would hold it: by its nearest ancestor with one, so /boot on the
// root filesystem reports the root's numbers. It is safe for concurrent
// use.
type FakeDisk struct {
	mu    sync.Mutex
	usage map[string]DiskUsage
	calls []string
}

// NewFakeDisk returns a FakeDisk holding a copy of usage.
func NewFakeDisk(usage map[string]DiskUsage) *FakeDisk {
	d := &FakeDisk{usage: map[string]DiskUsage{}}
	for p, u := range usage {
		d.usage[filepath.Clean(p)] = u
	}
	return d
}

// Set registers the usage reported for path.
func (d *FakeDisk) Set(path string, u DiskUsage) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.usage[filepath.Clean(path)] = u
}

// Calls returns the paths asked about so far, in order.
func (d *FakeDisk) Calls() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.calls...)
}

// Statfs implements Disk.
func (d *FakeDisk) Statfs(path string) (DiskUsage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, path)
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		if u, ok := d.usage[p]; ok {
			return u, nil
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return DiskUsage{}, &fs.PathError{Op: "statfs", Path: path, Err: fs.ErrNotExist}
}
