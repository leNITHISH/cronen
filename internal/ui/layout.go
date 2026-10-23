package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// fitLine truncates s to exactly w cells, or pads it with spaces.
func fitLine(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// fitBlock returns exactly h lines of exactly w cells: s's lines, cut or
// padded as needed.
func fitBlock(s string, w, h int) []string {
	if h <= 0 {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	out := make([]string, h)
	for i := range out {
		var l string
		if i < len(lines) {
			l = lines[i]
		}
		out[i] = fitLine(l, w)
	}
	return out
}

// spread puts left and right on one line of width w, with right aligned to
// the right edge. If both don't fit, right is dropped first.
func spread(left, right string, w int) string {
	lw, rw := ansi.StringWidth(left), ansi.StringWidth(right)
	if lw+1+rw > w {
		return fitLine(left, w)
	}
	return left + strings.Repeat(" ", w-lw-rw) + right
}

// wrapIndent wraps text to width w, indenting continuation lines by indent
// spaces.
func wrapIndent(text string, w, indent int) string {
	if w-indent < 8 {
		return text
	}
	lines := strings.Split(ansi.Wrap(text, w-indent, ""), "\n")
	if len(lines) > 1 {
		// The first line may use the indent's width too, but keeping every
		// line to the same width is simpler and still reads well.
		pad := strings.Repeat(" ", indent)
		for i := 1; i < len(lines); i++ {
			lines[i] = pad + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// clamp returns v limited to [lo, hi]. If hi < lo, lo wins.
func clamp(v, lo, hi int) int {
	return max(lo, min(v, hi))
}

// ago describes how long before now t was, coarsely.
func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Hour:
		return "just now"
	case d < 24*time.Hour:
		h := int(d / time.Hour)
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	case d < 48*time.Hour:
		return "yesterday"
	}
	return fmt.Sprintf("%d days ago", int(d/(24*time.Hour)))
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

// plural formats a count with the singular or plural noun.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}
