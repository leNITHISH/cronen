package ui

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/ops"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/system"
)

// pkgFilter is what the Packages screen lists.
type pkgFilter int

const (
	pkgAll pkgFilter = iota
	pkgExplicit
	pkgDeps
	pkgForeign
	pkgOrphans
	pkgFilterCount
)

func (f pkgFilter) String() string {
	return [...]string{"all", "explicit", "dependencies", "foreign", "orphans"}[f]
}

// pkgSort is the order of the Packages screen.
type pkgSort int

const (
	sortName pkgSort = iota
	sortSize
	sortDate
	pkgSortCount
)

func (s pkgSort) String() string {
	return [...]string{"name", "size", "install date"}[s]
}

// packagesPane is the installed package list. The selection is tracked by
// name, so it stays on the same package when the filter or the order
// changes.
type packagesPane struct {
	filter pkgFilter
	sort   pkgSort
	// rows indexes localDB.Packages.
	rows    []int
	sel     int
	selName string
	offset  int
	// marked are the packages marked with space; X removes them.
	marked map[string]bool
}

type localDBMsg struct {
	gen int
	db  *pacman.LocalDB
	err error
}

func loadLocalDB(ctx context.Context, r system.Runner, loc *time.Location, gen int) tea.Cmd {
	return func() tea.Msg {
		db, err := pacman.LoadLocalDB(ctx, r, loc)
		return localDBMsg{gen: gen, db: db, err: err}
	}
}

func (m *Model) handleLocalDB(msg localDBMsg) tea.Cmd {
	if msg.gen != m.gen {
		return nil
	}
	m.localSec = section{err: msg.err}
	m.localDB = msg.db
	if msg.err != nil {
		m.localDB = nil
	}
	// Marks of packages that are gone don't linger.
	for name := range m.pkgs.marked {
		if m.localDB == nil {
			delete(m.pkgs.marked, name)
		} else if _, ok := m.localDB.Lookup(name); !ok {
			delete(m.pkgs.marked, name)
		}
	}
	m.rebuildPackages()
	return m.refreshDetail()
}

func (p *packagesPane) selected(db *pacman.LocalDB) (pacman.LocalPackage, bool) {
	if db == nil || p.sel < 0 || p.sel >= len(p.rows) {
		return pacman.LocalPackage{}, false
	}
	return db.Packages[p.rows[p.sel]], true
}

func (p *packagesPane) move(delta int) {
	if len(p.rows) == 0 {
		return
	}
	delta = clamp(delta, -len(p.rows), len(p.rows))
	p.sel = clamp(p.sel+delta, 0, len(p.rows)-1)
}

func (p *packagesPane) scroll(h int) {
	h-- // the column header
	if h <= 0 {
		return
	}
	if p.sel < p.offset {
		p.offset = p.sel
	}
	if p.sel >= p.offset+h {
		p.offset = p.sel - h + 1
	}
	p.offset = clamp(p.offset, 0, max(0, len(p.rows)-h))
}

// rebuildPackages applies the filter, the name filter and the sort to the
// installed packages and restores the selection.
func (m *Model) rebuildPackages() {
	p := &m.pkgs
	p.rows = p.rows[:0]
	if m.localDB != nil {
		q := m.filterQuery()
		if m.screen != screenPackages {
			q = ""
		}
		for i, pkg := range m.localDB.Packages {
			if q != "" && !strings.Contains(strings.ToLower(pkg.Name), q) {
				continue
			}
			if !m.pkgMatches(pkg, p.filter) {
				continue
			}
			p.rows = append(p.rows, i)
		}
		pk := m.localDB.Packages
		switch p.sort {
		case sortSize:
			slices.SortStableFunc(p.rows, func(a, b int) int {
				return cmp.Compare(pk[b].InstalledSize, pk[a].InstalledSize)
			})
		case sortDate:
			slices.SortStableFunc(p.rows, func(a, b int) int {
				return pk[b].InstallDate.Compare(pk[a].InstallDate)
			})
		}
	}
	p.sel = 0
	if p.selName != "" {
		for i, r := range p.rows {
			if m.localDB.Packages[r].Name == p.selName {
				p.sel = i
				break
			}
		}
	}
	if len(p.rows) > 0 {
		p.selName = m.localDB.Packages[p.rows[p.sel]].Name
	} else {
		p.selName = ""
	}
	p.scroll(m.bodyHeight())
}

func (m Model) pkgMatches(pkg pacman.LocalPackage, f pkgFilter) bool {
	switch f {
	case pkgExplicit:
		return pkg.Explicit
	case pkgDeps:
		return !pkg.Explicit
	case pkgForeign:
		return m.localDB.Foreign[pkg.Name]
	case pkgOrphans:
		return m.localDB.Orphans[pkg.Name]
	}
	return true
}

