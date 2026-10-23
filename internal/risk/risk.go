package risk

import (
	"cmp"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/leNITHISH/cronen/internal/news"
)

// Severity ranks how much a flag needs the user's attention. The zero value
// means not flagged.
type Severity int

// Severity levels, lowest first.
const (
	None Severity = iota
	Info
	Warn
	Critical
)

func (s Severity) String() string {
	switch s {
	case None:
		return "none"
	case Info:
		return "info"
	case Warn:
		return "warn"
	case Critical:
		return "critical"
	}
	return fmt.Sprintf("Severity(%d)", int(s))
}

// Rule identifies which check raised a flag.
type Rule int

// Rules, in the order their flags are listed when severities tie.
const (
	RuleNews Rule = iota + 1
	RulePythonBump
	RuleMajorBump
	RuleWatchlist
	RuleKernel
)

func (r Rule) String() string {
	switch r {
	case RuleNews:
		return "news"
	case RulePythonBump:
		return "python-bump"
	case RuleMajorBump:
		return "major-bump"
	case RuleWatchlist:
		return "watchlist"
	case RuleKernel:
		return "kernel"
	}
	return fmt.Sprintf("Rule(%d)", int(r))
}

// DefaultWatchlist is the watchlist used when the config doesn't replace it.
var DefaultWatchlist = []string{
	"linux*", "nvidia*", "mesa", "vulkan-*", "glibc", "systemd", "grub",
	"pacman", "openssl", "python", "gcc*",
}

// Update is one pending update.
type Update struct {
	Name string
	// Base is the pkgbase. Empty means the same as Name.
	Base       string
	OldVersion string
	NewVersion string
	AUR        bool
}

func (u Update) base() string {
	if u.Base == "" {
		return u.Name
	}
	return u.Base
}

// Flag is one reason an update needs attention.
type Flag struct {
	Rule     Rule
	Severity Severity
	// Reason is a short human-readable explanation for the detail pane.
	Reason string
	// NewsGUID is the news item behind a RuleNews flag.
	NewsGUID string
}

// Package is an update with the flags raised for it.
type Package struct {
	Update
	// Flags are ordered by severity, highest first.
	Flags []Flag
}

// Severity is the highest severity among p's flags, or None.
func (p Package) Severity() Severity {
	if len(p.Flags) == 0 {
		return None
	}
	return p.Flags[0].Severity
}

// Input is everything Assess looks at.
type Input struct {
	Updates []Update
	// UnreadNews must hold only unread items (see news.ReadState.Unread);
	// Assess treats every item it is given as unread.
	UnreadNews []news.Item
	// Watchlist holds glob patterns (path.Match syntax) matched against
	// package names and pkgbases. Malformed patterns never match.
	Watchlist []string
	// PythonDependents lists installed foreign packages that depend on
	// python, for the Python bump warning. It may be empty.
	PythonDependents []string
	// Now is the current time, used for "2 days ago" in reasons.
	Now time.Time
}

// Result is the outcome of Assess.
type Result struct {
	// Packages has one entry per input update, in input order.
	Packages []Package
	// Interventions are the unread news items that require manual
	// intervention. They warrant a global banner whether or not any pending
	// update mentions them.
	Interventions []news.Item
	// RebootRequired is set when a kernel is among the updates.
	RebootRequired bool
}

// Flagged returns the packages with at least one flag, in input order.
func (r Result) Flagged() []Package {
	var out []Package
	for _, p := range r.Packages {
		if len(p.Flags) > 0 {
			out = append(out, p)
		}
	}
	return out
}

// Count returns how many packages have s as their highest severity.
func (r Result) Count(s Severity) int {
	n := 0
	for _, p := range r.Packages {
		if p.Severity() == s {
			n++
		}
	}
	return n
}

// Assess applies every rule to every update.
func Assess(in Input) Result {
	var res Result
	for _, it := range in.UnreadNews {
		if RequiresManualIntervention(it) {
			res.Interventions = append(res.Interventions, it)
		}
	}

	for _, u := range in.Updates {
		var flags []Flag
		flags = append(flags, newsFlags(u, in.UnreadNews, in.Now)...)
		if f, ok := pythonFlag(u, in.PythonDependents); ok {
			flags = append(flags, f)
		}
		if f, ok := majorBumpFlag(u); ok {
			flags = append(flags, f)
		}
		if f, ok := watchlistFlag(u, in.Watchlist); ok {
			flags = append(flags, f)
		}
		if IsKernel(u.Name) {
			res.RebootRequired = true
			flags = append(flags, Flag{Rule: RuleKernel, Severity: Info, Reason: "Kernel update: reboot after upgrading"})
		}
		slices.SortStableFunc(flags, func(a, b Flag) int {
			return cmp.Or(cmp.Compare(b.Severity, a.Severity), cmp.Compare(a.Rule, b.Rule))
		})
		res.Packages = append(res.Packages, Package{Update: u, Flags: flags})
	}
	return res
}

var interventionPhrases = []string{"manual intervention", "intervention required", "requires manual"}

