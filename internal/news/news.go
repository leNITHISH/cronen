package news

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"

	"github.com/leNITHISH/cronen/internal/system"
)

// FeedURL is the Arch Linux news RSS feed.
const FeedURL = "https://archlinux.org/feeds/news/"

// Item is one news post.
type Item struct {
	// GUID identifies the item in the state file's read set. It falls back
	// to Link when the feed has no guid.
	GUID      string
	Title     string
	Link      string
	Published time.Time
	// HTML is the post body as published in the feed.
	HTML string
	// Markdown is HTML converted to Markdown, for rendering with Glamour.
	Markdown string
	// Text is the post body as plain text with tags removed and entities
	// decoded. It is what package mentions are matched against, so link
	// targets and markup never count as mentions.
	Text string
}

// Fetch downloads and parses the news feed. Caching is the Fetcher's job:
// the real system.HTTPClient serves a cached copy within its TTL unless
// fresh is true.
func Fetch(ctx context.Context, f system.Fetcher, fresh bool) ([]Item, error) {
	body, err := f.Get(ctx, FeedURL, fresh)
	if err != nil {
		return nil, fmt.Errorf("news: %w", err)
	}
	return Parse(body)
}

// Parse parses an RSS (or Atom) feed. Items keep the feed's order, which for
// the Arch feed is newest first.
func Parse(data []byte) ([]Item, error) {
	feed, err := gofeed.NewParser().Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("news: parse feed: %w", err)
	}
	items := make([]Item, 0, len(feed.Items))
	for _, fi := range feed.Items {
		body := fi.Description
		if body == "" {
			body = fi.Content
		}
		it := Item{
			GUID:  fi.GUID,
			Title: strings.TrimSpace(fi.Title),
			Link:  fi.Link,
			HTML:  body,
			Text:  plainText(body),
		}
		if it.GUID == "" {
			it.GUID = it.Link
		}
		if fi.PublishedParsed != nil {
			it.Published = *fi.PublishedParsed
		} else if fi.UpdatedParsed != nil {
			it.Published = *fi.UpdatedParsed
		}
		md, err := htmltomarkdown.ConvertString(body)
		if err != nil {
			// Fall back to the plain text so the post is still readable.
			md = it.Text
		}
		it.Markdown = md
		items = append(items, it)
	}
	return items, nil
}

// plainText strips tags from an HTML fragment and decodes entities. Block
// elements are separated by whitespace so words on either side don't merge.
func plainText(fragment string) string {
	var b strings.Builder
	z := html.NewTokenizer(strings.NewReader(fragment))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return strings.Join(strings.Fields(b.String()), " ")
		case html.TextToken:
			b.Write(z.Text())
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			if blockTags[string(name)] {
				b.WriteByte(' ')
			}
		}
	}
}

// blockTags are the elements that separate words. Inline elements such as
// <code> and <a> don't, so "<code>foo</code>." stays "foo.".
var blockTags = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "br": true,
	"dd": true, "div": true, "dl": true, "dt": true, "figcaption": true, "figure": true,
	"footer": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"header": true, "hr": true, "li": true, "ol": true, "p": true, "pre": true,
	"section": true, "table": true, "td": true, "th": true, "tr": true, "ul": true,
}

// ReadState is what decides whether an item is unread.
type ReadState struct {
	// LastUpgrade is the time of the last full system upgrade. The zero
	// value means it is unknown (pacman.log unreadable).
	LastUpgrade time.Time
	// Read holds the GUIDs marked read.
	Read map[string]bool
	// Now is the current time.
	Now time.Time
	// UnknownWindow is how far back items count as unread when LastUpgrade
	// is unknown. Zero means DefaultUnknownWindow.
	UnknownWindow time.Duration
}

// DefaultUnknownWindow is how far back news counts as unread when the time of
// the last upgrade is unknown.
const DefaultUnknownWindow = 30 * 24 * time.Hour

// IsUnread reports whether it is unread: not marked read, and published after
// the last full upgrade (or within the unknown window if that is unknown).
func (s ReadState) IsUnread(it Item) bool {
	if s.Read[it.GUID] {
		return false
	}
	if !s.LastUpgrade.IsZero() {
		return it.Published.After(s.LastUpgrade)
	}
	window := s.UnknownWindow
	if window <= 0 {
		window = DefaultUnknownWindow
	}
	return it.Published.After(s.Now.Add(-window))
}

// Unread returns the unread items, keeping their order.
func (s ReadState) Unread(items []Item) []Item {
	var out []Item
	for _, it := range items {
		if s.IsUnread(it) {
			out = append(out, it)
		}
	}
	return out
}

// Recent returns the items published within the last days days of now,
// keeping their order. days <= 0 returns all items.
func Recent(items []Item, now time.Time, days int) []Item {
	if days <= 0 {
		return items
	}
	cutoff := now.AddDate(0, 0, -days)
	var out []Item
	for _, it := range items {
		if it.Published.After(cutoff) {
			out = append(out, it)
		}
	}
	return out
}
