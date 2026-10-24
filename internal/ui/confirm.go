package ui

import (
	"strings"

	"github.com/leNITHISH/cronen/internal/risk"
)

// confirmView is the upgrade confirmation: unread news requiring manual
// intervention first, then what will run.
func (m Model) confirmView(w int) string {
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
	add(" This runs: " + s.Bold.Render(strings.Join(m.cfg.UpgradeArgv(), " ")))
	if m.opts.Demo {
		add(" " + s.Subtle.Render("demo: nothing is run"))
	}
	add("")
	add(" Press " + s.Bold.Render("y") + " to upgrade, " + s.Bold.Render("n") + " or " + s.Bold.Render("esc") + " to cancel.")
	return strings.Join(lines, "\n")
}
