package ui

import (
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/system"
)

func TestRebuildShownInDetail(t *testing.T) {
	m := selectPackage(t, loaded(t, fixtureOptions(), 220, 30), "zstd")
	out := plain(m)
	if !strings.Contains(out, "Needs rebuild: gnome-shell-extension-legacy depends on libzstd.so.1") {
		t.Errorf("detail lacks the metadata reason:\n%s", out)
	}

	m = selectPackage(t, loaded(t, fixtureOptions(), 220, 30), "python")
	if out := plain(m); !strings.Contains(out, "rebuild these AUR packages: python-pywal") {
		t.Errorf("python's detail lacks the module-directory package:\n%s", out)
	}
	if mesa := plain(selectPackage(t, loaded(t, fixtureOptions(), 220, 30), "mesa")); strings.Contains(mesa, "Needs rebuild") {
		t.Errorf("mesa keeps its soname:\n%s", mesa)
	}
}

// Without the opener or with the scan off, the ELF signal is the only one
// that goes.
func TestRebuildELFScanOff(t *testing.T) {
	cfg := config.Default()
	cfg.RebuildScanELF = false
	opts := fixtureOptions()
	opts.Config = &cfg
	m := loaded(t, opts, 100, 30)
	if len(m.rebuilds) != 2 {
		t.Errorf("%d findings, want the metadata and module-dir ones", len(m.rebuilds))
	}
}

// A failing scan is reported in the status bar and leaves everything else
// as it was.
func TestRebuildFailureIsContained(t *testing.T) {
	opts := fixtureOptions()
	r := opts.Runner.(*system.FakeRunner)
	r.Set("pacman -Ql -- gnome-shell-extension-legacy neovim-git paru python-pywal spotify ttf-ms-fonts visual-studio-code-bin",
		system.FakeResult{Code: 1, Stderr: []byte("boom")})
	m := loaded(t, opts, 120, 30)
	if !strings.Contains(plain(m), "rebuild scan failed") {
		t.Errorf("status bar lacks the error:\n%s", plain(m))
	}
	if len(m.result.Packages) != 18 || len(m.result.Flagged()) != 11 {
		t.Errorf("%d updates, %d flagged; the rest should be unaffected", len(m.result.Packages), len(m.result.Flagged()))
	}
}

func TestRebuildOffWithoutPreviewOrAUR(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit func(*config.Config)
	}{
		{"no preview", func(c *config.Config) { c.Preview = false }},
		{"no aur", func(c *config.Config) { c.AUR = false }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			tt.edit(&cfg)
			opts := fixtureOptions()
			opts.Config = &cfg
			m := loaded(t, opts, 100, 30)
			if len(m.rebuilds) != 0 || m.rebuildSec.loading {
				t.Errorf("findings %d, loading %v", len(m.rebuilds), m.rebuildSec.loading)
			}
		})
	}
}
