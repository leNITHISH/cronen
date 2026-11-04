package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/rebuild"
	"github.com/leNITHISH/cronen/internal/summary"
)

// rebuildMsg carries the installed foreign packages the upgrade leaves
// needing a rebuild.
type rebuildMsg struct {
	gen      int
	findings []rebuild.Finding
	err      error
}

// wantsRebuild reports whether rebuild detection applies: it reads the
// transaction preview and the installed foreign packages.
func (m Model) wantsRebuild() bool {
	return m.cfg.Preview && m.cfg.AUR
}

// loadRebuild looks for foreign packages to rebuild once the preview is
// there. The ELF scan is the slow part, so it runs after the rest is shown.
func (m Model) loadRebuild(tx pacman.Transaction) tea.Cmd {
	ctx, gen := m.ctx(), m.gen
	src := summary.RebuildSources{
		Runner: m.opts.Runner, Opener: m.opts.Opener, FS: m.opts.FS, CacheDir: m.opts.CacheDir,
		ScanELF: m.cfg.RebuildScanELF, Fresh: m.freshLoad,
	}
	return func() tea.Msg {
		findings, err := summary.LoadRebuild(ctx, src, tx)
		return rebuildMsg{gen: gen, findings: findings, err: err}
	}
}

// clearRebuild forgets the findings, for when what they depend on is gone.
func (m *Model) clearRebuild() {
	m.rebuildSec = section{}
	m.rebuilds = nil
}
