package ui

import (
	"cmp"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/archweb"
	"github.com/leNITHISH/cronen/internal/aur"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/risk"
)

// entry is one selectable package in the updates list.
type entry struct {
	// key identifies the entry across rebuilds: "repo:name" or "aur:name".
	key string
	pkg risk.Package
	// aur is set for AUR packages.
	aur *aur.Package
	// kind says which part of the transaction the entry is.
	kind entryKind
	// newPkg and removed are set for entryNew and entryRemoved.
	newPkg  *pacman.NewPackage
	removed *pacman.RemovedPackage
}

// entryKind tells apart the entries that come from the transaction preview.
type entryKind int

const (
	entryUpdate entryKind = iota
	entryNew
	entryRemoved
	entryHeld
)

// pending reports whether the entry is an update rather than an installed
// AUR package shown only for its status.
func (e entry) pending() bool {
	return e.pkg.NewVersion != ""
}

type rowKind int

const (
	rowSection rowKind = iota
	rowNote
	rowEntry
)

// row is one line of the updates list.
type row struct {
	kind rowKind
	text string
	// spin prefixes a note with the spinner.
	spin bool
	err  bool
	// entry indexes updatesPane.entries for rowEntry rows.
	entry int
}

// updatesPane is the grouped package list on the updates screen. The
// selection is tracked by key, so it stays on the same package when flags
// arrive and packages move between sections.
type updatesPane struct {
	entries []entry
	rows    []row
	sel     int
	selKey  string
	offset  int
	// moved reports whether the user has moved the selection. Until then
	// it stays on the first, most severe entry as data arrives.
	moved bool
}

func (p *updatesPane) selected() (entry, bool) {
	if p.sel < 0 || p.sel >= len(p.entries) {
		return entry{}, false
	}
	return p.entries[p.sel], true
}

func (p *updatesPane) move(delta int) {
	if len(p.entries) == 0 {
		return
	}
	// Clamp before adding so huge deltas (g/G) can't overflow.
	delta = clamp(delta, -len(p.entries), len(p.entries))
	p.sel = clamp(p.sel+delta, 0, len(p.entries)-1)
	p.selKey = p.entries[p.sel].key
	p.moved = true
}

// scroll adjusts the offset so the selected row is visible in h lines.
func (p *updatesPane) scroll(h int) {
	if h <= 0 {
		return
	}
	selRow := -1
	for i, r := range p.rows {
		if r.kind == rowEntry && r.entry == p.sel {
			selRow = i
			break
		}
	}
	switch {
	case selRow < 0:
	case p.sel == 0:
		// Keep the first section header in view.
		p.offset = 0
	case selRow < p.offset:
		p.offset = selRow
	case selRow >= p.offset+h:
		p.offset = selRow - h + 1
	}
	p.offset = clamp(p.offset, 0, max(0, len(p.rows)-h))
}

