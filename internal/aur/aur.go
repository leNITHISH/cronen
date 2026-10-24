package aur

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
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

// Kind says how an installed foreign package relates to the AUR.
type Kind int

// Package kinds.
const (
	// KindRegular is an ordinary AUR package, compared by version.
	KindRegular Kind = iota
	// KindDebug is a makepkg debug split (<name>-debug of an installed
	// <name>). Debug splits are never on the AUR, so they aren't looked up.
	KindDebug
	// KindVCS is a package built from a VCS checkout (-git, -svn, ...).
	// The AUR's version is just the last snapshot, so it isn't compared.
	KindVCS
)

func (k Kind) String() string {
	switch k {
	case KindRegular:
		return "regular"
	case KindDebug:
		return "debug"
	case KindVCS:
		return "vcs"
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

// vcsSuffixes are the package name suffixes of VCS packages.
var vcsSuffixes = []string{"-git", "-svn", "-hg", "-bzr", "-fossil", "-darcs", "-cvs"}

// Classify returns the kind of the foreign package name. installed reports
// whether a package is installed; it decides whether <name>-debug is a debug
// split.
func Classify(name string, installed func(string) bool) Kind {
	if base, ok := strings.CutSuffix(name, "-debug"); ok && base != "" && installed(base) {
		return KindDebug
	}
	for _, suffix := range vcsSuffixes {
		if strings.HasSuffix(name, suffix) && len(name) > len(suffix) {
			return KindVCS
		}
	}
	return KindRegular
}

// Package is the status of one installed foreign package.
type Package struct {
	Name             string
	InstalledVersion string
	Kind             Kind
	// Info is the package's AUR record. It is nil if the package is not in
	// the AUR.
	Info *Info
	// Pending reports whether the AUR has a newer version than the one
	// installed. It is always false for debug and VCS packages.
	Pending bool
}

// Missing reports whether the package is installed but not in the AUR
// (orphaned and deleted, renamed, or never from the AUR). Debug splits are
// never missing: they were never meant to be on the AUR.
func (p Package) Missing() bool { return p.Info == nil && p.Kind != KindDebug }

// Client queries the AUR RPC v5 interface.
type Client struct {
	Fetcher system.Fetcher
	// Runner runs vercmp.
	Runner system.Runner
	// InfoURL defaults to DefaultInfoURL.
	InfoURL string
	// BatchSize defaults to DefaultBatchSize.
	BatchSize int
	// Installed holds the names of every installed package (pacman -Qq),
	// used to recognize debug splits of repo packages. If nil, only the
	// foreign packages passed to Check count as installed.
	Installed map[string]bool
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
// a package installed at a newer version than the AUR's is not pending.
// Debug splits aren't looked up, and VCS packages are looked up but never
// compared; neither is ever pending.
func (c *Client) Check(ctx context.Context, installed []pacman.Package, fresh bool) ([]Package, error) {
	if c.Runner == nil {
		return nil, errors.New("AUR check: no Runner for vercmp")
	}
	foreign := map[string]bool{}
	for _, p := range installed {
		foreign[p.Name] = true
	}
	isInstalled := func(name string) bool { return foreign[name] || c.Installed[name] }

	kinds := make([]Kind, len(installed))
	var names []string
	for i, p := range installed {
		kinds[i] = Classify(p.Name, isInstalled)
		if kinds[i] != KindDebug {
			names = append(names, p.Name)
		}
	}
	infos, err := c.Info(ctx, names, fresh)
	if err != nil {
		return nil, err
	}

	pkgs := make([]Package, 0, len(installed))
	for i, p := range installed {
		pkg := Package{Name: p.Name, InstalledVersion: p.Version, Kind: kinds[i]}
		if info, ok := infos[p.Name]; ok {
			pkg.Info = &info
		}
		if pkg.Info != nil && pkg.Kind == KindRegular {
			cmp, err := pacman.Vercmp(ctx, c.Runner, p.Version, pkg.Info.Version)
			if err != nil {
				return nil, fmt.Errorf("AUR check %s: %w", p.Name, err)
			}
			pkg.Pending = cmp < 0
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs, nil
}
