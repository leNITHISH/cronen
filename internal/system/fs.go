package system

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FS is the filesystem access cronen needs: reading pacman.log, and reading
// and writing the state file and the HTTP cache.
type FS interface {
	// ReadFile returns the contents of a file. Missing files report an
	// error matching fs.ErrNotExist.
	ReadFile(path string) ([]byte, error)
	// WriteFile atomically replaces path with data, creating parent
	// directories as needed.
	WriteFile(path string, data []byte, perm fs.FileMode) error
	// MkdirAll creates a directory and any missing parents.
	MkdirAll(path string, perm fs.FileMode) error
	// Stat reports on a file or directory. Missing paths report an error
	// matching fs.ErrNotExist.
	Stat(path string) (fs.FileInfo, error)
}

// OSFS is the real FS, backed by package os.
type OSFS struct{}

// ReadFile implements FS.
func (OSFS) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// WriteFile implements FS by writing a temporary file in the same directory
// and renaming it over path.
func (OSFS) WriteFile(path string, data []byte, perm fs.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// MkdirAll implements FS.
func (OSFS) MkdirAll(path string, perm fs.FileMode) error {
	return os.MkdirAll(path, perm)
}

// Stat implements FS.
func (OSFS) Stat(path string) (fs.FileInfo, error) {
	return os.Stat(path)
}

// FakeFS is an in-memory FS. It is safe for concurrent use.
type FakeFS struct {
	mu    sync.Mutex
	files map[string][]byte
	// ReadErr, if set, is returned by every ReadFile call, to simulate an
	// unreadable file.
	ReadErr error
}

// NewFakeFS returns a FakeFS holding a copy of files, keyed by path.
func NewFakeFS(files map[string][]byte) *FakeFS {
	f := &FakeFS{files: map[string][]byte{}}
	for p, b := range files {
		f.files[filepath.Clean(p)] = append([]byte(nil), b...)
	}
	return f
}

// ReadFile implements FS.
func (f *FakeFS) ReadFile(path string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ReadErr != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: f.ReadErr}
	}
	b, ok := f.files[filepath.Clean(path)]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
	}
	return append([]byte(nil), b...), nil
}

// WriteFile implements FS.
func (f *FakeFS) WriteFile(path string, data []byte, _ fs.FileMode) error {
	if path == "" {
		return &fs.PathError{Op: "write", Path: path, Err: errors.New("empty path")}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[filepath.Clean(path)] = append([]byte(nil), data...)
	return nil
}

// MkdirAll implements FS. Directories are implicit in FakeFS.
func (f *FakeFS) MkdirAll(string, fs.FileMode) error {
	return nil
}

// Stat implements FS. A path is a file if it was given to NewFakeFS or
// WriteFile, and a directory if some file lies beneath it.
func (f *FakeFS) Stat(path string) (fs.FileInfo, error) {
	path = filepath.Clean(path)
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.files[path]; ok {
		return fakeInfo{name: filepath.Base(path), size: int64(len(b))}, nil
	}
	prefix := path + string(filepath.Separator)
	if path == string(filepath.Separator) {
		prefix = path
	}
	for p := range f.files {
		if strings.HasPrefix(p, prefix) {
			return fakeInfo{name: filepath.Base(path), dir: true}, nil
		}
	}
	return nil, &fs.PathError{Op: "stat", Path: path, Err: fs.ErrNotExist}
}

type fakeInfo struct {
	name string
	size int64
	dir  bool
}

func (i fakeInfo) Name() string { return i.name }
func (i fakeInfo) Size() int64  { return i.size }
func (i fakeInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (i fakeInfo) ModTime() time.Time { return time.Time{} }
func (i fakeInfo) IsDir() bool        { return i.dir }
func (i fakeInfo) Sys() any           { return nil }
