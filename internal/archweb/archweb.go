package archweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/leNITHISH/cronen/internal/system"
)

// SearchURL is the archlinux.org package search JSON endpoint.
const SearchURL = "https://archlinux.org/packages/search/json/"

// ErrNotFound reports a package that isn't in the official repos.
var ErrNotFound = errors.New("package not found in the official repos")

// Package is the metadata cronen needs about a repo package.
type Package struct {
	Name    string
	Base    string
	Repo    string
	Arch    string
	Version string
	// URL is the upstream project URL. It may be empty.
	URL string
}

// ChangelogURL returns the packaging commit log on the Arch GitLab.
//
// GitLab project paths are derived from pkgbase, which is used verbatim
// here; pkgbases with characters GitLab rewrites (such as '+') may not
// resolve.
func (p Package) ChangelogURL() string {
	return "https://gitlab.archlinux.org/archlinux/packaging/packages/" + url.PathEscape(p.Base) + "/-/commits/main"
}

// Client looks up packages on archlinux.org.
type Client struct {
	Fetcher system.Fetcher
	// SearchURL defaults to the package SearchURL constant.
	SearchURL string
}

// NewClient returns a Client using f.
func NewClient(f system.Fetcher) *Client {
	return &Client{Fetcher: f, SearchURL: SearchURL}
}

type searchResponse struct {
	Valid   bool           `json:"valid"`
	Results []searchResult `json:"results"`
}

type searchResult struct {
	Name  string `json:"pkgname"`
	Base  string `json:"pkgbase"`
	Repo  string `json:"repo"`
	Arch  string `json:"arch"`
	Ver   string `json:"pkgver"`
	Rel   string `json:"pkgrel"`
	Epoch int    `json:"epoch"`
	URL   string `json:"url"`
}

// Lookup returns the metadata for the repo package named name. It returns
// ErrNotFound if no official repo has it. When several repos carry the
// package, stable repos win over testing and staging ones. If fresh is
// true any cached response is bypassed.
func (c *Client) Lookup(ctx context.Context, name string, fresh bool) (Package, error) {
	base := c.SearchURL
	if base == "" {
		base = SearchURL
	}
	body, err := c.Fetcher.Get(ctx, base+"?"+url.Values{"name": {name}}.Encode(), fresh)
	if err != nil {
		return Package{}, fmt.Errorf("archweb lookup %s: %w", name, err)
	}
	var resp searchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return Package{}, fmt.Errorf("archweb lookup %s: decoding response: %w", name, err)
	}
	if !resp.Valid {
		return Package{}, fmt.Errorf("archweb lookup %s: server rejected the query", name)
	}

	var best *searchResult
	for i := range resp.Results {
		r := &resp.Results[i]
		if r.Name != name {
			continue
		}
		if best == nil || repoRank(r.Repo) < repoRank(best.Repo) {
			best = r
		}
	}
	if best == nil {
		return Package{}, fmt.Errorf("archweb lookup %s: %w", name, ErrNotFound)
	}
	return best.pkg(), nil
}

func (r searchResult) pkg() Package {
	v := r.Ver
	if r.Rel != "" {
		v += "-" + r.Rel
	}
	if r.Epoch > 0 {
		v = fmt.Sprintf("%d:%s", r.Epoch, v)
	}
	b := r.Base
	if b == "" {
		b = r.Name
	}
	return Package{Name: r.Name, Base: b, Repo: r.Repo, Arch: r.Arch, Version: v, URL: r.URL}
}

// repoRank orders repos so stable ones sort first.
func repoRank(repo string) int {
	switch {
	case strings.HasSuffix(repo, "-staging"):
		return 2
	case strings.HasSuffix(repo, "-testing"):
		return 1
	default:
		return 0
	}
}
