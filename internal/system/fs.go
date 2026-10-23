package system

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
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