// rebuild regroups the packages into sections and restores the selection.
func (m *Model) rebuildUpdates() {
	p := &m.updates
	aurByName := map[string]*aur.Package{}
	for i := range m.aurPkgs {
		aurByName[m.aurPkgs[i].Name] = &m.aurPkgs[i]
	}

	var flagged, repo, aurEntries []entry
	for _, pkg := range m.result.Packages {
		e := entry{key: "repo:" + pkg.Name, pkg: pkg}
		if pkg.AUR {
			e.key, e.aur = "aur:"+pkg.Name, aurByName[pkg.Name]
		}
		switch {
		case len(pkg.Flags) > 0:
			flagged = append(flagged, e)
		case len(pkg.Acknowledged) > 0 && m.hideAcked:
			// Acknowledged packages are hidden on request.
			continue
		case pkg.AUR:
			aurEntries = append(aurEntries, e)
		default:
			repo = append(repo, e)
		}
	}
	slices.SortStableFunc(flagged, func(a, b entry) int {
		return cmp.Compare(b.pkg.Severity(), a.pkg.Severity())
	})
	// Installed AUR packages with problems are worth showing even without
	// a pending update, and so are VCS packages, whose updates only the AUR
	// helper can tell. Debug splits are hidden unless asked for.
	var vcs, debug int
	for i := range m.aurPkgs {
		a := &m.aurPkgs[i]
		switch {
		case a.Pending:
			continue
		case a.Kind == aur.KindVCS:
			vcs++
		case a.Kind == aur.KindDebug:
			if !m.cfg.ShowDebug {
				continue
			}
			debug++
		case aurStatus(*a) == "":
			continue
		}
		aurEntries = append(aurEntries, entry{
			key: "aur:" + a.Name,
			pkg: risk.Package{Update: risk.Update{Name: a.Name, OldVersion: a.InstalledVersion, AUR: true}},
			aur: a,
		})
	}

	// The filter hides entries but keeps the sections, so it's clear
	// where the matches are and what is hidden.
	q := m.filterQuery()
	keep := func(es []entry) []entry {
		if q == "" {
			return es
		}
		var out []entry
		for _, e := range es {
			if strings.Contains(strings.ToLower(e.pkg.Name), q) {
				out = append(out, e)
			}
		}
		return out
	}
	hidden := map[string]bool{}
	for name, es := range map[string][]entry{"flagged": flagged, "repo": repo, "aur": aurEntries} {
		hidden[name] = len(keep(es)) < len(es)
	}
	flagged, repo = keep(flagged), keep(repo)
	if q != "" {
		vcs, debug = 0, 0
		aurEntries = keep(aurEntries)
		for _, e := range aurEntries {
			switch {
			case e.pending():
			case e.aur.Kind == aur.KindVCS:
				vcs++
			case e.aur.Kind == aur.KindDebug:
				debug++
			}
		}
	}

	var newEntries, removedEntries, heldEntries []entry
	if m.previewed {
		for i := range m.tx.New {
			n := &m.tx.New[i]
			newEntries = append(newEntries, entry{
				key: "new:" + n.Name, kind: entryNew, newPkg: n,
				pkg: risk.Package{Update: risk.Update{Name: n.Name, NewVersion: n.Version}},
			})
		}
		for i := range m.tx.Removed {
			r := &m.tx.Removed[i]
			removedEntries = append(removedEntries, entry{
				key: "removed:" + r.Name, kind: entryRemoved, removed: r,
				pkg: risk.Package{Update: risk.Update{Name: r.Name, OldVersion: r.Version}},
			})
		}
		for _, h := range m.result.Held {
			heldEntries = append(heldEntries, entry{key: "held:" + h.Name, kind: entryHeld, pkg: h})
		}
		newEntries, removedEntries, heldEntries = keep(newEntries), keep(removedEntries), keep(heldEntries)
	}

	p.entries = p.entries[:0:0]
	p.rows = nil
	addEntries := func(es []entry) {
		for _, e := range es {
			p.rows = append(p.rows, row{kind: rowEntry, entry: len(p.entries)})
			p.entries = append(p.entries, e)
		}
	}
	note := func(text string, spin, err bool) {
		if !err || ansi.StringWidth(text) <= m.listWidth()-1 {
			p.rows = append(p.rows, row{kind: rowNote, text: text, spin: spin, err: err})
			return
		}
		// Errors wrap rather than truncate: the end often says what to do.
		for l := range strings.SplitSeq(wrapIndent(text, m.listWidth()-1, 2), "\n") {
			p.rows = append(p.rows, row{kind: rowNote, text: l, err: true})
		}
	}

	p.rows = append(p.rows, row{kind: rowSection, text: "NEEDS ATTENTION (" + itoa(len(flagged)) + ")"})
	addEntries(flagged)
	switch {
	case m.newsSec.loading || m.logSec.loading:
		note("checking news", true, false)
	case m.newsSec.err != nil:
		note(newsErrorNote, false, true)
	case len(flagged) == 0 && hidden["flagged"]:
		note("no matches", false, false)
	case len(flagged) == 0 && !m.repoSec.loading && !m.aurSec.loading:
		note("nothing flagged", false, false)
	}

	addSection := func(title string, s section, es []entry, count, loadingText, emptyText string) {
		if len(es) == 0 && hidden[strings.ToLower(title)] {
			emptyText = "no matches"
		}
		if s.loading && len(es) == 0 {
			p.rows = append(p.rows, row{kind: rowSection, text: title})
		} else {
			p.rows = append(p.rows, row{kind: rowSection, text: title + " (" + count + ")"})
		}
		addEntries(es)
		switch {
		case s.loading:
			note(loadingText, true, false)
		case s.err != nil:
			note("✗ "+s.err.Error(), false, true)
		case len(es) == 0:
			note(emptyText, false, false)
		}
	}
	addSection("REPO", m.repoSec, repo, itoa(len(repo)), "running checkupdates", "up to date")
	if m.cfg.AUR {
		// VCS packages and debug splits are listed but aren't updates.
		count := itoa(len(aurEntries) - vcs - debug)
		if vcs > 0 {
			count += " + " + itoa(vcs) + " VCS"
		}
		if debug > 0 {
			count += " + " + itoa(debug) + " debug"
		}
		addSection("AUR", m.aurSec, aurEntries, count, "querying the AUR", "up to date")
	}

	if m.cfg.Preview && !m.repoSec.loading && m.repoSec.err == nil {
		m.addPreviewRows(p, addEntries, note, newEntries, removedEntries, heldEntries)
	}
	m.addPreflightRows(p, note)

	m.addFeedErrorRows(p, note)

	p.sel = slices.IndexFunc(p.entries, func(e entry) bool { return e.key == p.selKey })
	if p.sel < 0 || !p.moved {
		p.sel = 0
	}
	if len(p.entries) == 0 {
		p.sel, p.selKey = -1, ""
	} else {
		p.selKey = p.entries[p.sel].key
	}
}

