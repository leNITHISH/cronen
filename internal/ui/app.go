// Package ui holds the Bubble Tea models for the cronen TUI.
package ui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/leNITHISH/cronen/internal/aur"
	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/news"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/summary"
	"github.com/leNITHISH/cronen/internal/system"
)

// Options configures the root model.
type Options struct {
	// Demo reports that the app is running on bundled fixture data.
	Demo bool

	// System access. In demo mode these are fakes serving fixtures.
	Runner  system.Runner
	Fetcher system.Fetcher
	FS      system.FS

	// Config is the user configuration. Nil means config.Default().
	Config *config.Config
	// State is what cronen remembered from earlier runs.
	State config.State
	// StatePath is where changes to State are saved. Empty means they are
	// kept in memory only, as in demo mode.
	StatePath string

	// Exec hands the terminal to the upgrade command and reports how it
	// exited through done; the real one wraps tea.ExecProcess. Nil
	// disables upgrading.
	Exec func(argv []string, done tea.ExecCallback) tea.Cmd

	// Context bounds every command and request. Nil means
	// context.Background().
	Context context.Context
	// Now returns the current time. Nil means time.Now.
	Now func() time.Time
	// Location is the time zone of pacman.log timestamps and of dates
	// shown in the UI. Nil means time.Local.
	Location *time.Location
}

type screen int

const (
	screenUpdates screen = iota
	screenNews
	screenHelp
	screenConfirm
)

type focus int

const (
	focusList focus = iota
	focusDetail
)

// section is the load status of one data source.
type section struct {
	loading bool
	err     error
}

// defaultWidth and defaultHeight are used until the first WindowSizeMsg.
const (
	defaultWidth  = 80
	defaultHeight = 24
)

// Model is the root Bubble Tea model.
type Model struct {
	opts   Options
	cfg    config.Config
	keys   KeyMap
	styles Styles
	isDark bool

	help    help.Model
	spinner spinner.Model
	filter  textinput.Model
	// filtering reports whether the filter input has the keyboard.
	filtering bool
	// flashMsg is shown in the status bar until the next key press.
	flashMsg string
	flashErr bool
	upgrade  upgradeOutcome
	// spinning reports whether a spinner tick is in flight.
	spinning bool

	width, height int
	screen        screen
	// prevScreen is where the help screen returns to.
	prevScreen screen
	focus      focus

	// gen is the generation of the current load; see load.go.
	gen                              int
	repoSec, aurSec, newsSec, logSec section
	pacnewSec                        section
	repo                             []pacman.Update
	aurPkgs                          []aur.Package
	newsItems                        []news.Item
	lastUpgrade                      time.Time
	pacnew                           []string
	unreadNews                       []news.Item
	unread                           map[string]bool
	newsDecided                      bool
	result                           risk.Result
	archweb                          map[string]archwebResult
	rendered                         map[renderKey]string
	updates                          updatesPane
	newsPane                         newsPane
	detail, newsView                 viewport.Model
	detailKey, newsViewKey           string
}

// New returns the root model. Its Init starts loading every data source.
func New(opts Options) Model {
	cfg := config.Default()
	if opts.Config != nil {
		cfg = *opts.Config
	}
	m := Model{
		opts:     opts,
		cfg:      cfg,
		keys:     DefaultKeyMap(),
		isDark:   true,
		help:     help.New(),
		spinner:  spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		filter:   textinput.New(),
		spinning: true,
		gen:      1,
		archweb:  map[string]archwebResult{},
		rendered: map[renderKey]string{},
		detail:   viewport.New(),
		newsView: viewport.New(),
	}
	m.filter.Prompt = "/"
	m.filter.Placeholder = "filter packages"
	m.setStyles(true)
	m.markLoading()
	m.layout()
	return m
}

