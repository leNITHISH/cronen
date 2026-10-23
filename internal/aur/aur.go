package aur

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/system"
)

const (
	// DefaultInfoURL is the AUR RPC v5 info endpoint.
	DefaultInfoURL = "https://aur.archlinux.org/rpc/v5/info"
	// DefaultBatchSize is the most package names sent in one RPC request,
	// which keeps request URLs a sane length.
	DefaultBatchSize = 150
)

// Info is the AUR's record of one package.
type Info struct {
	Name        string
	PackageBase string
	Version     string
	// URL is the upstream project URL.
	URL string
	// Maintainer is empty for orphaned packages.
	Maintainer string
	// OutOfDate is when the package was flagged out of date, or the zero
	// time if it isn't flagged.
	OutOfDate time.Time
}

// Orphaned reports whether the package has no maintainer.
func (i Info) Orphaned() bool { return i.Maintainer == "" }

// FlaggedOutOfDate reports whether the package is flagged out of date.
func (i Info) FlaggedOutOfDate() bool { return !i.OutOfDate.IsZero() }

// ChangelogURL is the AUR packaging history of the package's pkgbase.
func (i Info) ChangelogURL() string {
	base := i.PackageBase
	if base == "" {
		base = i.Name
	}
	return "https://aur.archlinux.org/cgit/aur.git/log/?h=" + url.QueryEscape(base)
}

// Package is the status of one installed foreign package.
type Package struct {
	Name             string
	InstalledVersion string
	// Info is the package's AUR record. It is nil if the package is not in
	// the AUR.
	Info *Info
	// Pending reports whether the AUR has a newer version than the one
	// installed.
	Pending bool
}

// Missing reports whether the package is installed but not in the AUR
// (orphaned and deleted, renamed, or never from the AUR).
func (p Package) Missing() bool { return p.Info == nil }

// Client queries the AUR RPC v5 interface.
type Client struct {
	Fetcher system.Fetcher
	// Runner runs vercmp.
	Runner system.Runner
	// InfoURL defaults to DefaultInfoURL.
	InfoURL string
	// BatchSize defaults to DefaultBatchSize.
	BatchSize int
}

// NewClient returns a Client using the default endpoint and batch size.
func NewClient(f system.Fetcher, r system.Runner) *Client {
	return &Client{Fetcher: f, Runner: r}
}

type rpcResult struct {
	Name        string  `json:"Name"`
	PackageBase string  `json:"PackageBase"`
	Version     string  `json:"Version"`
	URL         *string `json:"URL"`
	Maintainer  *string `json:"Maintainer"`
	OutOfDate   *int64  `json:"OutOfDate"`
}

type rpcResponse struct {
	Type    string      `json:"type"`
	Error   string      `json:"error"`
	Results []rpcResult `json:"results"`
}

// Info looks up names in the AUR, batching requests, and returns the
// records found keyed by name. Names the AUR doesn't know are absent from
// the map. If fresh is true, cached responses are bypassed.
func (c *Client) Info(ctx context.Context, names []string, fresh bool) (map[string]Info, error) {
	batch := c.BatchSize
	if batch <= 0 {
		batch = DefaultBatchSize
	}

	seen := map[string]bool{}
	var unique []string
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			unique = append(unique, n)
		}
	}

	infos := map[string]Info{}
	for start := 0; start < len(unique); start += batch {
		end := min(start+batch, len(unique))
		if err := c.fetchBatch(ctx, unique[start:end], fresh, infos); err != nil {
			return nil, err
		}
	}
	return infos, nil
}

func (c *Client) fetchBatch(ctx context.Context, names []string, fresh bool, into map[string]Info) error {
	base := c.InfoURL
	if base == "" {
		base = DefaultInfoURL
	}
	q := url.Values{"arg[]": names}
	body, err := c.Fetcher.Get(ctx, base+"?"+q.Encode(), fresh)
	if err != nil {
		return fmt.Errorf("AUR info: %w", err)
	}

	var resp rpcResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("AUR info: decoding response: %w", err)
	}
	if resp.Type == "error" || resp.Error != "" {
		return fmt.Errorf("AUR info: %s", resp.Error)
	}

	requested := map[string]bool{}
	for _, n := range names {
		requested[n] = true
	}
	for _, r := range resp.Results {
		if !requested[r.Name] {
			continue
		}
		info := Info{Name: r.Name, PackageBase: r.PackageBase, Version: r.Version}
		if r.URL != nil {
			info.URL = *r.URL
		}
		if r.Maintainer != nil {
			info.Maintainer = *r.Maintainer
		}
		if r.OutOfDate != nil {
			info.OutOfDate = time.Unix(*r.OutOfDate, 0).UTC()
		}
		into[r.Name] = info
	}
	return nil
}

// Check looks up the installed foreign packages in the AUR and returns
// their status, in the same order. Every package is returned, not only
// pending ones, because missing, orphaned and out-of-date packages are
// worth showing without a version bump. Versions are compared with vercmp;
// a package installed at a newer version than the AUR's (common for -git
// packages) is not pending.
func (c *Client) Check(ctx context.Context, installed []pacman.Package, fresh bool) ([]Package, error) {
	if c.Runner == nil {
		return nil, errors.New("AUR check: no Runner for vercmp")
	}
	names := make([]string, len(installed))
	for i, p := range installed {
		names[i] = p.Name
	}
	infos, err := c.Info(ctx, names, fresh)
	if err != nil {
		return nil, err
	}

	pkgs := make([]Package, 0, len(installed))
	for _, p := range installed {
		pkg := Package{Name: p.Name, InstalledVersion: p.Version}
		if info, ok := infos[p.Name]; ok {
			pkg.Info = &info
			cmp, err := pacman.Vercmp(ctx, c.Runner, p.Version, info.Version)
			if err != nil {
				return nil, fmt.Errorf("AUR check %s: %w", p.Name, err)
			}
			pkg.Pending = cmp < 0
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs, nil
}
