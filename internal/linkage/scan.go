package linkage

import (
	"context"
	"errors"
	"io/fs"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/leNITHISH/cronen/internal/rebuild"
	"github.com/leNITHISH/cronen/internal/system"
)

// ldSoConf is where the loader's configuration starts.
const ldSoConf = "/etc/ld.so.conf"

// maxIncludeDepth bounds ld.so.conf includes.
const maxIncludeDepth = 8

// Package is an installed package and the files it owns.
type Package struct {
	Name, Version string
	Files         []string
}

// Broken is one library an ELF file needs and nothing provides.
type Broken struct {
	// Path is the ELF file.
	Path string `json:"path"`
	// Package and Version own it.
	Package string `json:"package"`
	Version string `json:"version"`
	// Lib is the missing DT_NEEDED entry.
	Lib string `json:"lib"`
}

// Result is what a scan found. Broken is sorted by package, path and
// library.
type Result struct {
	Broken []Broken
	// Packages and Files count what was looked at: packages, and ELF files
	// with libraries.
	Packages, Files int
	// Skipped counts ELF files left out by the skip list.
	Skipped int
}

// BrokenPackages lists the distinct packages with something broken.
func (r Result) BrokenPackages() []string {
	var names []string
	for _, b := range r.Broken {
		if !slices.Contains(names, b.Package) {
			names = append(names, b.Package)
		}
	}
	slices.Sort(names)
	return names
}

// Progress reports how far a scan is.
type Progress struct {
	Done, Total int
	// Package is the one just finished.
	Package string
}

// Options configure Scan.
type Options struct {
	Opener system.Opener
	FS     system.FS
	// Cache holds file data by package version. Nil means no cache.
	Cache *Cache
	// Fresh ignores cached file data (new data is still stored).
	Fresh bool
	// Skip are the patterns of directories not to check; see SkipPatterns.
	Skip []string
	// OnProgress, if set, is called after each package. Calls are serialised.
	OnProgress func(Progress)
	// Workers bounds how many packages are read at once. 0 picks a number
	// from the CPU count.
	Workers int
}

// LoadLdConf reads /etc/ld.so.conf and the files it includes (normally
// /etc/ld.so.conf.d/*.conf) through fsys, and returns the directories in
// order. A missing or unreadable file contributes nothing: Arch ships both,
// but a scan shouldn't fail for lack of them.
func LoadLdConf(fsys system.FS) []string {
	var dirs []string
	seen := map[string]bool{}
	var read func(path string, depth int)
	read = func(p string, depth int) {
		if depth > maxIncludeDepth || seen[p] {
			return
		}
		seen[p] = true
		data, err := fsys.ReadFile(p)
		if err != nil {
			return
		}
		ds, includes := ParseLdConf(data)
		dirs = append(dirs, ds...)
		for _, pat := range includes {
			if !strings.HasPrefix(pat, "/") {
				pat = "/etc/" + pat
			}
			files, err := fsys.Glob(pat)
			if err != nil {
				continue
			}
			for _, f := range files {
				// A directory named by the glob is not a config file;
				// reading it fails and contributes nothing.
				read(f, depth+1)
			}
		}
	}
	read(ldSoConf, 0)
	return dirs
}

// Scan reads the ELF files of pkgs and reports every library that can't be
// resolved. It checks ctx between packages and returns ctx.Err() when
// cancelled, along with the cache holding what was read so far.
//
// ELF files are only opened for paths rebuild.Candidate accepts, and
// files with no DT_NEEDED (static programs, non-ELF) are dropped at once, so
// the work is a header read per candidate. Packages are read concurrently.
func Scan(ctx context.Context, opt Options, pkgs []Package) (Result, error) {
	cache := opt.Cache
	if cache == nil {
		cache = &Cache{}
	}
	cache.Keep(pkgs)

	workers := opt.Workers
	if workers <= 0 {
		workers = min(8, max(2, runtime.NumCPU()))
	}
	files := make([][]File, len(pkgs))
	var (
		wg     sync.WaitGroup
		next   atomic.Int64
		done   int
		progMu sync.Mutex
	)
	for range workers {
		wg.Go(func() {
			for {
				i := int(next.Add(1)) - 1
				if i >= len(pkgs) {
					return
				}
				if ctx.Err() != nil {
					return
				}
				p := pkgs[i]
				var cached []File
				ok := false
				if !opt.Fresh {
					cached, ok = cache.Get(p.Name, p.Version)
				}
				if ok {
					files[i] = cached
				} else {
					files[i] = readPackage(ctx, opt.Opener, p)
					if ctx.Err() == nil {
						cache.Put(p.Name, p.Version, files[i])
					}
				}
				if opt.OnProgress != nil {
					progMu.Lock()
					done++
					opt.OnProgress(Progress{Done: done, Total: len(pkgs), Package: p.Name})
					progMu.Unlock()
				}
			}
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	return resolveAll(opt, pkgs, files), nil
}

// readPackage reads the ELF files of one package that have libraries.
func readPackage(ctx context.Context, op system.Opener, p Package) []File {
	var out []File
	for _, path := range p.Files {
		if ctx.Err() != nil {
			return nil
		}
		if !rebuild.Candidate(path) {
			continue
		}
		info, ok := rebuild.ReadELF(op, path)
		if !ok || len(info.Needed) == 0 {
			continue
		}
		out = append(out, File{Path: path, Class32: info.Class32, Needed: info.Needed, RunPath: info.RunPath})
	}
	return out
}

// resolveAll resolves every file's libraries, with the results of lookups
// shared between files that search the same directories.
func resolveAll(opt Options, pkgs []Package, files [][]File) Result {
	ldDirs := LoadLdConf(opt.FS)
	exists := map[string]bool{}
	s := Searcher{ConfDirs: ldDirs, Exists: func(p string) bool {
		if v, ok := exists[p]; ok {
			return v
		}
		_, err := opt.FS.Stat(p)
		// A directory cronen may not read says nothing: don't call it
		// broken. A missing path, a dangling link or a loop does.
		v := err == nil || errors.Is(err, fs.ErrPermission)
		exists[p] = v
		return v
	}}

	res := Result{Packages: len(pkgs)}
	for i, p := range pkgs {
		for _, f := range files[i] {
			if Skipped(f.Path, opt.Skip) {
				res.Skipped++
				continue
			}
			res.Files++
			for _, lib := range f.Needed {
				if !s.Resolve(f, lib) {
					res.Broken = append(res.Broken, Broken{Path: f.Path, Package: p.Name, Version: p.Version, Lib: lib})
				}
			}
		}
	}
	slices.SortFunc(res.Broken, func(a, b Broken) int {
		return strings.Compare(a.Package+"\x00"+a.Path+"\x00"+a.Lib, b.Package+"\x00"+b.Path+"\x00"+b.Lib)
	})
	return res
}
