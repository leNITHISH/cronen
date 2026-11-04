package system

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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
	// ReadDir lists the files directly inside a directory (not its
	// subdirectories), sorted by name. A missing directory reports an
	// error matching fs.ErrNotExist.
	ReadDir(path string) ([]fs.FileInfo, error)
	// Glob returns the paths matching a filepath.Match pattern, sorted.
	Glob(pattern string) ([]string, error)
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

// ReadDir implements FS.
func (OSFS) ReadDir(path string) ([]fs.FileInfo, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var out []fs.FileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil {
			out = append(out, info)
		}
	}
	return out, nil
}

// Glob implements FS.
func (OSFS) Glob(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

// FakeFS is an in-memory FS. It is safe for concurrent use.
type FakeFS struct {
	mu    sync.Mutex
	files map[string][]byte
	// ReadErr, if set, is returned by every ReadFile call, to simulate an
	// unreadable file.
	ReadErr error
	// sizes overrides the size Stat and ReadDir report for a file, so a
	// test can have a 4 GiB cache without holding 4 GiB.
	sizes map[string]int64
}

// SetSize makes Stat and ReadDir report size bytes for the file at path.
func (f *FakeFS) SetSize(path string, size int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sizes == nil {
		f.sizes = map[string]int64{}
	}
	f.sizes[filepath.Clean(path)] = size
}

func (f *FakeFS) sizeOf(path string, b []byte) int64 {
	if n, ok := f.sizes[path]; ok {
		return n
	}
	return int64(len(b))
}

// ReadDir implements FS: the files whose directory is path.
func (f *FakeFS) ReadDir(path string) ([]fs.FileInfo, error) {
	path = filepath.Clean(path)
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fs.FileInfo
	found := false
	for p, b := range f.files {
		if !strings.HasPrefix(p, path+string(filepath.Separator)) {
			continue
		}
		found = true
		if filepath.Dir(p) == path {
			out = append(out, fakeInfo{name: filepath.Base(p), size: f.sizeOf(p, b)})
		}
	}
	if !found {
		return nil, &fs.PathError{Op: "readdir", Path: path, Err: fs.ErrNotExist}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

// Glob implements FS: the files and directories holding a file that match.
func (f *FakeFS) Glob(pattern string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	set := map[string]bool{}
	for p := range f.files {
		for q := p; q != "/" && q != "." && q != ""; q = filepath.Dir(q) {
			if ok, err := filepath.Match(pattern, q); err != nil {
				return nil, err
			} else if ok {
				set[q] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
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
		return fakeInfo{name: filepath.Base(path), size: f.sizeOf(path, b)}, nil
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