// RequiresManualIntervention reports whether a news item's title or body
// says the upgrade needs manual steps.
func RequiresManualIntervention(it news.Item) bool {
	text := strings.ToLower(it.Title + " " + it.Text)
	for _, p := range interventionPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

func newsFlags(u Update, items []news.Item, now time.Time) []Flag {
	var flags []Flag
	for _, it := range items {
		if !mentionsPackage(it, u) {
			continue
		}
		f := Flag{Rule: RuleNews, Severity: Warn, NewsGUID: it.GUID}
		label := "Mentioned in news"
		if RequiresManualIntervention(it) {
			f.Severity = Critical
			label = "Manual intervention in news"
		}
		f.Reason = fmt.Sprintf("%s: '%s'", label, it.Title)
		if !it.Published.IsZero() {
			f.Reason += " (" + ago(it.Published, now) + ")"
		}
		flags = append(flags, f)
	}
	return flags
}

func mentionsPackage(it news.Item, u Update) bool {
	for _, name := range []string{u.Name, u.base()} {
		if Mentions(it.Title, name) || Mentions(it.Text, name) {
			return true
		}
	}
	return false
}

// Mentions reports whether text mentions the package name as a whole word,
// ignoring case. Package names contain '-', '.', '+', '_' and '@', so
// "linux" is not mentioned by "linux-firmware". A trailing '.' that ends a
// sentence still counts as a boundary. "Arch Linux" is the distribution,
// not the linux package.
func Mentions(text, name string) bool {
	if name == "" {
		return false
	}
	lower := strings.ToLower(text)
	name = strings.ToLower(name)
	for from := 0; ; {
		i := strings.Index(lower[from:], name)
		if i < 0 {
			return false
		}
		start := from + i
		end := start + len(name)
		from = start + 1
		if start > 0 && isNameByte(lower[start-1]) {
			continue
		}
		if end < len(lower) && isNameByte(lower[end]) {
			// "foo." at the end of a sentence is a mention; "foo.so" isn't.
			if lower[end] != '.' || (end+1 < len(lower) && isNameByte(lower[end+1])) {
				continue
			}
		}
		if name == "linux" && strings.HasSuffix(lower[:start], "arch ") {
			continue
		}
		return true
	}
}

func isNameByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("-._+@", c) >= 0
}

func ago(t, now time.Time) string {
	days := int(now.Sub(t) / (24 * time.Hour))
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "yesterday"
	}
	return fmt.Sprintf("%d days ago", days)
}

func watchlistFlag(u Update, patterns []string) (Flag, bool) {
	for _, p := range patterns {
		for _, name := range []string{u.Name, u.base()} {
			// path.Match only fails on malformed patterns, which never match.
			if ok, err := path.Match(p, name); err == nil && ok {
				return Flag{Rule: RuleWatchlist, Severity: Info, Reason: fmt.Sprintf("On your watchlist (%s)", p)}, true
			}
		}
	}
	return Flag{}, false
}

// upstreamVersion strips the epoch and pkgrel from an Arch version:
// "1:2.4.8-2" becomes "2.4.8".
func upstreamVersion(v string) string {
	if i := strings.IndexByte(v, ':'); i >= 0 {
		if _, err := strconv.Atoi(v[:i]); err == nil {
			v = v[i+1:]
		}
	}
	if i := strings.LastIndexByte(v, '-'); i >= 0 {
		v = v[:i]
	}
	return v
}

// leadingNumber returns the leading run of digits of v, if any.
func leadingNumber(v string) (string, bool) {
	n := 0
	for n < len(v) && v[n] >= '0' && v[n] <= '9' {
		n++
	}
	return v[:n], n > 0
}

// calendarDigits is the length from which a leading number is taken to be a
// date (20251021) rather than a major version.
const calendarDigits = 8

func majorBumpFlag(u Update) (Flag, bool) {
	oldS, ok1 := leadingNumber(upstreamVersion(u.OldVersion))
	newS, ok2 := leadingNumber(upstreamVersion(u.NewVersion))
	if !ok1 || !ok2 || len(oldS) >= calendarDigits || len(newS) >= calendarDigits {
		return Flag{}, false
	}
	oldN, err1 := strconv.Atoi(oldS)
	newN, err2 := strconv.Atoi(newS)
	// An epoch bump can restart upstream numbering lower; that isn't a
	// major bump.
	if err1 != nil || err2 != nil || newN <= oldN {
		return Flag{}, false
	}
	return Flag{Rule: RuleMajorBump, Severity: Warn, Reason: fmt.Sprintf("Major version bump (%d → %d)", oldN, newN)}, true
}

// pythonMinor returns "3.13" for "3.13.7-1".
func pythonMinor(v string) (string, bool) {
	parts := strings.SplitN(upstreamVersion(v), ".", 3)
	if len(parts) < 2 {
		return "", false
	}
	for _, p := range parts[:2] {
		if _, err := strconv.Atoi(p); err != nil {
			return "", false
		}
	}
	return parts[0] + "." + parts[1], true
}

func pythonFlag(u Update, dependents []string) (Flag, bool) {
	if u.Name != "python" {
		return Flag{}, false
	}
	oldV, ok1 := pythonMinor(u.OldVersion)
	newV, ok2 := pythonMinor(u.NewVersion)
	if !ok1 || !ok2 || oldV == newV {
		return Flag{}, false
	}
	reason := fmt.Sprintf("Python %s → %s: ", oldV, newV)
	if len(dependents) == 0 {
		reason += "AUR packages that use Python may need rebuilding"
	} else {
		reason += "rebuild these AUR packages: " + strings.Join(dependents, ", ")
	}
	return Flag{Rule: RulePythonBump, Severity: Warn, Reason: reason}, true
}

// notKernels are linux-* packages that aren't kernels, so updating them
// doesn't call for a reboot.
var notKernels = []string{"linux-api-headers", "linux-firmware*", "linux-tools*", "*-headers", "*-docs"}

// IsKernel reports whether name is a kernel package: linux or linux-*, other
// than headers, docs, firmware and tools.
func IsKernel(name string) bool {
	if name != "linux" && !strings.HasPrefix(name, "linux-") {
		return false
	}
	for _, p := range notKernels {
		if ok, _ := path.Match(p, name); ok {
			return false
		}
	}
	return true
}
