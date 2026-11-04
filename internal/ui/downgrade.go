package ui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/ops"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/system"
)

// maxCandidates caps the versions the downgrade picker lists.
const maxCandidates = 15

// versionsMsg is the answer to the cache lookup D starts: the cached
// versions older than the installed one, newest first.
type versionsMsg struct {
	name, from string
	dirs       []string
	versions   []pacman.CachedPackage
	// dateErr says why the build dates are missing; the list still works.
	dateErr error
	err     error
}

// loadVersions lists the package cache for name and keeps the versions older
// than from. The build dates come from one read-only `pacman -Qip`.
func loadVersions(ctx context.Context, r system.Runner, fsys system.FS, extra []string, home string, loc *time.Location, name, from string) tea.Cmd {
	return func() tea.Msg {
		dirs := pacman.ResolveCacheDirs(fsys, extra, home)
		msg := versionsMsg{name: name, from: from, dirs: dirs}
		all, err := pacman.ScanCache(fsys, dirs)
		if err != nil && len(all) == 0 {
			msg.err = err
			return msg
		}
		older, err := pacman.OlderCached(ctx, r, all, name, from)
		if err != nil {
			msg.err = err
			return msg
		}
		if len(older) > maxCandidates {
			older = older[:maxCandidates]
		}
		msg.versions, msg.dateErr = pacman.ReadBuildDates(ctx, r, dirs, older, loc)
		return msg
	}
}

// startDowngrade looks for older versions of name in the package cache.
func (m *Model) startDowngrade(name string) tea.Cmd {
	switch {
	case m.opts.Runner == nil || m.opts.FS == nil || !m.cfg.History:
		m.flash("downgrading isn't available here (it needs history = true)", true)
		return nil
	case m.localDB == nil:
		m.flash("the installed packages aren't loaded yet", false)
		return nil
	}
	pkg, ok := m.localDB.Lookup(name)
	if !ok {
		m.flash(name+" isn't installed, so there is nothing to downgrade", false)
		return nil
	}
	if !ops.ValidName(name) {
		m.flash("refusing package name "+name, true)
		return nil
	}
	m.flash("looking for older versions of "+name+" in the package cache…", false)
	return loadVersions(m.ctx(), m.opts.Runner, m.opts.FS, m.cfg.ExtraCacheDirs, m.opts.Home, m.location(), name, pkg.Version)
}

func (m *Model) handleVersions(msg versionsMsg) {
	switch {
	case msg.err != nil:
		m.flash("couldn't read the package cache: "+msg.err.Error(), true)
		return
	case len(msg.versions) == 0:
		m.flash("no version of "+msg.name+" older than "+msg.from+" is in the package cache", false)
		return
	}
	m.flash("", false)
	m.act = &pendingAction{
		kind: ops.KindDowngrade, names: []string{msg.name}, from: msg.from,
		picking: true, candidates: msg.versions, cacheDirs: msg.dirs, dateErr: msg.dateErr,
	}
	m.actionReturn = m.screen
	m.screen, m.focus = screenConfirm, focusList
}

// pickVersion starts the dry run for the chosen version.
func (m *Model) pickVersion() tea.Cmd {
	a := m.act
	c := a.candidates[a.pick]
	a.picking, a.target = false, c
	return m.rerunDowngradeDry()
}

// rerunDowngradeDry runs `pacman -Up` for the chosen file, after checking
// the file is a regular file inside a cache directory.
func (m *Model) rerunDowngradeDry() tea.Cmd {
	a := m.act
	a.seq++
	a.loading, a.typed = true, ""
	fail := func(err error) tea.Cmd {
		a.loading = false
		a.plan = ops.Plan{Kind: ops.KindDowngrade, Failure: err.Error()}
		return nil
	}
	if err := pacman.CheckCacheFile(m.opts.FS, a.target.Path, a.cacheDirs); err != nil {
		return fail(err)
	}
	argv, err := ops.DowngradeDryArgv([]string{a.target.Path}, a.cacheDirs)
	if err != nil {
		return fail(err)
	}
	return dryRun(m.ctx(), m.opts.Runner, a.seq, argv, false)
}

func (m *Model) buildDowngradePlan(msg dryRunMsg) {
	a := m.act
	plan, err := ops.BuildDowngradePlan(ops.DowngradeInput{
		Targets: []ops.DowngradeTarget{{
			Name: a.names[0], From: a.from, Version: a.target.Version, Path: a.target.Path, HasSig: a.target.HasSig,
		}},
		Priv: m.cfg.PrivilegeArgv(), Protected: m.cfg.EffectiveProtected(), CacheDirs: a.cacheDirs, DB: m.localDB,
		Out: msg.out, Stderr: msg.stderr, Failed: msg.err != nil, RunErr: msg.err,
	})
	if err != nil {
		plan = ops.Plan{Kind: ops.KindDowngrade, Failure: err.Error()}
	}
	a.plan = plan
}

// recheckDowngrade looks at the file again right before the hand-off.
func (m *Model) recheckDowngrade() error {
	a := m.act
	return pacman.CheckCacheFile(m.opts.FS, a.target.Path, a.cacheDirs)
}

