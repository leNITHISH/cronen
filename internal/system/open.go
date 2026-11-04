package system

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// ReadAtCloser is an open file that can be read at any offset, which is
// what debug/elf needs.
type ReadAtCloser interface {
	io.ReaderAt
	io.Closer
}

// ErrNotRegular reports a path that isn't a regular file (a directory, a
// symlink, a device or a pipe). Opener implementations refuse those, so
// scanning a package's file list can never block on a FIFO.
var ErrNotRegular = errors.New("not a regular file")

// Opener opens files for random-access reading.
type Opener interface {
	// Open opens a regular file. Missing files report an error matching
	// fs.ErrNotExist, and anything else that isn't a regular file reports
	// ErrNotRegular. Symlinks are not followed.
	Open(path string) (ReadAtCloser, error)
}

// OSOpener is the real Opener, backed by package os.
type OSOpener struct{}

// Open implements Opener.
func (OSOpener) Open(path string) (ReadAtCloser, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, &fs.PathError{Op: "open", Path: path, Err: ErrNotRegular}
	}
	return os.Open(filepath.Clean(path))
}

// FakeOpener is an in-memory Opener. It is safe for concurrent use.
type FakeOpener struct {
	mu    sync.Mutex
	files map[string][]byte
	opens []string
}

// NewFakeOpener returns a FakeOpener serving a copy of files, keyed by path.
func NewFakeOpener(files map[string][]byte) *FakeOpener {
	o := &FakeOpener{files: map[string][]byte{}}
	for p, b := range files {
		o.files[filepath.Clean(p)] = append([]byte(nil), b...)
	}
	return o
}

// Opened returns the paths opened so far, in order.
func (o *FakeOpener) Opened() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.opens...)
}

// Open implements Opener.
func (o *FakeOpener) Open(path string) (ReadAtCloser, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.opens = append(o.opens, path)
	b, ok := o.files[filepath.Clean(path)]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
	}
	return fakeFile(b), nil
}

type fakeFile []byte

func (f fakeFile) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off > int64(len(f)) {
		return 0, fmt.Errorf("fake open: offset %d out of range", off)
	}
	n := copy(p, f[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (fakeFile) Close() error { return nil }
