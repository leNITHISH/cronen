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
