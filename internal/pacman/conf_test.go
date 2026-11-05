package pacman

import (
	"slices"
	"testing"
)

func TestCacheDirs(t *testing.T) {
	tests := []struct {
		name string
		conf string
		want []string
	}{
		{"empty", "", []string{DefaultCacheDir}},
		{"default", "[options]\nHoldPkg = pacman glibc\n#CacheDir = /nope/\n", []string{DefaultCacheDir}},
		{"one", "[options]\nCacheDir = /mnt/cache/\n", []string{"/mnt/cache/"}},
		{"several on one line and several lines", "[options]\nCacheDir = /a/ /b/\nCacheDir=/c/ # comment\n", []string{"/a/", "/b/", "/c/"}},
		{"other sections ignored", "[options]\nIgnorePkg = foo\n[core]\nCacheDir = /wrong/\nInclude = /etc/pacman.d/mirrorlist\n", []string{DefaultCacheDir}},
		{"options after other sections", "[core]\nInclude = x\n[options]\nCacheDir = /late/\n", []string{"/late/"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CacheDirs([]byte(tt.conf)); !slices.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParallelDownloads(t *testing.T) {
	tests := []struct {
		name    string
		conf    string
		want    int
		wantSet bool
	}{
		{"missing", "[options]\nCacheDir = /x\n", 1, false},
		{"commented out", "[options]\n#ParallelDownloads = 5\n", 1, false},
		{"set", "[options]\nParallelDownloads = 5\n", 5, true},
		{"trailing comment", "[options]\nParallelDownloads=8 # fast\n", 8, true},
		{"one is set but sequential", "[options]\nParallelDownloads = 1\n", 1, true},
		{"zero is not a positive number", "[options]\nParallelDownloads = 0\n", 1, false},
		{"junk", "[options]\nParallelDownloads = many\n", 1, false},
		{"other section is ignored", "[options]\n[core]\nParallelDownloads = 9\n", 1, false},
		{"empty file", "", 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, set := ParallelDownloads([]byte(tt.conf))
			if got != tt.want || set != tt.wantSet {
				t.Errorf("ParallelDownloads = %d, %v; want %d, %v", got, set, tt.want, tt.wantSet)
			}
		})
	}
}