func (m *Model) setStyles(isDark bool) {
	m.isDark = isDark
	m.styles = DefaultStyles(isDark)
	m.help.Styles = m.styles.Help
	m.spinner.Style = m.styles.Subtle
	fs := textinput.DefaultStyles(isDark)
	// A blinking cursor would keep a tick running for nothing.
	fs.Cursor.Blink = false
	m.filter.SetStyles(fs)
}

func (m Model) ctx() context.Context {
	if m.opts.Context != nil {
		return m.opts.Context
	}
	return context.Background()
}

func (m Model) now() time.Time {
	if m.opts.Now != nil {
		return m.opts.Now()
	}
	return time.Now()
}

func (m Model) location() *time.Location {
	if m.opts.Location != nil {
		return m.opts.Location
	}
	return time.Local
}

func (m *Model) markLoading() {
	m.repoSec = section{loading: true}
	m.aurSec = section{loading: m.cfg.AUR}
	m.newsSec = section{loading: true}
	m.logSec = section{loading: true}
	m.pacnewSec = section{loading: true}
}

func (m Model) anyLoading() bool {
	for _, s := range []section{m.repoSec, m.aurSec, m.newsSec, m.logSec, m.pacnewSec} {
		if s.loading {
			return true
		}
	}
	return false
}

// loadCmds returns one command per data source, for the current generation.
func (m Model) loadCmds(fresh bool) []tea.Cmd {
	ctx := m.ctx()
	cmds := []tea.Cmd{
		loadRepo(ctx, m.opts.Runner, m.gen),
		loadNews(ctx, m.opts.Fetcher, m.gen, fresh),
		loadLog(m.opts.FS, m.location(), m.gen),
		loadPacnew(ctx, m.opts.Runner, m.gen),
	}
	if m.cfg.AUR {
		cmds = append(cmds, loadAUR(ctx, m.opts.Runner, m.opts.Fetcher, m.gen, fresh))
	}
	return cmds
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	cmds := append(m.loadCmds(false), m.spinner.Tick, tea.RequestBackgroundColor)
	return tea.Batch(cmds...)
}

