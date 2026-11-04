package aur

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/system"
)

const cgitBase = "https://aur.archlinux.org/cgit/aur.git"

// Commit is one commit of a package base's AUR git history.
type Commit struct {
	ID     string
	Title  string
	Author string
	Date   time.Time
}

// LogURL is the cgit atom feed of a package base's commits.
func LogURL(pkgbase string) string {
	return cgitBase + "/atom/?h=" + url.QueryEscape(pkgbase)
}

// DiffURL is the cgit raw diff of a package base between two commits: what
// changed from old to newest.
func DiffURL(pkgbase, newest, old string) string {
	q := url.Values{"h": {pkgbase}, "id": {newest}, "id2": {old}}
	return cgitBase + "/rawdiff/?" + q.Encode()
}

type atomFeed struct {
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title     string `xml:"title"`
	ID        string `xml:"id"`
	Updated   string `xml:"updated"`
	Published string `xml:"published"`
	Author    struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Links []struct {
		Href string `xml:"href,attr"`
	} `xml:"link"`
}

var commitIDRe = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// ParseAtom parses cgit's atom feed. Entries without a usable commit id or
// date are skipped. The commits come back newest first.
func ParseAtom(data []byte) ([]Commit, error) {
	var feed atomFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, fmt.Errorf("AUR log: %w", err)
	}
	var commits []Commit
	for _, e := range feed.Entries {
		// cgit writes the id as "urn:sha1:<sha>".
		id := strings.TrimPrefix(strings.TrimSpace(e.ID), "urn:sha1:")
		if !commitIDRe.MatchString(id) {
			// cgit puts the commit id in the link too.
			id = ""
			for _, l := range e.Links {
				if u, err := url.Parse(l.Href); err == nil && commitIDRe.MatchString(u.Query().Get("id")) {
					id = u.Query().Get("id")
				}
			}
		}
		if id == "" {
			continue
		}
		var date time.Time
		for _, s := range []string{e.Published, e.Updated} {
			if t, err := time.Parse(time.RFC3339, strings.TrimSpace(s)); err == nil {
				date = t
				break
			}
		}
		if date.IsZero() {
			continue
		}
		commits = append(commits, Commit{
			ID: id, Title: strings.TrimSpace(e.Title),
			Author: strings.TrimSpace(e.Author.Name), Date: date.UTC(),
		})
	}
	slices.SortStableFunc(commits, func(a, b Commit) int { return b.Date.Compare(a.Date) })
	return commits, nil
}

// BaseCommit picks the commit to diff against: the newest one dated at or
// before the installed package's build date, which is the PKGBUILD the
// installed package was most likely built from. commits must be newest
// first, as ParseAtom returns them. It reports false if there is none, or
// the build date is unknown.
func BaseCommit(commits []Commit, built time.Time) (Commit, bool) {
	if built.IsZero() {
		return Commit{}, false
	}
	for _, c := range commits {
		if !c.Date.After(built) {
			return c, true
		}
	}
	return Commit{}, false
}

// Review is what cronen shows about an AUR update's history.
type Review struct {
	// Commits is the full log, newest first.
	Commits []Commit
	// Base is the commit the diff starts from, valid when HasBase is set.
	Base    Commit
	HasBase bool
	// Diff is the raw diff from Base to the newest commit. It is empty
	// when there is nothing to compare.
	Diff string
	// Note says why there is no diff, or "".
	Note string
	// Findings are the suspicious changes in Diff.
	Findings []Finding
}

// Log fetches a package base's commits, newest first.
func (c *Client) Log(ctx context.Context, pkgbase string, fresh bool) ([]Commit, error) {
	if pkgbase == "" {
		return nil, errors.New("AUR log: no package base")
	}
	body, err := c.Fetcher.Get(ctx, LogURL(pkgbase), fresh)
	if err != nil {
		return nil, fmt.Errorf("AUR log: %w", err)
	}
	return ParseAtom(body)
}

// Review fetches the log of pkgbase and the diff between the commit the
// installed package was built from (the newest commit dated at or before
// built) and the newest one. With no such commit it returns the log, no
// diff, and a note saying why.
func (c *Client) Review(ctx context.Context, pkgbase string, built time.Time, fresh bool) (Review, error) {
	commits, err := c.Log(ctx, pkgbase, fresh)
	if err != nil {
		return Review{}, err
	}
	return c.diffFor(ctx, pkgbase, commits, built, fresh)
}

