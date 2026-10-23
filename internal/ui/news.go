package ui

import (
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"

	"github.com/leNITHISH/cronen/internal/risk"
)

// newsPane is the list of news items on the news screen.
type newsPane struct {
	sel    int
	offset int
}

func (p *newsPane) move(delta, n int) {
	if n == 0 {
		p.sel = 0
		return
	}
	delta = clamp(delta, -n, n)
	p.sel = clamp(p.sel+delta, 0, n-1)
}

// scroll adjusts the offset so the selected item is visible in h lines.
func (p *newsPane) scroll(h int) {
	if h <= 0 {
		return
	}
	if p.sel < p.offset {
		p.offset = p.sel
	}
	if p.sel >= p.offset+h {
		p.offset = p.sel - h + 1
	}
	p.offset = max(0, p.offset)
}

func (m Model) newsListView(w, h int) string {
	switch {
	case m.newsSec.loading && len(m.newsItems) == 0:
		return "   " + m.styles.Subtle.Render(m.spinner.View()+" fetching news…")
	case m.newsSec.err != nil && len(m.newsItems) == 0:
		return "   " + m.styles.Error.Render("✗ "+m.newsSec.err.Error())
	case len(m.newsItems) == 0:
		return "   " + m.styles.Subtle.Render("no news")
	}
	end := min(len(m.newsItems), m.newsPane.offset+h)
	lines := make([]string, 0, h)
	for i := m.newsPane.offset; i < end; i++ {
		it := m.newsItems[i]
		cursor := "  "
		if i == m.newsPane.sel {
			cursor = "▸ "
		}
		marker := " "
		if risk.RequiresManualIntervention(it) && m.unread[it.GUID] {
			marker = m.styles.Critical.Render(severityIcon(risk.Critical))
		}
		date := it.Published.In(m.location()).Format("2006-01-02")
		title := it.Title
		if m.unread[it.GUID] {
			title = m.styles.Bold.Render(title)
		} else {
			title = m.styles.Subtle.Render(title)
		}
		lines = append(lines, fitLine(cursor+marker+" "+m.styles.Subtle.Render(date)+"  "+title, w))
	}
	return strings.Join(lines, "\n")
}

// renderKey identifies one Glamour rendering of a news item.
type renderKey struct {
	guid  string
	width int
	dark  bool
}

// refreshNewsView renders the selected news item into the news viewport.
func (m *Model) refreshNewsView() {
	w := m.newsView.Width()
	if len(m.newsItems) == 0 || w <= 0 {
		m.newsView.SetContent("")
		m.newsViewKey = ""
		return
	}
	it := m.newsItems[clamp(m.newsPane.sel, 0, len(m.newsItems)-1)]
	k := renderKey{guid: it.GUID, width: w, dark: m.isDark}
	body, ok := m.rendered[k]
	if !ok {
		body = renderMarkdown(it.Markdown, w, m.isDark)
		m.rendered[k] = body
	}

	status := "read"
	if m.unread[it.GUID] {
		status = "unread"
	}
	header := []string{
		" " + m.styles.Bold.Render(wrapIndent(it.Title, w-1, 1)),
		" " + m.styles.Subtle.Render(it.Published.In(m.location()).Format("2006-01-02 15:04")+" · "+status),
		" " + m.styles.Subtle.Render(it.Link),
	}
	if risk.RequiresManualIntervention(it) {
		header = append(header, " "+m.styles.Critical.Render(severityIcon(risk.Critical)+" requires manual intervention"))
	}
	m.newsView.SetContent(strings.Join(header, "\n") + "\n" + body)
	if it.GUID != m.newsViewKey {
		m.newsViewKey = it.GUID
		m.newsView.GotoTop()
	}
}

// renderMarkdown renders a news post with Glamour, falling back to the raw
// Markdown if rendering fails.
func renderMarkdown(md string, width int, dark bool) string {
	style := styles.LightStyle
	if dark {
		style = styles.DarkStyle
	}
	// Glamour indents the document by a margin on each side; keep the
	// wrapped text inside the pane.
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(max(10, width-4)),
	)
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return strings.TrimRight(out, "\n")
}