// handlePackagesKey handles the keys of the Packages screen that the
// shared handler doesn't. It reports whether the key was used.
func (m *Model) handlePackagesKey(k tea.KeyPressMsg) (tea.Cmd, bool) {
	keys, p := m.keys, &m.pkgs
	delta := 0
	switch {
	case key.Matches(k, keys.CycleFilter):
		p.filter = (p.filter + 1) % pkgFilterCount
		m.rebuildPackages()
		return m.refreshDetail(), true
	case key.Matches(k, keys.CycleSort):
		p.sort = (p.sort + 1) % pkgSortCount
		m.rebuildPackages()
		return m.refreshDetail(), true
	case key.Matches(k, keys.Mark):
		if pkg, ok := p.selected(m.localDB); ok {
			if p.marked == nil {
				p.marked = map[string]bool{}
			}
			if p.marked[pkg.Name] {
				delete(p.marked, pkg.Name)
			} else {
				p.marked[pkg.Name] = true
			}
		}
		return m.refreshDetail(), true
	case key.Matches(k, keys.Remove):
		names := slices.Sorted(maps.Keys(p.marked))
		if len(names) == 0 {
			m.flash("mark packages with space first, then X", false)
			return nil, true
		}
		return m.startRemove(ops.KindRemove, names), true
	case key.Matches(k, keys.Down):
		delta = 1
	case key.Matches(k, keys.Up):
		delta = -1
	case key.Matches(k, keys.Top):
		delta = -1 << 30
	case key.Matches(k, keys.Bottom):
		delta = 1 << 30
	default:
		return nil, false
	}
	p.move(delta)
	if pkg, ok := p.selected(m.localDB); ok {
		p.selName = pkg.Name
	}
	p.scroll(m.bodyHeight())
	return m.refreshDetail(), true
}

// pkgBadges are the flags of a package: E explicit or D dependency, F
// foreign, O orphan. Each has a fixed column.
func (m Model) pkgBadges(pkg pacman.LocalPackage) string {
	b := []byte("D  ")
	if pkg.Explicit {
		b[0] = 'E'
	}
	if m.localDB.Foreign[pkg.Name] {
		b[1] = 'F'
	}
	if m.localDB.Orphans[pkg.Name] {
		b[2] = 'O'
	}
	return string(b)
}

// minPkgName is how wide the name column must stay before the others give
// way, one by one.
const minPkgName = 14

// pkgColumns decides which optional columns fit in w cells: version, size,
// install date, dropped from the right when the name would get too narrow.
func pkgColumns(w int) (version, size, date bool) {
	const fixed = 2 + 2 + 1 + 3 // cursor, mark, gap, badges
	version, size, date = true, true, true
	room := func() int {
		n := w - fixed
		if version {
			n -= 1 + versionCol
		}
		if size {
			n -= 1 + sizeCol
		}
		if date {
			n -= 1 + dateCol
		}
		return n
	}
	for _, drop := range []*bool{&date, &size, &version} {
		if room() >= minPkgName {
			break
		}
		*drop = false
	}
	return
}

// Widths of the optional columns.
const (
	versionCol = 12
	sizeCol    = 7
	dateCol    = 10
)

func (m Model) packagesListView(w, h int) string {
	switch {
	case !m.cfg.PackageBrowser:
		return "   " + m.styles.Subtle.Render("package browser is off (package_browser = false)")
	case m.localSec.loading && m.localDB == nil:
		return "   " + m.styles.Subtle.Render(m.spinner.View()+" reading installed packages…")
	case m.localSec.err != nil:
		return " " + m.styles.Error.Render("✗ "+m.localSec.err.Error())
	case m.localDB == nil:
		return ""
	}
	p := m.pkgs
	vc, sc, dc := pkgColumns(w)
	nameW := w - (2 + 2 + 1 + 3)
	header := "    "
	cols := func(version, size, date string) string {
		var s string
		if vc {
			s += " " + fitLine(version, versionCol)
		}
		if sc {
			s += " " + fitLine(size, sizeCol)
		}
		if dc {
			s += " " + fitLine(date, dateCol)
		}
		return s
	}
	if vc {
		nameW -= 1 + versionCol
	}
	if sc {
		nameW -= 1 + sizeCol
	}
	if dc {
		nameW -= 1 + dateCol
	}
	header = fitLine(header+fitLine("name", nameW)+cols("version", "size", "installed")+" EFO", w)
	lines := []string{m.styles.Section.Render(header)}
	if len(p.rows) == 0 {
		lines = append(lines, "   "+m.styles.Subtle.Render("no packages match"))
		return strings.Join(lines, "\n")
	}
	end := min(len(p.rows), p.offset+h-1)
	for i := p.offset; i < end; i++ {
		pkg := m.localDB.Packages[p.rows[i]]
		cursor, mark := "  ", "  "
		if i == p.sel {
			cursor = "▸ "
		}
		if p.marked[pkg.Name] {
			mark = "✓ "
		}
		name := fitLine(pkg.Name, nameW)
		if i == p.sel && m.focus == focusList {
			name = m.styles.Bold.Render(name)
		}
		date := ""
		if !pkg.InstallDate.IsZero() {
			date = pkg.InstallDate.Format("2006-01-02")
		}
		line := cursor + mark + name + cols(pkg.Version, pacman.FormatSize(pkg.InstalledSize), date) + " " + m.pkgBadges(pkg)
		lines = append(lines, fitLine(line, w))
	}
	return strings.Join(lines, "\n")
}