// ReviewLog is Review for a log that is already fetched.
func (c *Client) ReviewLog(ctx context.Context, pkgbase string, commits []Commit, built time.Time, fresh bool) (Review, error) {
	return c.diffFor(ctx, pkgbase, commits, built, fresh)
}

func (c *Client) diffFor(ctx context.Context, pkgbase string, commits []Commit, built time.Time, fresh bool) (Review, error) {
	r := Review{Commits: commits}
	switch {
	case len(commits) == 0:
		r.Note = "the AUR has no commits for this package"
		return r, nil
	case built.IsZero():
		r.Note = "the installed package's build date is unknown"
		return r, nil
	}
	base, ok := BaseCommit(commits, built)
	if !ok {
		r.Note = "every commit is newer than the installed build (" + built.UTC().Format("2006-01-02") + ")"
		return r, nil
	}
	r.Base, r.HasBase = base, true
	if base.ID == commits[0].ID {
		r.Note = "the installed build already has the newest commit"
		return r, nil
	}
	body, err := c.Fetcher.Get(ctx, DiffURL(pkgbase, commits[0].ID, base.ID), fresh)
	if err != nil {
		return Review{}, fmt.Errorf("AUR diff: %w", err)
	}
	r.Diff = string(body)
	r.Findings = AnalyzeDiff(r.Diff)
	return r, nil
}

// BuildDate is the installed build date of name, from pacman -Qi.
func BuildDate(ctx context.Context, r system.Runner, name string, loc *time.Location) (time.Time, error) {
	infos, err := pacman.LocalInfo(ctx, r, []string{name}, loc)
	if err != nil {
		return time.Time{}, err
	}
	return infos[name].BuildDate, nil
}

// Finding is one suspicious line in a PKGBUILD diff.
type Finding struct {
	// Kind identifies the heuristic.
	Kind FindingKind
	// Reason is a short explanation, without the line.
	Reason string
	// Line is the 0-based line of the diff the finding is on.
	Line int
	// Text is that line, without its leading "+".
	Text string
}

// FindingKind is a suspicious-change heuristic.
type FindingKind int

// The heuristics of rule 13.
const (
	FindSourceHost FindingKind = iota + 1
	FindInstall
	FindPipeToShell
	FindDecodeRun
	FindSkipChecksum
)

var (
	urlHostRe     = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://([^/\s'")]+)`)
	sourceStartRe = regexp.MustCompile(`^\s*source(_[A-Za-z0-9_]+)?\+?=\(`)
	installRe     = regexp.MustCompile(`^\s*install=`)
	sumsRe        = regexp.MustCompile(`^\s*(?:md5|sha1|sha224|sha256|sha384|sha512|b2|ck)sums(_[A-Za-z0-9_]+)?\+?=`)
	skipRe        = regexp.MustCompile(`['"]SKIP['"]`)
	pipeShellRe   = regexp.MustCompile(`\b(?:curl|wget)\b[^|\n]*\|\s*(?:sudo\s+(?:-\S+\s+)*)?(?:ba|z|da|k)?sh\b`)
	decodeRe      = regexp.MustCompile(`\bbase64\s+(?:-d\b|-D\b|--decode\b)|\bxxd\s+-r|(?:^|[\s;&|(])eval\s`)
	vcsSourceRe   = regexp.MustCompile(`(?:^|[\s'"(:])(?:git|svn|hg|bzr|fossil)\+`)
)

// hostOf returns the lower-cased host of the first URL in s, or "".
func hostOf(s string) string {
	m := urlHostRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	h := strings.ToLower(m[1])
	if i := strings.LastIndexByte(h, '@'); i >= 0 {
		h = h[i+1:]
	}
	return h
}

type addedLine struct {
	n    int
	text string
}

// trackArray reports whether text is inside a source array, given whether
// the previous line was, and updates *in for the next line.
func trackArray(in *bool, text string) bool {
	if sourceStartRe.MatchString(text) {
		*in = true
	}
	inside := *in
	if inside && strings.Contains(text, ")") {
		*in = false
	}
	return inside
}

