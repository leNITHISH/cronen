package linkage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// cacheVersion 2: the verdict now holds only soname mismatches; a version 1
// cache may hold false positives and is dropped.
const cacheVersion = 2

// CacheFile is the name, inside the cache directory, of the scan cache.
const CacheFile = "linkage.json"

type cacheEntry struct {
	Version string `json:"version"`
	Files   []File `json:"files"`
}

// Saved is the verdict of the last scan, kept so --json and the TUI can
// report it without scanning again. It only counts while the installed
// packages are the ones it was made for (Signature).
type Saved struct {
	Signature string    `json:"signature"`
	At        time.Time `json:"at"`
	Broken    []Broken  `json:"broken"`
}

// Cache remembers the dynamic section of every ELF file with libraries, by
// package name and version: reading the files is the slow part, and a
// package's files only change with its version. It holds file data, not
// verdicts, because whether a library resolves depends on the other
// packages; resolving again is cheap. The zero value is an empty cache and
// it is safe for concurrent use.
type Cache struct {
	mu       sync.Mutex
	packages map[string]cacheEntry
	// Last is the verdict of the last completed scan, or nil.
	Last *Saved
	// Dirty is set when something changed since the cache was parsed.
	Dirty bool
}

// ParseCache reads a cache file. Anything unreadable (corrupt JSON, another
// format version) yields an empty cache; it only ever saves time.
func ParseCache(data []byte) *Cache {
	var doc struct {
		Version  int                   `json:"version"`
		Packages map[string]cacheEntry `json:"packages"`
		Last     *Saved                `json:"last"`
	}
	c := &Cache{packages: map[string]cacheEntry{}}
	if json.Unmarshal(data, &doc) != nil || doc.Version != cacheVersion {
		return c
	}
	for k, v := range doc.Packages {
		c.packages[k] = v
	}
	c.Last = doc.Last
	return c
}

// Marshal encodes the cache for ParseCache.
func (c *Cache) Marshal() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.packages == nil {
		c.packages = map[string]cacheEntry{}
	}
	b, err := json.Marshal(struct {
		Version  int                   `json:"version"`
		Packages map[string]cacheEntry `json:"packages"`
		Last     *Saved                `json:"last,omitempty"`
	}{cacheVersion, c.packages, c.Last})
	if err != nil {
		// Only strings, ints and times are marshalled.
		panic(err)
	}
	return append(b, '\n')
}

// Get returns the cached files of name at version. An entry for another
// version is a miss.
func (c *Cache) Get(name, version string) ([]File, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.packages[name]
	if !ok || e.Version != version {
		return nil, false
	}
	return e.Files, true
}

// Put records the files of name at version, replacing any older entry.
func (c *Cache) Put(name, version string, files []File) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.packages == nil {
		c.packages = map[string]cacheEntry{}
	}
	c.packages[name] = cacheEntry{Version: version, Files: files}
	c.Dirty = true
}

// Keep drops the entries of packages that are no longer installed.
func (c *Cache) Keep(pkgs []Package) {
	c.mu.Lock()
	defer c.mu.Unlock()
	have := map[string]bool{}
	for _, p := range pkgs {
		have[p.Name] = true
	}
	for name := range c.packages {
		if !have[name] {
			delete(c.packages, name)
			c.Dirty = true
		}
	}
}

// Save records the verdict of a completed scan for the packages scanned.
func (c *Cache) Save(pkgs []Package, res Result, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Last = &Saved{Signature: Signature(pkgs), At: at, Broken: res.Broken}
	c.Dirty = true
}

// Verdict returns the last scan's verdict if it was made for exactly the
// installed packages pkgs.
func (c *Cache) Verdict(pkgs []Package) (Saved, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Last == nil || c.Last.Signature != Signature(pkgs) {
		return Saved{}, false
	}
	return *c.Last, true
}

// Signature identifies a set of installed packages and versions.
func Signature(pkgs []Package) string {
	lines := make([]string, len(pkgs))
	for i, p := range pkgs {
		lines[i] = p.Name + " " + p.Version
	}
	slices.Sort(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:8])
}

func (b Broken) String() string {
	return fmt.Sprintf("%s (%s %s) needs %s, which no installed package provides", b.Path, b.Package, b.Version, b.Lib)
}