// packageDetail renders the detail pane of one installed package.
func (m Model) packageDetail(pkg pacman.LocalPackage, w int) string {
	var b strings.Builder
	b.WriteString(" " + m.styles.Title.Render(pkg.Name) + " " + pkg.Version + "\n")
	if pkg.Description != "" {
		b.WriteString(" " + wrapIndent(pkg.Description, w-2, 1) + "\n")
	}
	b.WriteString("\n")

	var facts []string
	if pkg.Explicit {
		facts = append(facts, "explicitly installed")
	} else {
		facts = append(facts, "installed as a dependency")
	}
	if m.localDB.Foreign[pkg.Name] {
		facts = append(facts, "foreign (not in a sync database)")
	}
	if m.localDB.Orphans[pkg.Name] {
		facts = append(facts, "orphan")
	}
	b.WriteString(" " + wrapIndent(strings.Join(facts, " · "), w-2, 1) + "\n")
	b.WriteString(" " + pacman.FormatSize(pkg.InstalledSize))
	if !pkg.InstallDate.IsZero() {
		b.WriteString(" · installed " + pkg.InstallDate.In(m.location()).Format("2006-01-02"))
	}
	b.WriteString("\n")
	if m.pkgs.marked[pkg.Name] {
		b.WriteString(" " + m.styles.Bold.Render("✓ marked") + "\n")
	}
	b.WriteString("\n " + m.styles.Section.Render("Why it's installed") + "\n")
	chain, ok := m.localDB.WhyInstalled(pkg.Name)
	switch {
	case !ok:
		b.WriteString("   " + m.styles.Warn.Render("no explicitly installed package needs it") + "\n")
	case len(chain) == 1:
		b.WriteString("   you installed it\n")
	default:
		b.WriteString("   " + wrapIndent(strings.Join(chain, " ← ")+" (explicit)", w-3, 5) + "\n")
	}

	for _, sec := range []struct {
		title string
		names []string
	}{
		{"Depends on", pkg.DependsOn},
		{"Required by", pkg.RequiredBy},
		{"Optional for", pkg.OptionalFor},
		{"Provides", pkg.Provides},
		{"Groups", pkg.Groups},
		{"Backup files", pkg.BackupFiles},
	} {
		if len(sec.names) == 0 {
			continue
		}
		b.WriteString("\n " + m.styles.Section.Render(sec.title) + "\n")
		b.WriteString("   " + wrapIndent(strings.Join(sec.names, "  "), w-3, 3) + "\n")
	}
	if pkg.URL != "" {
		b.WriteString("\n " + hyperlink(pkg.URL, max(0, w-2)) + "\n")
	}
	return b.String()
}

// refreshPackageDetail renders the selected package into the detail pane.
func (m *Model) refreshPackageDetail() tea.Cmd {
	var content, key string
	switch pkg, ok := m.pkgs.selected(m.localDB); {
	case !m.cfg.PackageBrowser:
		content = " Turn on package_browser in the config to list installed packages."
	case ok:
		content, key = m.packageDetail(pkg, m.detail.Width()), pkg.Name
	case m.localSec.err != nil:
		content = " " + m.styles.Subtle.Render("The installed packages couldn't be read.")
	case m.localSec.loading:
		content = " " + m.styles.Subtle.Render("Reading installed packages…")
	case m.filterQuery() != "":
		content = " No packages match “" + m.filter.Value() + "”."
	default:
		content = " No packages in this view."
	}
	m.detail.SetContent(content)
	if key != m.detailKey {
		m.detailKey = key
		m.detail.GotoTop()
	}
	return nil
}

// packagesStatus are the status bar parts of the Packages screen.
func (m Model) packagesStatus() []string {
	if m.localDB == nil {
		return []string{"packages"}
	}
	parts := []string{plural(len(m.localDB.Packages), "package", "packages")}
	if m.pkgs.filter != pkgAll || m.filterQuery() != "" {
		parts = append(parts, itoa(len(m.pkgs.rows))+" shown")
	}
	parts = append(parts, "show: "+m.pkgs.filter.String(), "sort: "+m.pkgs.sort.String())
	if n := len(m.pkgs.marked); n > 0 {
		parts = append(parts, itoa(n)+" marked")
	}
	return parts
}
