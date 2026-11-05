package linkage

import (
	"slices"
	"strings"
	"testing"
)

func kinds(fs []Fix) []FixKind {
	var out []FixKind
	for _, f := range fs {
		out = append(out, f.Kind)
	}
	return out
}

func TestSuggest(t *testing.T) {
	broken := []Broken{
		{Path: "/usr/bin/pacman", Package: "pacman", Lib: "libalpm.so.16"},
		{Path: "/usr/bin/paru", Package: "paru", Lib: "libalpm.so.15"},
	}
	foreign := map[string]bool{"paru": true}
	cacheDirs := []string{"/var/cache/pacman/pkg"}
	cached := map[string]string{"pacman": "/var/cache/pacman/pkg/pacman-7.0.0-1-x86_64.pkg.tar.zst"}

	t.Run("repo updates pending: no rebuild is offered", func(t *testing.T) {
		got := Suggest(FixInput{Broken: broken, Foreign: foreign, PendingRepo: 3, AURHelper: "paru", Priv: []string{"sudo"}})
		if want := []FixKind{FixUpgrade, FixUpgradeFirst}; !slices.Equal(kinds(got), want) {
			t.Fatalf("kinds = %v, want %v", kinds(got), want)
		}
		for _, f := range got {
			if f.Kind == FixRebuild || (f.Kind == FixUpgradeFirst && f.Argv != nil) {
				t.Errorf("offered a rebuild command: %+v", f)
			}
		}
		if !strings.Contains(got[1].Text, "upgrade first, then rebuild") {
			t.Errorf("text = %q", got[1].Text)
		}
	})
	t.Run("pending updates unknown: no rebuild is offered", func(t *testing.T) {
		got := Suggest(FixInput{Broken: broken[1:], Foreign: foreign, PendingUnknown: true, AURHelper: "paru"})
		if want := []FixKind{FixUpgradeFirst}; !slices.Equal(kinds(got), want) {
			t.Fatalf("kinds = %v, want %v", kinds(got), want)
		}
		if got[0].Argv != nil {
			t.Errorf("offered a command: %+v", got[0])
		}
	})
	t.Run("nothing pending: rebuild with the helper", func(t *testing.T) {
		got := Suggest(FixInput{Broken: broken, Foreign: foreign, AURHelper: "paru", Priv: []string{"sudo"}})
		if want := []FixKind{FixUpgrade, FixRebuild}; !slices.Equal(kinds(got), want) {
			t.Fatalf("kinds = %v, want %v", kinds(got), want)
		}
		if want := []string{"paru", "-S", "--", "paru"}; !slices.Equal(got[1].Argv, want) {
			t.Errorf("Argv = %v, want %v", got[1].Argv, want)
		}
	})
	t.Run("no helper: described, not handed off", func(t *testing.T) {
		got := Suggest(FixInput{Broken: broken[1:], Foreign: foreign})
		if len(got) != 1 || got[0].Kind != FixRebuild || got[0].Argv != nil {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("a cached package can be reinstalled", func(t *testing.T) {
		got := Suggest(FixInput{Broken: broken[:1], Cached: cached, CacheDirs: cacheDirs, Priv: []string{"sudo"}})
		if want := []FixKind{FixUpgrade, FixReinstall}; !slices.Equal(kinds(got), want) {
			t.Fatalf("kinds = %v, want %v", kinds(got), want)
		}
		want := []string{"sudo", "pacman", "-U", "--", cached["pacman"]}
		if !slices.Equal(got[1].Argv, want) {
			t.Errorf("Argv = %v, want %v", got[1].Argv, want)
		}
	})
	t.Run("a cached file outside the cache dirs is not offered", func(t *testing.T) {
		got := Suggest(FixInput{Broken: broken[:1], Cached: map[string]string{"pacman": "/tmp/pacman-7-1-x86_64.pkg.tar.zst"}, CacheDirs: cacheDirs, Priv: []string{"sudo"}})
		if want := []FixKind{FixUpgrade}; !slices.Equal(kinds(got), want) {
			t.Errorf("kinds = %v, want %v", kinds(got), want)
		}
	})
	t.Run("nothing broken, nothing suggested", func(t *testing.T) {
		if got := Suggest(FixInput{}); len(got) != 0 {
			t.Errorf("got %+v", got)
		}
	})
}