// refresh reloads everything, bypassing the cache.
func (m *Model) refresh() tea.Cmd {
	m.gen++
	m.markLoading()
	m.archweb = map[string]archwebResult{}
	cmds := m.loadCmds(true)
	if !m.spinning {
		m.spinning = true
		cmds = append(cmds, m.spinner.Tick)
	}
	return tea.Batch(append(cmds, m.changed())...)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, m.layout()

	case tea.BackgroundColorMsg:
		m.setStyles(msg.IsDark())
		m.refreshNewsView()
		return m, m.refreshDetail()

	case spinner.TickMsg:
		if !m.anyLoading() {
			m.spinning = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case repoMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.repoSec = section{err: msg.err}
		m.repo = msg.updates
		return m, m.changed()

	case aurMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.aurSec = section{err: msg.err}
		m.aurPkgs = msg.pkgs
		return m, m.changed()

	case newsMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.newsSec = section{err: msg.err}
		if msg.err == nil {
			m.newsItems = msg.items
		}
		return m, m.changed()

	case logMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.logSec = section{err: msg.err}
		m.lastUpgrade = msg.last
		return m, m.changed()

	case pacnewMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.pacnewSec = section{err: msg.err}
		m.pacnew = msg.files
		return m, nil

	case archwebMsg:
		m.archweb[msg.name] = archwebResult{pkg: msg.pkg, err: msg.err, done: true}
		m.refreshDetail()
		return m, nil

	case openedMsg:
		m.handleOpened(msg)
		return m, nil

	case stateSavedMsg:
		if msg.err != nil {
			m.flash("couldn't save state: "+msg.err.Error(), true)
		}
		return m, nil

	case upgradedMsg:
		return m, m.handleUpgraded(msg)

	case tea.KeyPressMsg:
		m.flash("", false)
		if m.filtering {
			return m.handleFilterKey(msg)
		}
		return m.handleKey(msg)
	}
	if m.filtering {
		// Pastes and the like.
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		return m, tea.Batch(cmd, m.layout())
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	if m.screen == screenConfirm {
		switch {
		case key.Matches(msg, k.Confirm):
			return m, m.startUpgrade()
		case key.Matches(msg, k.Cancel):
			m.screen = screenUpdates
		case key.Matches(msg, k.Quit):
			return m, tea.Quit
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, k.Quit):
		return m, tea.Quit
	case key.Matches(msg, k.Help):
		if m.screen == screenHelp {
			m.screen = m.prevScreen
		} else {
			m.prevScreen, m.screen = m.screen, screenHelp
		}
		return m, nil
	case key.Matches(msg, k.Refresh):
		return m, m.refresh()
	}

	if m.screen == screenHelp {
		if key.Matches(msg, k.Back) {
			m.screen = m.prevScreen
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, k.News):
		if m.screen == screenNews {
			m.screen = screenUpdates
		} else {
			m.screen = screenNews
			m.refreshNewsView()
		}
		return m, nil
	case key.Matches(msg, k.Back):
		switch {
		case m.screen != screenUpdates:
			m.screen = screenUpdates
		case m.filterQuery() != "":
			m.clearFilter()
			return m, m.layout()
		case m.upgrade.done:
			m.upgrade = upgradeOutcome{}
			return m, m.layout()
		}
		return m, nil
	case key.Matches(msg, k.Filter):
		return m, m.startFilter()
	case key.Matches(msg, k.MarkRead):
		return m, m.markRead()
	case key.Matches(msg, k.Upstream):
		return m, m.openLink(false)
	case key.Matches(msg, k.Changelog):
		return m, m.openLink(true)
	case key.Matches(msg, k.Upgrade):
		m.requestUpgrade()
		return m, nil
	case key.Matches(msg, k.Focus):
		if m.focus == focusList {
			m.focus = focusDetail
		} else {
			m.focus = focusList
		}
		return m, nil
	case key.Matches(msg, k.HalfDown):
		m.activeViewport().HalfPageDown()
		return m, nil
	case key.Matches(msg, k.HalfUp):
		m.activeViewport().HalfPageUp()
		return m, nil
	}

	if m.focus == focusDetail {
		vp := m.activeViewport()
		switch {
		case key.Matches(msg, k.Down):
			vp.ScrollDown(1)
		case key.Matches(msg, k.Up):
			vp.ScrollUp(1)
		case key.Matches(msg, k.Top):
			vp.GotoTop()
		case key.Matches(msg, k.Bottom):
			vp.GotoBottom()
		}
		return m, nil
	}

	delta := 0
	switch {
	case key.Matches(msg, k.Down):
		delta = 1
	case key.Matches(msg, k.Up):
		delta = -1
	case key.Matches(msg, k.Top):
		delta = -1 << 30
	case key.Matches(msg, k.Bottom):
		delta = 1 << 30
	default:
		return m, nil
	}
	if m.screen == screenNews {
		m.newsPane.move(delta, len(m.newsItems))
		m.newsPane.scroll(m.bodyHeight())
		m.refreshNewsView()
		return m, nil
	}
	m.updates.move(delta)
	m.updates.scroll(m.bodyHeight())
	return m, m.refreshDetail()
}

func (m *Model) activeViewport() *viewport.Model {
	if m.screen == screenNews {
		return &m.newsView
	}
	return &m.detail
}

// lastFullUpgrade is the later of pacman.log's last full upgrade and the
// last upgrade cronen ran, or zero if both are unknown.
func (m Model) lastFullUpgrade() time.Time {
	return summary.LastUpgrade(m.lastUpgrade, m.opts.State)
}

// changed re-runs the risk rules after any data arrived and rebuilds
// everything derived from them.
func (m *Model) changed() tea.Cmd {
	// News rules wait for pacman.log: until it is read, every recent item
	// would look unread and raise flags that vanish a moment later. While
	// either reloads, the previous decision stands.
	if !m.newsSec.loading && !m.logSec.loading {
		m.unreadNews = summary.Unread(m.newsItems, m.lastFullUpgrade(), m.opts.State, m.cfg, m.now())
		m.unread = map[string]bool{}
		for _, it := range m.unreadNews {
			m.unread[it.GUID] = true
		}
		m.newsDecided = true
	}

	m.result = summary.Assess(summary.Updates(m.repo, m.aurPkgs), m.unreadNews, m.cfg, m.now())

	m.newsPane.move(0, len(m.newsItems))
	// The banner may have appeared or gone, changing the body height.
	return m.layout()
}

// chromeHeight is the number of lines outside the body: header, optional
// banner, two rules and the status bar.
func (m Model) chromeHeight() int {
	h := 4
	if len(m.result.Interventions) > 0 {
		h++
	}
	if m.noticeText() != "" {
		h++
	}
	return h
}

func (m Model) size() (int, int) {
	if m.width <= 0 || m.height <= 0 {
		return defaultWidth, defaultHeight
	}
	return m.width, m.height
}

func (m Model) bodyHeight() int {
	_, h := m.size()
	return max(0, h-m.chromeHeight())
}

// listWidth is the width of the left pane on both screens.
func (m Model) listWidth() int {
	w, _ := m.size()
	lw := clamp(w*2/5, min(28, w/2), 56)
	// Widen the list so a section error fits on one line, as long as the
	// detail pane keeps minDetailWidth. Narrower terminals wrap it.
	for _, note := range m.errorNotes() {
		lw = max(lw, min(1+ansi.StringWidth(note), w-1-minDetailWidth))
	}
	return max(0, lw)
}

// minDetailWidth is the narrowest the right pane gets to make room for an
// error in the list.
const minDetailWidth = 40

// newsErrorNote is the attention section's note when the news failed.
const newsErrorNote = "✗ news unavailable; news rules skipped"

// errorNotes returns the error notes the updates list shows.
func (m Model) errorNotes() []string {
	var notes []string
	if m.newsSec.err != nil && !m.newsSec.loading {
		notes = append(notes, newsErrorNote)
	}
	for _, s := range []section{m.repoSec, m.aurSec} {
		if s.err != nil && !s.loading {
			notes = append(notes, "✗ "+s.err.Error())
		}
	}
	return notes
}

func (m Model) paneWidth() int {
	w, _ := m.size()
	return max(0, w-m.listWidth()-1)
}

// layout rebuilds the updates list, resizes the viewports to the current
// body and re-renders their content.
func (m *Model) layout() tea.Cmd {
	m.rebuildUpdates()
	pw, bh := m.paneWidth(), m.bodyHeight()
	w, _ := m.size()
	m.help.SetWidth(w)
	for _, vp := range []*viewport.Model{&m.detail, &m.newsView} {
		vp.SetWidth(pw)
		vp.SetHeight(bh)
	}
	m.updates.scroll(bh)
	m.newsPane.scroll(bh)
	m.refreshNewsView()
	return m.refreshDetail()
}

// View implements tea.View.
func (m Model) View() tea.View {
	w, h := m.size()
	var lines []string
	lines = append(lines, m.headerLine(w))
	if n := len(m.result.Interventions); n > 0 {
		lines = append(lines, fitLine(m.styles.Banner.Render(bannerText(n)), w))
	}
	if notice := m.noticeText(); notice != "" {
		style := m.styles.Bold
		if m.upgrade.err != nil {
			style = m.styles.Error
		}
		lines = append(lines, fitLine(" "+style.Render(notice), w))
	}

	lw, pw, bh := m.listWidth(), m.paneWidth(), m.bodyHeight()
	switch m.screen {
	case screenHelp, screenConfirm:
		body := m.help.FullHelpView(m.keys.FullHelp())
		if m.screen == screenConfirm {
			body = m.confirmView(w)
		}
		lines = append(lines, m.styles.Rule.Render(strings.Repeat("─", w)))
		lines = append(lines, fitBlock(body, w, bh)...)
		lines = append(lines, m.styles.Rule.Render(strings.Repeat("─", w)))
	default:
		lines = append(lines, m.rule(lw, pw, "┬"))
		var left, right []string
		if m.screen == screenNews {
			left = fitBlock(m.newsListView(lw, bh), lw, bh)
			right = fitBlock(m.newsView.View(), pw, bh)
		} else {
			left = fitBlock(m.updatesListView(lw, bh), lw, bh)
			right = fitBlock(m.detail.View(), pw, bh)
		}
		sep := m.styles.Rule.Render("│")
		for i := range bh {
			lines = append(lines, left[i]+sep+right[i])
		}
		lines = append(lines, m.rule(lw, pw, "┴"))
	}
	lines = append(lines, m.statusLine(w))

	v := tea.NewView(strings.Join(fitBlock(strings.Join(lines, "\n"), w, h), "\n"))
	v.AltScreen = true
	return v
}

func (m Model) rule(lw, pw int, joint string) string {
	return m.styles.Rule.Render(strings.Repeat("─", lw) + joint + strings.Repeat("─", pw))
}

func (m Model) headerLine(w int) string {
	title := "cronen"
	if m.opts.Demo {
		title += " (demo)"
	}
	var last string
	switch t := m.lastFullUpgrade(); {
	case m.logSec.loading && t.IsZero():
		last = "last upgrade: " + m.spinner.View()
	case t.IsZero():
		last = "last upgrade: unknown"
	default:
		last = "last upgrade: " + ago(t, m.now())
	}
	return spread(" "+m.styles.Title.Render(title), m.styles.Subtle.Render(last)+" ", w)
}

func bannerText(n int) string {
	if n == 1 {
		return " ⚠ 1 news item requires manual intervention — press n to read"
	}
	return " ⚠ " + itoa(n) + " news items require manual intervention — press n to read"
}

func (m Model) statusLine(w int) string {
	if m.filtering {
		hints := helpHint(m.keys.Accept) + "  esc clear"
		return spread(" "+m.filter.View(), m.styles.Subtle.Render(hints)+" ", w)
	}
	if m.screen == screenConfirm {
		hints := helpHint(m.keys.Confirm) + "  " + helpHint(m.keys.Cancel)
		return spread(" Upgrade?", m.styles.Subtle.Render(hints)+" ", w)
	}
	if m.flashMsg != "" {
		style := m.styles.Subtle
		if m.flashErr {
			style = m.styles.Error
		}
		return spread(" "+style.Render(m.flashMsg), m.styles.Subtle.Render(helpHint(m.keys.Help))+" ", w)
	}

	var parts []string
	switch {
	case m.repoSec.loading || m.aurSec.loading:
		parts = append(parts, m.spinner.View()+" checking updates")
	default:
		parts = append(parts, plural(len(m.result.Packages), "update", "updates"))
	}
	if m.newsDecided {
		parts = append(parts, itoa(len(m.result.Flagged()))+" flagged")
		parts = append(parts, itoa(len(m.unreadNews))+" unread news")
	}
	if m.newsSec.err != nil {
		parts = append(parts, m.styles.Error.Render("news unavailable"))
	}
	if n := len(m.pacnew); n > 0 {
		parts = append(parts, plural(n, ".pacnew", ".pacnew"))
	}
	if q := m.filter.Value(); strings.TrimSpace(q) != "" {
		parts = append(parts, "filter: "+q)
	}
	left := " " + strings.Join(parts, " · ")

	var hints []string
	if m.screen != screenUpdates {
		hints = append(hints, helpHint(m.keys.Back))
	}
	hints = append(hints, helpHint(m.keys.Help), helpHint(m.keys.Quit))
	return spread(left, m.styles.Subtle.Render(strings.Join(hints, "  "))+" ", w)
}

func helpHint(b key.Binding) string {
	h := b.Help()
	return h.Key + " " + h.Desc
}
