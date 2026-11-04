package security

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/leNITHISH/cronen/internal/system"
)

// URL is the tracker's list of every advisory group.
const URL = "https://security.archlinux.org/all.json"

// TTL is how long the tracker's list is cached. The tracker lags, so there
// is no point asking more often.
const TTL = 6 * time.Hour

// Status values of a group that matter here.
const (
	StatusFixed       = "Fixed"
	StatusNotAffected = "Not affected"
)

// Group is one advisory group (AVG-…) from the tracker.
type Group struct {
	Name     string
	Packages []string
	Status   string
	Severity string
	// Affected is the first version known to be vulnerable.
	Affected string
	// Fixed is the version that fixes the group, or empty if none does.
	Fixed string
	// CVEs are the issues the group covers (CVE ids).
	CVEs []string
}

// wire is the JSON shape of one group. Field names are as the tracker is
// believed to spell them; see "Needs checking on Arch" in PLAN.md.
type wire struct {
	Name     string   `json:"name"`
	Packages []string `json:"packages"`
	Status   string   `json:"status"`
	Severity string   `json:"severity"`
	Affected string   `json:"affected"`
	Fixed    *string  `json:"fixed"`
	Issues   []string `json:"issues"`
}

// Parse decodes the tracker's all.json: an array of groups.
func Parse(data []byte) ([]Group, error) {
	var ws []wire
	if err := json.Unmarshal(data, &ws); err != nil {
		return nil, fmt.Errorf("security tracker: decoding: %w", err)
	}
	groups := make([]Group, 0, len(ws))
	for _, w := range ws {
		g := Group{
			Name: clean(w.Name), Packages: w.Packages, Status: clean(w.Status),
			Severity: clean(w.Severity), Affected: clean(w.Affected),
		}
		if w.Fixed != nil {
			g.Fixed = clean(*w.Fixed)
		}
		for _, c := range w.Issues {
			g.CVEs = append(g.CVEs, clean(c))
		}
		groups = append(groups, g)
	}
	return groups, nil
}

// clean drops control characters from text that came off the network.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
}

// Client fetches the tracker.
type Client struct {
	Fetcher system.Fetcher
	// URL defaults to the URL constant.
	URL string
}

// NewClient returns a Client using f.
func NewClient(f system.Fetcher) *Client {
	return &Client{Fetcher: f, URL: URL}
}

// Groups returns every advisory group. If fresh is true the cache is
// bypassed.
func (c *Client) Groups(ctx context.Context, fresh bool) ([]Group, error) {
	u := c.URL
	if u == "" {
		u = URL
	}
	body, err := system.GetTTL(ctx, c.Fetcher, u, TTL, fresh)
	if err != nil {
		return nil, fmt.Errorf("security tracker: %w", err)
	}
	return Parse(body)
}

// Compare is a version comparison like vercmp: negative if a is older than
// b, zero if equal, positive if a is newer.
type Compare func(a, b string) (int, error)

// Fix is an advisory group an update fixes.
type Fix struct {
	Group    string
	Severity string
	CVEs     []string
	// Fixed is the version that fixes the group.
	Fixed string
}

// Pending is a pending update.
type Pending struct {
	Name string
	// Base is the pkgbase, or empty if unknown.
	Base      string
	Installed string
	New       string
}

func (g Group) mentions(names ...string) bool {
	for _, p := range g.Packages {
		for _, n := range names {
			if n != "" && p == n {
				return true
			}
		}
	}
	return false
}

// FixesFor returns the groups that updating p fixes: those with installed <
// fixed <= new, matched on the package name and on its pkgbase.
func FixesFor(groups []Group, p Pending, cmp Compare) ([]Fix, error) {
	var fixes []Fix
	for _, g := range groups {
		if g.Fixed == "" || g.Status == StatusNotAffected || !g.mentions(p.Name, p.Base) {
			continue
		}
		before, err := cmp(p.Installed, g.Fixed)
		if err != nil {
			return nil, err
		}
		if before >= 0 {
			continue
		}
		upTo, err := cmp(g.Fixed, p.New)
		if err != nil {
			return nil, err
		}
		if upTo > 0 {
			continue
		}
		fixes = append(fixes, Fix{Group: g.Name, Severity: g.Severity, CVEs: g.CVEs, Fixed: g.Fixed})
	}
	return fixes, nil
}

// Installed is an installed package and its version.
type Installed struct {
	Name    string
	Version string
}

// Vulnerable is an installed package with a known problem that no version
// fixes yet.
type Vulnerable struct {
	Name      string
	Installed string
	Group     string
	Severity  string
	CVEs      []string
	Status    string
}

// Unfixed returns the installed packages affected by a group that has no fix
// newer than the installed version: the group's status isn't Fixed (or Not
// affected), the installed version is at or past Affected, and Fixed is
// empty or not newer than the installed version.
func Unfixed(groups []Group, installed []Installed, cmp Compare) ([]Vulnerable, error) {
	byName := map[string][]Group{}
	for _, g := range groups {
		if g.Status == StatusFixed || g.Status == StatusNotAffected {
			continue
		}
		for _, p := range g.Packages {
			byName[p] = append(byName[p], g)
		}
	}
	var out []Vulnerable
	for _, in := range installed {
		for _, g := range byName[in.Name] {
			if g.Affected != "" {
				c, err := cmp(in.Version, g.Affected)
				if err != nil {
					return nil, err
				}
				if c < 0 {
					continue
				}
			}
			if g.Fixed != "" {
				c, err := cmp(g.Fixed, in.Version)
				if err != nil {
					return nil, err
				}
				if c > 0 {
					// A fix exists; it just isn't installed.
					continue
				}
			}
			out = append(out, Vulnerable{
				Name: in.Name, Installed: in.Version, Group: g.Name,
				Severity: g.Severity, CVEs: g.CVEs, Status: g.Status,
			})
		}
	}
	return out, nil
}

// Rank orders severities, highest first: Critical, High, Medium, Low, and
// anything else last.
func Rank(severity string) int {
	switch strings.ToLower(severity) {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	}
	return 4
}

// Describe is "AVG-2843 (High): CVE-2025-1234, CVE-2025-1235".
func (f Fix) Describe() string {
	s := f.Group
	if f.Severity != "" {
		s += " (" + f.Severity + ")"
	}
	if len(f.CVEs) > 0 {
		s += ": " + strings.Join(f.CVEs, ", ")
	}
	return s
}
