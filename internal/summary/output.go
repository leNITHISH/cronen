package summary

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/leNITHISH/cronen/internal/risk"
)

// Headline is the one-line summary: "18 updates (2 AUR) · 5 flagged · 2
// unread news · 1 .pacnew".
func (s Summary) Headline() string {
	updates := plural(s.Updates(), "update", "updates")
	if s.AUR > 0 {
		updates += " (" + strconv.Itoa(s.AUR) + " AUR)"
	}
	parts := []string{
		updates,
		strconv.Itoa(len(s.Flagged)) + " flagged",
		strconv.Itoa(s.UnreadNews) + " unread news",
	}
	if s.Pacnew > 0 {
		parts = append(parts, plural(s.Pacnew, ".pacnew", ".pacnew"))
	}
	return strings.Join(parts, " · ")
}

// Text is the plain-text report --check prints: the headline, then what
// needs attention, most severe first, then any errors.
func (s Summary) Text() string {
	lines := []string{s.Headline()}
	for _, it := range s.Interventions {
		lines = append(lines, icon(risk.Critical)+" manual intervention: "+oneLine(it.Title))
	}
	for _, sev := range []risk.Severity{risk.Critical, risk.Warn, risk.Info} {
		for _, p := range s.Flagged {
			if p.Severity() == sev {
				lines = append(lines, icon(sev)+" "+p.Name+": "+oneLine(p.Flags[0].Reason))
			}
		}
	}
	if s.Reboot {
		lines = append(lines, "reboot after upgrading: the kernel is updated")
	}
	for _, err := range s.Errors {
		lines = append(lines, "error: "+oneLine(err.Error()))
	}
	return strings.Join(lines, "\n") + "\n"
}

// JSON is the machine-readable summary --json prints, for example
// {"updates":18,"aur":2,"flagged":5,"critical":1,"unread_news":2,...}.
// critical counts the unread news items requiring manual intervention.
func (s Summary) JSON() []byte {
	v := struct {
		Updates    int      `json:"updates"`
		AUR        int      `json:"aur"`
		Flagged    int      `json:"flagged"`
		Critical   int      `json:"critical"`
		UnreadNews int      `json:"unread_news"`
		Pacnew     int      `json:"pacnew"`
		Reboot     bool     `json:"reboot"`
		Errors     []string `json:"errors,omitempty"`
	}{
		Updates:    s.Updates(),
		AUR:        s.AUR,
		Flagged:    len(s.Flagged),
		Critical:   len(s.Interventions),
		UnreadNews: s.UnreadNews,
		Pacnew:     s.Pacnew,
		Reboot:     s.Reboot,
	}
	for _, err := range s.Errors {
		v.Errors = append(v.Errors, err.Error())
	}
	return mustJSON(v)
}

// Class is the state name --waybar reports in "class" and "alt", matching
// ExitCode: "error", "critical", "attention", "updates" or "none".
func (s Summary) Class() string {
	switch s.ExitCode() {
	case ExitError:
		return "error"
	case ExitIntervention:
		return "critical"
	case ExitAttention:
		return "attention"
	case ExitUpdates:
		return "updates"
	}
	return "none"
}

// Waybar is the output for a waybar custom module with return-type json.
// The text is the number of updates, empty when there are none so waybar
// hides the module, or "!" when a source failed. Waybar renders text and
// tooltip as Pango markup, so both are escaped.
func (s Summary) Waybar() []byte {
	text := ""
	switch {
	case len(s.Errors) > 0:
		text = "!"
	case s.Updates() > 0:
		text = strconv.Itoa(s.Updates())
	}
	class := s.Class()
	return mustJSON(struct {
		Text    string `json:"text"`
		Alt     string `json:"alt"`
		Tooltip string `json:"tooltip"`
		Class   string `json:"class"`
	}{
		Text:    pango(text),
		Alt:     class,
		Tooltip: pango(strings.TrimSuffix(s.Text(), "\n")),
		Class:   class,
	})
}

var pangoEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func pango(s string) string {
	return pangoEscaper.Replace(s)
}

// oneLine keeps network-supplied text from breaking the line-based output:
// control characters, newlines included, become spaces.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return ' '
		}
		return r
	}, s)
}

func icon(sev risk.Severity) string {
	switch sev {
	case risk.Info:
		return "ℹ"
	case risk.Warn:
		return "⚠"
	case risk.Critical:
		return "‼"
	}
	return " "
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func mustJSON(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	// Keep "&", "<" and ">" readable; waybar output is Pango-escaped
	// separately.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		// Only plain strings, ints and bools are marshalled.
		panic(err)
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}