// handleDowngradeKey handles the keys of the downgrade modal.
func (m Model) handleDowngradeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	a := m.act
	s := msg.String()
	if s == "ctrl+c" {
		return m, tea.Quit
	}
	if a.picking {
		switch s {
		case "j", "down":
			a.pick = clamp(a.pick+1, 0, len(a.candidates)-1)
		case "k", "up":
			a.pick = clamp(a.pick-1, 0, len(a.candidates)-1)
		case "g", "home":
			a.pick = 0
		case "G", "end":
			a.pick = len(a.candidates) - 1
		case "enter":
			return m, m.pickVersion()
		}
		return m, nil
	}
	if a.loading || a.plan.Blocked() {
		return m, nil
	}
	tok := a.plan.Token()
	if tok == "" {
		if s == "y" {
			return m, m.runAction()
		}
		return m, nil
	}
	switch {
	case s == "enter":
		if a.typed == tok {
			return m, m.runAction()
		}
		m.flash("type "+tok+" exactly to confirm", true)
	case s == "backspace":
		if a.typed != "" {
			a.typed = a.typed[:len(a.typed)-1]
		}
	case msg.Text != "":
		a.typed += msg.Text
	}
	return m, nil
}

// downgradeView is the downgrade modal: the version picker, then the dry
// run's plan, or its error.
func (m Model) downgradeView(w int) string {
	s, a := m.styles, m.act
	var lines []string
	add := func(text string) {
		lines = append(lines, wrapIndent(text, w, 4))
	}
	add(" " + s.Bold.Render(actionTitle(a)))
	add("")
	add(" Installed: " + a.names[0] + " " + s.Subtle.Render(a.from))
	add("")
	if a.picking {
		add(" " + s.Bold.Render("Older versions in the package cache, newest first"))
		for i, c := range a.candidates {
			cursor := "   "
			if i == a.pick {
				cursor = " ▸ "
			}
			built := "build date unknown"
			if !c.BuildDate.IsZero() {
				built = "built " + c.BuildDate.Format("2006-01-02")
			}
			sig := s.Warn.Render("no .sig")
			if c.HasSig {
				sig = "signed (.sig present)"
			}
			add(cursor + c.Version + "  " + s.Subtle.Render(built) + "  " + sig)
		}
		if a.dateErr != nil {
			add("")
			add(" " + s.Subtle.Render("build dates unavailable: "+a.dateErr.Error()))
		}
		add("")
		add(" " + s.Bold.Render("j/k") + " move · " + s.Bold.Render("enter") + " choose · " + s.Bold.Render("esc") + " cancel")
		return strings.Join(lines, "\n")
	}

	add(" Chosen: " + a.target.Path)
	add(" Dry run: " + s.Subtle.Render(strings.Join(quoteFormat(dryDisplay(a)), " ")))
	add("")
	switch {
	case a.loading:
		add(" " + s.Subtle.Render(m.spinner.View()+" running pacman's dry run…"))
		return strings.Join(lines, "\n")
	case a.plan.Blocked():
		add(" " + s.Error.Render("✗ pacman's dry run failed; nothing will be run"))
		add("   " + a.plan.Failure)
		for _, wn := range a.plan.Warnings {
			add("   " + s.Warn.Render("⚠ "+wn))
		}
		add("")
		add(" " + s.Subtle.Render("esc cancels"))
		return strings.Join(lines, "\n")
	}
	p := a.plan
	add(" This runs: " + s.Bold.Render(strings.Join(p.Command, " ")))
	if m.opts.Demo {
		add(" " + s.Subtle.Render("demo: nothing is run"))
	}
	add("")
	var sel, pulled []ops.Item
	for _, it := range p.Items {
		if it.Selected {
			sel = append(sel, it)
		} else {
			pulled = append(pulled, it)
		}
	}
	if len(sel) > 0 {
		add(" " + s.Bold.Render("Downgrading"))
		for _, it := range sel {
			row := "   " + it.Name + " " + s.Subtle.Render(it.From+" → "+it.Version)
			if it.Protected {
				row += " " + s.Critical.Render("protected")
			}
			add(row)
		}
	}
	if len(pulled) > 0 {
		add(" " + s.Bold.Render("Pulled in by pacman ("+itoa(len(pulled))+")"))
		for i, it := range pulled {
			if i == maxListed {
				add("   " + s.Subtle.Render("… and "+itoa(len(pulled)-maxListed)+" more"))
				break
			}
			add("   " + it.Name + " " + s.Subtle.Render(it.Version))
		}
	}
	if len(p.Warnings) > 0 {
		add("")
		for _, wn := range p.Warnings {
			add(" " + s.Warn.Render("⚠ "+wn))
		}
	}
	add("")
	if tok := p.Token(); tok != "" {
		add(" " + s.Critical.Render("Protected package: type ") + s.Bold.Render(tok) + s.Critical.Render(" and press enter to downgrade it."))
		add("   > " + a.typed + "▏")
	} else {
		add(" Press " + s.Bold.Render("y") + " to downgrade, " + s.Bold.Render("esc") + " to cancel.")
	}
	return strings.Join(lines, "\n")
}

// dryDisplay is the dry run for display, built from the chosen file.
func dryDisplay(a *pendingAction) []string {
	argv, err := ops.DowngradeDryArgv([]string{a.target.Path}, a.cacheDirs)
	if err != nil {
		return nil
	}
	return argv
}