// addPreviewRows adds the transaction preview's groups after the AUR: NEW,
// REMOVED and HELD while it is there, or a section with a spinner or the
// error while it isn't.
func (m *Model) addPreviewRows(p *updatesPane, addEntries func([]entry), note func(string, bool, bool), newEntries, removed, held []entry) {
	switch {
	case m.previewed:
		for _, g := range []struct {
			title string
			es    []entry
		}{{"NEW", newEntries}, {"REMOVED", removed}, {"HELD", held}} {
			if len(g.es) > 0 {
				p.rows = append(p.rows, row{kind: rowSection, text: g.title + " (" + itoa(len(g.es)) + ")"})
				addEntries(g.es)
			}
		}
	case m.previewSec.loading:
		p.rows = append(p.rows, row{kind: rowSection, text: "TRANSACTION"})
		note("previewing the transaction", true, false)
	case m.previewSec.err != nil:
		p.rows = append(p.rows, row{kind: rowSection, text: "TRANSACTION"})
		note("✗ "+m.previewSec.err.Error(), false, true)
	}
}

// aurStatus describes an AUR package's problems, or returns "".
func aurStatus(p aur.Package) string {
	switch {
	case p.Missing():
		return "not in AUR"
	case p.Info == nil:
		return ""
	case p.Info.Orphaned():
		return "orphaned"
	case p.Info.FlaggedOutOfDate():
		return "out of date"
	}
	return ""
}

// aurTag is what the list shows in place of versions for an AUR package that
// isn't pending: its kind and any problem.
func aurTag(p aur.Package) string {
	var parts []string
	switch p.Kind {
	case aur.KindVCS:
		parts = append(parts, "VCS")
	case aur.KindDebug:
		parts = append(parts, "debug")
	}
	if st := aurStatus(p); st != "" {
		parts = append(parts, st)
	}
	return strings.Join(parts, " · ")
}

