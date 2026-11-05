package ui

import (
	"strings"

	"github.com/leNITHISH/cronen/internal/ops"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/risk"
)

// confirmView is the upgrade confirmation: unread news requiring manual
// intervention first, then what will run.
func (m Model) confirmView(w int) string {
	if m.act != nil {
		return m.actionView(w)
	}
	s := m.styles
	var lines []string
	add := func(text string) {
		lines = append(lines, wrapIndent(text, w, 4))
	}
	add(" " + s.Bold.Render("Upgrade the system?"))
	add("")
	if n := len(m.result.Interventions); n > 0 {
		add(" " + s.Critical.Render(severityIcon(risk.Critical)+" Read first: "+plural(n, "unread news item requires", "unread news items require")+" manual intervention"))
		for _, it := range m.result.Interventions {
			add("   " + s.Subtle.Render(it.Published.In(m.location()).Format("2006-01-02")) + "  " + s.Bold.Render(it.Title))
		}
		add("   " + s.Subtle.Render("press esc, then n to read it"))
		add("")
	}

	m.addPreflight(add)

	var counts []string
	if m.repoSec.loading || m.aurSec.loading {
		counts = append(counts, "updates still loading")
	} else {
		counts = append(counts, plural(len(m.result.Packages), "update", "updates"))
	}
	if m.newsDecided {
		counts = append(counts, itoa(len(m.result.Flagged()))+" flagged")
	}
	if m.result.RebootRequired {
		counts = append(counts, "kernel update: reboot afterwards")
	}
	add(" " + strings.Join(counts, " · "))
	add("")
	m.addBandwidth(add)
	add("")
	add(" This runs: " + s.Bold.Render(strings.Join(m.cfg.UpgradeArgv(), " ")))
	if m.opts.Demo {
		add(" " + s.Subtle.Render("demo: nothing is run"))
	}
	add("")
	add(" Press " + s.Bold.Render("y") + " to upgrade, " + s.Bold.Render("n") + " or " + s.Bold.Render("esc") + " to cancel.")
	return strings.Join(lines, "\n")
}

// maxListed caps each package list in the action modal.
const maxListed = 6

// actionView is the confirmation of a write action: the exact command, who
// is affected, what it frees, and the warnings. A plan whose dry run failed
// shows pacman's error and no way to proceed.
func (m Model) actionView(w int) string {
	s, a := m.styles, m.act
	if a.kind == ops.KindDowngrade {
		return m.downgradeView(w)
	}
	if a.kind == ops.KindPrefetch {
		return m.prefetchView(w)
	}
	var lines []string
	add := func(text string) {
		lines = append(lines, wrapIndent(text, w, 4))
	}
	add(" " + s.Bold.Render(actionTitle(a)))
	add("")
	if a.kind == ops.KindPacdiff {
		add(" This runs: " + s.Bold.Render(strings.Join(a.plan.Command, " ")))
		add(" " + s.Subtle.Render("pacdiff is interactive: it asks what to do with each file; there is no dry run."))
		add("")
		add(" " + s.Bold.Render(plural(len(a.plan.Files), "file", "files")))
		for _, f := range a.plan.Files {
			add("   " + f)
		}
		add("")
		add(" Press " + s.Bold.Render("y") + " to run it, " + s.Bold.Render("esc") + " to cancel.")
		return strings.Join(lines, "\n")
	}

	add(" Dry run: " + s.Subtle.Render(strings.Join(quoteFormat(ops.DryArgvOrNil(a.opts, a.names)), " ")))
	add(" Options: " + optionLine(a.opts, a.plan.Token() != ""))
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
		add(" " + s.Subtle.Render("esc cancels; a toggle above reruns the dry run"))
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
	list := func(title string, items []ops.Item) {
		if len(items) == 0 {
			return
		}
		add(" " + s.Bold.Render(title+" ("+itoa(len(items))+")"))
		for i, it := range items {
			if i == maxListed {
				add("   " + s.Subtle.Render("… and "+itoa(len(items)-maxListed)+" more"))
				break
			}
			row := "   " + it.Name + " " + s.Subtle.Render(it.Version)
			if it.Size > 0 {
				row += " " + s.Subtle.Render(pacman.FormatSize(it.Size))
			}
			if it.Protected {
				row += " " + s.Critical.Render("protected")
			}
			add(row)
		}
	}
	list("Selected", sel)
	list("Pulled in by pacman", pulled)
	if p.Freed > 0 {
		add("")
		add(" Frees " + s.Bold.Render(pacman.FormatSize(p.Freed)))
	}
	if len(p.Pacsave) > 0 {
		add(" " + s.Warn.Render("Config files saved as .pacsave if modified: ") + strings.Join(p.Pacsave, ", "))
	}
	if len(p.Warnings) > 0 {
		add("")
		for _, wn := range p.Warnings {
			add(" " + s.Warn.Render("⚠ "+wn))
		}
	}
	add("")
	if tok := p.Token(); tok != "" {
		add(" " + s.Critical.Render("Protected package: type ") + s.Bold.Render(tok) + s.Critical.Render(" and press enter to remove it."))
		add("   > " + a.typed + "▏")
	} else {
		add(" Press " + s.Bold.Render("y") + " to remove, " + s.Bold.Render("esc") + " to cancel.")
	}
	return strings.Join(lines, "\n")
}

// optionLine shows the two toggles and their keys.
func optionLine(o ops.RemoveOptions, typing bool) string {
	box := func(on bool) string {
		if on {
			return "☑"
		}
		return "☐"
	}
	ks, kn := "s", "n"
	if typing {
		ks, kn = "ctrl+s", "ctrl+n"
	}
	return ks + " " + box(o.Recursive) + " also remove unneeded dependencies (-s) · " +
		kn + " " + box(o.NoSave) + " no .pacsave backups (-n)"
}

// quoteFormat quotes the --print-format argument for display only; the
// command itself is an argv, never a string.
func quoteFormat(argv []string) []string {
	out := append([]string(nil), argv...)
	for i, a := range out {
		if strings.Contains(a, " ") {
			out[i] = "'" + a + "'"
		}
	}
	return out
}
