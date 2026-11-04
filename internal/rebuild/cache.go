package rebuild

import (
	"context"
	"encoding/json"

	"github.com/leNITHISH/cronen/internal/system"
)

const cacheVersion = 1

type cacheEntry struct {
	Version string `json:"version"`
	Needs   []Need `json:"needs"`
}

// Cache remembers the DT_NEEDED entries found in each foreign package, by
// package name and version: scanning every ELF file is the slow part, and a
// package's files only change with its version. The zero value is an empty
// cache.
type Cache struct {
	packages map[string]cacheEntry
	// Dirty is set when something was added since the cache was parsed.
	Dirty bool
}

// ParseCache reads a cache file. Anything unreadable (corrupt JSON, another
// format version) yields an empty cache; the cache is only ever an
// optimisation.
func ParseCache(data []byte) *Cache {
	var doc struct {
		Version  int                   `json:"version"`
		Packages map[string]cacheEntry `json:"packages"`
	}
	c := &Cache{packages: map[string]cacheEntry{}}
	if json.Unmarshal(data, &doc) != nil || doc.Version != cacheVersion {
		return c
	}
	for k, v := range doc.Packages {
		c.packages[k] = v
	}
	return c
}

// Marshal encodes the cache for ParseCache.
func (c *Cache) Marshal() []byte {
	if c.packages == nil {
		c.packages = map[string]cacheEntry{}
	}
	b, err := json.MarshalIndent(struct {
		Version  int                   `json:"version"`
		Packages map[string]cacheEntry `json:"packages"`
	}{cacheVersion, c.packages}, "", " ")
	if err != nil {
		// Only strings are marshalled.
		panic(err)
	}
	return append(b, '\n')
}

// Get returns the cached needs of name at version. An entry for another
// version is a miss.
func (c *Cache) Get(name, version string) ([]Need, bool) {
	e, ok := c.packages[name]
	if !ok || e.Version != version {
		return nil, false
	}
	return e.Needs, true
}

// Put records the needs of name at version, replacing any older entry.
func (c *Cache) Put(name, version string, needs []Need) {
	if c.packages == nil {
		c.packages = map[string]cacheEntry{}
	}
	c.packages[name] = cacheEntry{Version: version, Needs: needs}
	c.Dirty = true
}

// Keep drops the entries of packages that are no longer installed, so the
// cache doesn't grow forever. installed maps names to versions.
func (c *Cache) Keep(installed map[string]string) {
	for name := range c.packages {
		if _, ok := installed[name]; !ok {
			delete(c.packages, name)
			c.Dirty = true
		}
	}
}

// Needs returns the DT_NEEDED entries of a package's ELF files, from the
// cache when it has them for this version (and fresh is false), otherwise by
// scanning files and remembering the result.
func (c *Cache) Needs(ctx context.Context, op system.Opener, name, version string, files []string, fresh bool) ([]Need, error) {
	if !fresh {
		if needs, ok := c.Get(name, version); ok {
			return needs, nil
		}
	}
	needs, err := ScanNeeds(ctx, op, files)
	if err != nil {
		return nil, err
	}
	c.Put(name, version, needs)
	return needs, nil
}
