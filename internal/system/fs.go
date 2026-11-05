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
	// Lstat is Stat that doesn't follow a final symbolic link: it reports
	// on the link itself, so a caller can refuse links.
	Lstat(path string) (fs.FileInfo, error)
	// ReadDir lists the files directly inside a directory (not its
	// subdirectories), sorted by name. A missing directory reports an
	// error matching fs.ErrNotExist.
	ReadDir(path string) ([]fs.FileInfo, error)
	// Writable reports whether the current user can write to path.
	Writable(path string) bool
	// Glob returns the paths matching a filepath.Match pattern, sorted.
	Glob(pattern string) ([]string, error)
	// MkdirNew creates one directory that must not exist yet, with exactly
	// perm (no umask loosening), and fails if it does. Its parent must
	// exist. It is how cronen gets a directory nobody else prepared.
	MkdirNew(path string, perm fs.FileMode) error
	// Symlink creates the symbolic link newname pointing at oldname. It
	// fails if newname exists.
	Symlink(oldname, newname string) error
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

// Lstat implements FS.
func (OSFS) Lstat(path string) (fs.FileInfo, error) {
	return os.Lstat(path)
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

// MkdirNew implements FS: os.Mkdir (which fails on an existing path), then
// Chmod so the umask can't have made the mode looser or stricter than asked.
func (OSFS) MkdirNew(path string, perm fs.FileMode) error {
	if err := os.Mkdir(path, perm); err != nil {
		return err
	}
	return os.Chmod(path, perm)
}

// Symlink implements FS.
func (OSFS) Symlink(oldname, newname string) error {
	return os.Symlink(oldname, newname)
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
	// symlinks are the paths Lstat reports as symbolic links.
	symlinks map[string]bool
	// writable and atimes are set by SetWritable and SetAccessTime.
	writable map[string]bool
	atimes   map[string]time.Time
	// dirs are the directories made with MkdirNew and their modes, and
	// links the symbolic links made with Symlink and their targets. perms
	// are the modes files were written with.
	dirs  map[string]fs.FileMode
	links map[string]string
	perms map[string]fs.FileMode
}

// DirMode returns the mode a directory made with MkdirNew has.
func (f *FakeFS) DirMode(path string) (fs.FileMode, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.dirs[filepath.Clean(path)]
	return m, ok
}

// FileMode returns the mode a file was last written with by WriteFile.
func (f *FakeFS) FileMode(path string) (fs.FileMode, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.perms[filepath.Clean(path)]
	return m, ok
}

// LinkTarget returns what a symbolic link made with Symlink points at.
func (f *FakeFS) LinkTarget(path string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.links[filepath.Clean(path)]
	return t, ok
}

// Dirs returns the directories made with MkdirNew, sorted.
func (f *FakeFS) Dirs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.dirs))
	for d := range f.dirs {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func (f *FakeFS) existsLocked(path string) bool {
	if _, ok := f.files[path]; ok {
		return true
	}
	if _, ok := f.dirs[path]; ok {
		return true
	}
	if _, ok := f.links[path]; ok {
		return true
	}
	prefix := path + string(filepath.Separator)
	for p := range f.files {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// MkdirNew implements FS: it fails if path exists, and records the mode.
func (f *FakeFS) MkdirNew(path string, perm fs.FileMode) error {
	path = filepath.Clean(path)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.existsLocked(path) {
		return &fs.PathError{Op: "mkdir", Path: path, Err: fs.ErrExist}
	}
	if f.dirs == nil {
		f.dirs = map[string]fs.FileMode{}
	}
	f.dirs[path] = perm
	return nil
}

// Symlink implements FS: it fails if newname exists, and records the link.
func (f *FakeFS) Symlink(oldname, newname string) error {
	newname = filepath.Clean(newname)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.existsLocked(newname) {
		return &fs.PathError{Op: "symlink", Path: newname, Err: fs.ErrExist}
	}
	if f.links == nil {
		f.links = map[string]string{}
	}
	f.links[newname] = oldname
	return nil
}

// SetSymlink makes Lstat report the file at path as a symbolic link (Stat
// still sees the file, as it follows links).
func (f *FakeFS) SetSymlink(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.symlinks == nil {
		f.symlinks = map[string]bool{}
	}
	f.symlinks[filepath.Clean(path)] = true
}

// Lstat implements FS: Stat, except for paths marked with SetSymlink.
func (f *FakeFS) Lstat(path string) (fs.FileInfo, error) {
	f.mu.Lock()
	_, isLink := f.links[filepath.Clean(path)]
	f.mu.Unlock()
	if isLink {
		return fakeInfo{name: filepath.Base(path), link: true}, nil
	}
	fi, err := f.Stat(path)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.symlinks[filepath.Clean(path)] {
		return fakeInfo{name: fi.Name(), size: fi.Size(), link: true}, nil
	}
	return fi, nil
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
func (f *FakeFS) WriteFile(path string, data []byte, perm fs.FileMode) error {
	if path == "" {
		return &fs.PathError{Op: "write", Path: path, Err: errors.New("empty path")}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[filepath.Clean(path)] = append([]byte(nil), data...)
	if f.perms == nil {
		f.perms = map[string]fs.FileMode{}
	}
	f.perms[filepath.Clean(path)] = perm
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
		return fakeInfo{name: filepath.Base(path), size: f.sizeOf(path, b), atime: f.atimes[path]}, nil
	}
	prefix := path + string(filepath.Separator)
	if path == string(filepath.Separator) {
		prefix = path
	}
	if perm, ok := f.dirs[path]; ok {
		return fakeInfo{name: filepath.Base(path), dir: true, perm: perm}, nil
	}
	for p := range f.files {
		if strings.HasPrefix(p, prefix) {
			return fakeInfo{name: filepath.Base(path), dir: true}, nil
		}
	}
	for p := range f.dirs {
		if strings.HasPrefix(p, prefix) {
			return fakeInfo{name: filepath.Base(path), dir: true}, nil
		}
	}
	for p := range f.links {
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
	link bool
	// perm, if set, is the permission bits Mode reports.
	perm fs.FileMode
	// atime is reported by AccessTime.
	atime time.Time
}

func (i fakeInfo) AccessTime() time.Time { return i.atime }

func (i fakeInfo) Name() string { return i.name }
func (i fakeInfo) Size() int64  { return i.size }
func (i fakeInfo) Mode() fs.FileMode {
	switch {
	case i.dir && i.perm != 0:
		return fs.ModeDir | i.perm
	case i.dir:
		return fs.ModeDir | 0o755
	case i.link:
		return fs.ModeSymlink | 0o777
	}
	return 0o644
}
func (i fakeInfo) ModTime() time.Time { return time.Time{} }
func (i fakeInfo) IsDir() bool        { return i.dir }
func (i fakeInfo) Sys() any           { return nil }

// AccessTime returns the last access time of a file from its FileInfo, as
// returned by FS.Stat. ok is false when the platform or the FS doesn't say.
// Whether the time means anything depends on how the filesystem is mounted
// (noatime never updates it); see internal/bandwidth.
func AccessTime(fi fs.FileInfo) (time.Time, bool) {
	if a, ok := fi.(interface{ AccessTime() time.Time }); ok {
		t := a.AccessTime()
		return t, !t.IsZero()
	}
	return sysAccessTime(fi)
}

// Writable reports whether the current user can write to path (a file or
// directory), for the real filesystem. It is how cronen decides whether
// a hand-off needs the privilege command.
func (OSFS) Writable(path string) bool { return sysWritable(path) }

// Writable reports whether path was marked writable with SetWritable.
func (f *FakeFS) Writable(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writable[filepath.Clean(path)]
}

// SetWritable marks a path as writable by the current user.
func (f *FakeFS) SetWritable(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writable == nil {
		f.writable = map[string]bool{}
	}
	f.writable[filepath.Clean(path)] = true
}

// SetAccessTime sets the access time Stat reports for the file at path.
func (f *FakeFS) SetAccessTime(path string, t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.atimes == nil {
		f.atimes = map[string]time.Time{}
	}
	f.atimes[filepath.Clean(path)] = t
}