func (m Model) updatesListView(w, h int) string {
	p := m.updates
	end := min(len(p.rows), p.offset+h)
	lines := make([]string, 0, h)
	for _, r := range p.rows[p.offset:end] {
		switch r.kind {
		case rowSection:
			lines = append(lines, " "+m.styles.Section.Render(r.text))
		case rowNote:
			text := r.text
			if r.spin {
				text = m.spinner.View() + " " + text + "…"
			}
			indent, style := "   ", m.styles.Subtle
			if r.err {
				// Less indent, so a one-line error fits a wide pane whole.
				indent, style = " ", m.styles.Error
			}
			lines = append(lines, indent+style.Render(text))
		case rowEntry:
			lines = append(lines, m.entryLine(p.entries[r.entry], r.entry == p.sel, w))
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) entryLine(e entry, selected bool, w int) string {
	cursor := "  "
	if selected {
		cursor = "▸ "
	}
	// newPart is never truncated; the old version and the name give way.
	var oldPart, newPart string
	switch {
	case e.kind == entryRemoved:
		newPart = "replaced"
	case e.pending():
		o, n := versionParts(e.pkg.OldVersion, e.pkg.NewVersion)
		if e.pkg.OldVersion == "" {
			o = ""
		}
		oldPart, newPart = o, "→"+n
	case e.aur != nil:
		newPart = aurTag(*e.aur)
	}
	sev := e.pkg.Severity()
	icon := severityIcon(sev)
	if sev == risk.None && e.aur != nil && aurStatus(*e.aur) != "" {
		icon = "?"
	}
	if sev == risk.None && len(e.pkg.Acknowledged) > 0 {
		// Acknowledged: back in the normal group, marked.
		icon = "✓"
	}
	icon = " " + m.styles.Severity(sev).Render(icon) + " "
	if len(e.pkg.Security) > 0 {
		// The shield marks an update that fixes a security issue.
		icon = " 🛡" + icon[1:]
	}

	name := e.pkg.Name
	if selected && m.focus == focusList && m.screen == screenUpdates {
		name = m.styles.Bold.Render(name)
	}
	rest := max(0, w-ansi.StringWidth(cursor)-ansi.StringWidth(icon))
	return cursor + versionColumn(name, oldPart, newPart, rest) + icon
}

// minNameWidth is how far a package name shrinks before the old version is
// dropped altogether.
const minNameWidth = 10

// versionColumn lays out name on the left and oldPart+newPart right-aligned,
// in exactly w cells. When they don't fit, the old version is truncated
// first, then the name; newPart is never cut. If even newPart doesn't fit
// beside a few cells of name, only the name is shown.
func versionColumn(name, oldPart, newPart string, w int) string {
	nw := ansi.StringWidth(newPart)
	if newPart == "" || w < nw+1+4 {
		return fitLine(name, w)
	}
	space := w - nw - 1 // for the name and the old version
	nameW, oldW := ansi.StringWidth(name), ansi.StringWidth(oldPart)
	if nameW+oldW <= space {
		return fitLine(name, space-oldW) + " " + oldPart + newPart
	}
	nameGet := min(nameW, max(min(nameW, minNameWidth), space-oldW))
	if oldGet := space - nameGet; oldGet >= 3 {
		return fitLine(name, nameGet) + " " + ansi.Truncate(oldPart, oldGet, "…") + newPart
	}
	return fitLine(name, space) + " " + newPart
}

// versionParts shortens a version change for the list: "580.95.05-1" →
// "590.44.01-1" becomes "580.95", "590.44". If the short forms are equal,
// the full upstream versions are used, then the full versions.
func versionParts(oldV, newV string) (string, string) {
	o, n := shortVersion(oldV, 2), shortVersion(newV, 2)
	if o == n {
		o, n = shortVersion(oldV, -1), shortVersion(newV, -1)
	}
	if o == n {
		o, n = oldV, newV
	}
	return o, n
}

// gitHash matches the commit hash that git snapshot versions end with:
// "2.42+r33+gc7ce5b4c0b7f" or "0.12.0.r1300.g1234567".
var gitHash = regexp.MustCompile(`[.+]g[0-9a-f]{7,}$`)

// shortVersion strips the epoch, the pkgrel and any trailing git commit
// hash, and keeps the first parts dot-separated components, or all of them
// if parts < 0.
func shortVersion(v string, parts int) string {
	if i := strings.IndexByte(v, ':'); i >= 0 {
		v = v[i+1:]
	}
	if i := strings.LastIndexByte(v, '-'); i >= 0 {
		v = v[:i]
	}
	v = gitHash.ReplaceAllString(v, "")
	if parts < 0 {
		return v
	}
	if f := strings.SplitN(v, ".", parts+1); len(f) > parts {
		return strings.Join(f[:parts], ".")
	}
	return v
}

// archwebResult is a cached archlinux.org lookup for one repo package.
type archwebResult struct {
	pkg  archweb.Package
	err  error
	done bool
}

// refreshDetail renders the selected package into the detail viewport. If
// the package's archlinux.org metadata hasn't been looked up yet, it returns
// the command that does.
func (m *Model) refreshDetail() tea.Cmd {
	e, ok := m.updates.selected()
	var content string
	var cmd tea.Cmd
	switch {
	case ok:
		if e.aur == nil {
			if _, seen := m.archweb[e.pkg.Name]; !seen && m.opts.Fetcher != nil {
				m.archweb[e.pkg.Name] = archwebResult{}
				cmd = lookupArchweb(m.ctx(), m.opts.Fetcher, e.pkg.Name, false)
			}
		}
		content = m.detailContent(e, m.detail.Width())
	case m.filterQuery() != "":
		content = " No packages match “" + m.filter.Value() + "”."
	case m.repoSec.loading || m.aurSec.loading:
		content = " " + m.styles.Subtle.Render("Checking for updates…")
	default:
		content = " Nothing to update."
	}

	m.detail.SetContent(content)
	if e.key != m.detailKey {
		m.detailKey = e.key
		m.detail.GotoTop()
	}
	return cmd
}

// links returns an entry's upstream and changelog URLs, either of which may
// be empty. pending reports that a repo package's links are still being
// looked up.
func (m Model) links(e entry) (upstream, changelog string, pending bool) {
	if e.aur != nil {
		if e.aur.Info != nil {
			return e.aur.Info.URL, e.aur.Info.ChangelogURL(), false
		}
		return "", "", false
	}
	r := m.archweb[e.pkg.Name]
	if !r.done {
		return "", "", true
	}
	if r.err != nil {
		return "", "", false
	}
	return r.pkg.URL, r.pkg.ChangelogURL(), false
}

// transactionLines describes what the upgrade does to an entry, from the
// preview: a blank line and a heading first, then one line per fact. It is
// empty for packages the preview knows nothing about.
func (m Model) transactionLines(e entry) []string {
	size := func(label string, n int64) string { return label + " " + pacman.FormatSize(n) }
	built := func(t time.Time) string {
		return "built " + risk.Age(t, m.now()) + " (" + t.In(m.location()).Format("2006-01-02 15:04") + ")"
	}
	download := func(n int64) string {
		if n == 0 {
			return "download: already in the cache"
		}
		return size("download:", n)
	}
	var lines []string
	switch e.kind {
	case entryUpdate:
		d, ok := m.txUpdates[e.pkg.Name]
		if !ok || e.aur != nil {
			return nil
		}
		lines = []string{"", " Transaction:", download(d.DownloadSize)}
		if d.OldSize > 0 || d.NewSize > 0 {
			lines = append(lines, "installed size: "+pacman.FormatSize(d.OldSize)+" → "+pacman.FormatSize(d.NewSize)+
				" ("+pacman.FormatDelta(d.NewSize-d.OldSize)+")")
		}
		if !d.BuildDate.IsZero() {
			lines = append(lines, built(d.BuildDate))
		}
	case entryNew:
		n := e.newPkg
		lines = []string{"", " Pulled in by the upgrade:"}
		if len(n.NeededBy) > 0 {
			lines = append(lines, "needed by "+strings.Join(n.NeededBy, ", "))
		}
		if len(n.Replaces) > 0 {
			lines = append(lines, "replaces "+strings.Join(n.Replaces, ", "))
		}
		lines = append(lines, download(n.DownloadSize), size("installed size:", n.Size))
		if !n.BuildDate.IsZero() {
			lines = append(lines, built(n.BuildDate))
		}
	case entryRemoved:
		r := e.removed
		lines = []string{"", " Removed by the upgrade:", "replaced by " + r.ReplacedBy, size("frees", r.Size)}
	case entryHeld:
		lines = []string{"", " Held back:", "not part of this upgrade: IgnorePkg or IgnoreGroup keeps it at " + e.pkg.OldVersion}
	}
	return lines
}

func (m Model) detailContent(e entry, w int) string {
	s := m.styles
	var b strings.Builder
	line := func(text string) {
		b.WriteString(wrapIndent(text, w, 4))
		b.WriteByte('\n')
	}
	bullet := func(text string) {
		line("  • " + text)
	}

	line(" " + s.Bold.Render(e.pkg.Name))
	where := ""
	upstream, changelog, _ := m.links(e)
	if e.aur != nil {
		where = "AUR"
		switch e.aur.Kind {
		case aur.KindVCS:
			where = "AUR · VCS"
		case aur.KindDebug:
			where = "debug split"
		}
	} else if r := m.archweb[e.pkg.Name]; r.done && r.err == nil {
		where = r.pkg.Repo
	}
	if where == "" && e.newPkg != nil {
		where = e.newPkg.Repo
	}
	version := "installed " + e.pkg.OldVersion
	switch {
	case e.kind == entryNew:
		version = "new: " + e.pkg.NewVersion
	case e.pending():
		version = e.pkg.OldVersion + " → " + e.pkg.NewVersion
	}
	if ansi.StringWidth(version)+ansi.StringWidth(where)+4 <= w {
		b.WriteString(" " + spread(version, s.Subtle.Render(where), w-2) + "\n")
	} else {
		line(" " + version)
		if where != "" {
			line(" " + s.Subtle.Render(where))
		}
	}

	if lines := securityLines(e.pkg); len(lines) > 0 {
		line("")
		line(" Security:")
		for _, l := range lines {
			bullet(s.Severity(risk.Info).Render(l))
		}
	}

	if len(e.pkg.Flags) > 0 {
		line("")
		line(" Why flagged:")
		for _, f := range e.pkg.Flags {
			bullet(s.Severity(f.Severity).Render(f.Reason))
		}
	}

	if len(e.pkg.Acknowledged) > 0 {
		line("")
		line(" Acknowledged for " + e.pkg.NewVersion + ":")
		for _, f := range e.pkg.Acknowledged {
			bullet(s.Subtle.Render(f.Reason))
		}
	}

	for _, l := range m.transactionLines(e) {
		if l == "" || l[0] == ' ' {
			// A blank line or a heading.
			line(l)
			continue
		}
		bullet(l)
	}

	if e.aur != nil {
		var notes []string
		switch e.aur.Kind {
		case aur.KindVCS:
			notes = append(notes, "VCS package: built from upstream's latest source, so the AUR version isn't compared. Your AUR helper's devel mode (--devel) checks for new commits.")
		case aur.KindDebug:
			notes = append(notes, "Debug symbols split off by makepkg from "+strings.TrimSuffix(e.aur.Name, "-debug")+". Never on the AUR; updated with it.")
		}
		switch {
		case e.aur.Missing():
			notes = append(notes, "Not in the AUR (orphaned and removed, or renamed?)")
		case e.aur.Info != nil:
			if e.aur.Info.Orphaned() {
				notes = append(notes, "Orphaned: the AUR package has no maintainer")
			}
			if e.aur.Info.FlaggedOutOfDate() {
				notes = append(notes, "Flagged out of date on "+e.aur.Info.OutOfDate.In(m.location()).Format("2006-01-02"))
			}
		}
		if len(notes) > 0 {
			line("")
			line(" AUR status:")
			for i, n := range notes {
				style := s.Warn
				if e.aur.Kind != aur.KindRegular && i == 0 {
					style = s.Subtle
				}
				bullet(style.Render(n))
			}
		}
		if rl := m.reviewLines(e); len(rl) > 0 {
			line("")
			line(" AUR review:")
			for _, l := range rl {
				bullet(l)
			}
		}
	}

	if e.kind == entryRemoved {
		return strings.TrimRight(b.String(), "\n")
	}
	line("")
	line(" Links:")
	switch r := m.archweb[e.pkg.Name]; {
	case e.aur == nil && !r.done:
		bullet(s.Subtle.Render("looking up archlinux.org…"))
	case e.aur == nil && errors.Is(r.err, archweb.ErrNotFound):
		bullet(s.Subtle.Render("not found on archlinux.org"))
	case e.aur == nil && r.err != nil:
		bullet(s.Error.Render("archlinux.org lookup failed: " + r.err.Error()))
	case upstream == "" && changelog == "":
		bullet(s.Subtle.Render("none"))
	default:
		// URLs are never wrapped: one cut in two can't be copied or
		// clicked. Long ones are truncated, and the link still opens the
		// whole URL in terminals that support OSC 8.
		for _, l := range [][2]string{{"upstream   ", upstream}, {"changelog  ", changelog}} {
			if l[1] != "" {
				prefix := "  • " + l[0]
				b.WriteString(prefix + hyperlink(l[1], w-ansi.StringWidth(prefix)-1) + "\n")
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