// AnalyzeDiff applies rule 13's heuristics to a unified diff of a package
// base. Only added lines are examined, and comments are skipped. The
// findings come back in diff order.
func AnalyzeDiff(diff string) []Finding {
	var (
		findings []Finding
		file     string
		// Per-file state, for the PKGBUILD's source array.
		inSource, inSourceOld    bool
		removedHosts, addedHosts []addedLine
		sawVCS, sawRemote        bool
		skips                    []addedLine
		inSums                   bool
		installSeen              = map[string]bool{}
	)
	flushFile := func() {
		if file == "PKGBUILD" || file == "" {
			for _, a := range addedHosts {
				h := hostOf(a.text)
				if len(removedHosts) > 0 && !slices.ContainsFunc(removedHosts, func(r addedLine) bool { return hostOf(r.text) == h }) {
					findings = append(findings, Finding{
						Kind: FindSourceHost, Line: a.n, Text: a.text,
						Reason: fmt.Sprintf("changes the host of a source URL (%s → %s)", hostOf(removedHosts[0].text), h),
					})
				}
			}
			if sawRemote || !sawVCS {
				for _, s := range skips {
					findings = append(findings, Finding{
						Kind: FindSkipChecksum, Line: s.n, Text: s.text,
						Reason: "sets a checksum to SKIP for a source that isn't a VCS checkout",
					})
				}
			}
		}
		file, inSource, inSourceOld, inSums = "", false, false, false
		removedHosts, addedHosts, skips = nil, nil, nil
		sawVCS, sawRemote = false, false
	}

	lines := strings.Split(diff, "\n")
	for i, raw := range lines {
		switch {
		case strings.HasPrefix(raw, "diff --git "):
			flushFile()
			// diff --git a/PKGBUILD b/PKGBUILD
			if f := strings.Fields(raw); len(f) >= 4 {
				file = strings.TrimPrefix(f[3], "b/")
			}
			continue
		case strings.HasPrefix(raw, "+++ "):
			if f := strings.TrimPrefix(raw, "+++ "); f != "/dev/null" && file == "" {
				file = strings.TrimPrefix(f, "b/")
			}
			continue
		case strings.HasPrefix(raw, "--- "), strings.HasPrefix(raw, "index "), strings.HasPrefix(raw, "new file"), strings.HasPrefix(raw, "deleted file"):
			continue
		case strings.HasPrefix(raw, "@@"):
			inSource, inSourceOld, inSums = false, false, false
			continue
		}
		if raw == "" {
			continue
		}
		sign, text := raw[0], raw[1:]
		if sign != '+' && sign != '-' && sign != ' ' {
			continue
		}
		trimmed := strings.TrimSpace(text)
		isComment := strings.HasPrefix(trimmed, "#")

		if strings.HasSuffix(file, ".install") && sign == '+' && !isComment && trimmed != "" && !installSeen[file] {
			installSeen[file] = true
			findings = append(findings, Finding{
				Kind: FindInstall, Line: i, Text: text,
				Reason: "changes the install script " + file,
			})
		}
		if file != "PKGBUILD" && file != "" {
			continue
		}
		if isComment {
			continue
		}

		// Track the source array on each side of the diff: the old side is
		// context and removed lines, the new side context and added ones.
		// A removed line closing the old array must not close the new one.
		if sign != '+' && trackArray(&inSourceOld, text) {
			if h := hostOf(text); h != "" && sign == '-' {
				removedHosts = append(removedHosts, addedLine{i, text})
			}
		}
		if sign != '-' && trackArray(&inSource, text) {
			if h := hostOf(text); h != "" {
				if sign == '+' {
					addedHosts = append(addedHosts, addedLine{i, text})
				}
				if vcsSourceRe.MatchString(text) {
					sawVCS = true
				} else {
					sawRemote = true
				}
			}
		}
		if sign == '-' {
			continue
		}
		if sumsRe.MatchString(text) {
			inSums = true
		}
		if inSums {
			if sign == '+' && skipRe.MatchString(text) {
				skips = append(skips, addedLine{i, text})
			}
			if strings.Contains(text, ")") {
				inSums = false
			}
		}
		if sign != '+' {
			continue
		}

		// Added lines.
		if installRe.MatchString(text) {
			findings = append(findings, Finding{
				Kind: FindInstall, Line: i, Text: text,
				Reason: "adds or changes an install= script",
			})
		}
		if pipeShellRe.MatchString(text) {
			findings = append(findings, Finding{
				Kind: FindPipeToShell, Line: i, Text: text,
				Reason: "pipes a download into a shell",
			})
		}
		if decodeRe.MatchString(text) {
			findings = append(findings, Finding{
				Kind: FindDecodeRun, Line: i, Text: text,
				Reason: "decodes data or evals a string (base64 -d, xxd -r, eval)",
			})
		}

	}
	flushFile()
	slices.SortStableFunc(findings, func(a, b Finding) int { return a.Line - b.Line })
	return findings
}
