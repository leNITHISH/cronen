package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/leNITHISH/cronen/internal/archweb"
	"github.com/leNITHISH/cronen/internal/system"
)

// APIBase is the GitLab REST API root.
const APIBase = "https://gitlab.archlinux.org/api/v4/projects/"

// TTL is how long a project's issues are cached.
const TTL = time.Hour

// MaxConcurrent bounds the requests in flight at once.
const MaxConcurrent = 4

// perPage is how many issues one request asks for.
const perPage = 20

// Issue is one open issue.
type Issue struct {
	Title     string
	URL       string
	Labels    []string
	CreatedAt time.Time
}

// severeLabels are the severity names that make an issue worth a warning.
//
// Observed on the live Arch packaging GitLab (2026-11-04): numbered scoped
// labels severity::2-high, severity::3-medium, severity::4-low,
// severity::5-lowest and priority::1-urgent, priority::2-high,
// priority::3-normal, priority::4-low. Still UNVERIFIED: severity::1-* was
// never seen (most likely severity::1-critical). The match stays loose: after
// the scope and a leading "<digits>-" are stripped, the name must be one of
// these words; bare ("high") and slash ("severity/high") forms count too.
var severeLabels = []string{"critical", "urgent", "high"}

// Severe reports whether the issue carries a critical, urgent or high
// severity or priority label.
func (i Issue) Severe() bool {
	for _, l := range i.Labels {
		l = strings.ToLower(strings.TrimSpace(l))
		if k := strings.LastIndexAny(l, ":/"); k >= 0 {
			if !strings.HasPrefix(l, "severity") && !strings.HasPrefix(l, "priority") {
				continue
			}
			l = l[k+1:]
		}
		// Numbered labels: "2-high" is "high".
		if k := strings.IndexByte(l, '-'); k > 0 && strings.Trim(l[:k], "0123456789") == "" {
			l = l[k+1:]
		}
		for _, s := range severeLabels {
			if l == s {
				return true
			}
		}
	}
	return false
}

type wireIssue struct {
	Title     string    `json:"title"`
	WebURL    string    `json:"web_url"`
	Labels    []string  `json:"labels"`
	CreatedAt time.Time `json:"created_at"`
}

// Parse decodes a project's issue list.
func Parse(data []byte) ([]Issue, error) {
	var ws []wireIssue
	if err := json.Unmarshal(data, &ws); err != nil {
		return nil, fmt.Errorf("decoding issues: %w", err)
	}
	issues := make([]Issue, 0, len(ws))
	for _, w := range ws {
		issues = append(issues, Issue{
			Title: clean(w.Title), URL: clean(w.WebURL), Labels: w.Labels, CreatedAt: w.CreatedAt,
		})
	}
	return issues, nil
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

// Client queries the GitLab API.
type Client struct {
	Fetcher system.Fetcher
	// APIBase defaults to the APIBase constant.
	APIBase string
}

// NewClient returns a Client using f.
func NewClient(f system.Fetcher) *Client {
	return &Client{Fetcher: f, APIBase: APIBase}
}

// IssuesURL is the request for the open issues of project created after
// since. project is the GitLab project name (see archweb.GitLabProject).
func (c *Client) IssuesURL(project string, since time.Time) string {
	base := c.APIBase
	if base == "" {
		base = APIBase
	}
	q := url.Values{
		"state":         {"opened"},
		"created_after": {since.UTC().Format(time.RFC3339)},
		"per_page":      {strconv.Itoa(perPage)},
	}
	return base + url.PathEscape(archweb.GitLabNamespace+"/"+project) + "/issues?" + q.Encode()
}

// Issues returns the open issues of project created after since. A project
// that doesn't exist (404) has no issues and isn't an error.
func (c *Client) Issues(ctx context.Context, project string, since time.Time, fresh bool) ([]Issue, error) {
	body, err := system.GetTTL(ctx, c.Fetcher, c.IssuesURL(project, since), TTL, fresh)
	var httpErr *system.HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode == 404 {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("gitlab issues for %s: %w", project, err)
	}
	issues, err := Parse(body)
	if err != nil {
		return nil, fmt.Errorf("gitlab issues for %s: %w", project, err)
	}
	return issues, nil
}

// Query is one project to look up.
type Query struct {
	// Key identifies the result; usually the package name.
	Key string
	// Project is the GitLab project name.
	Project string
	// Since is the new build's date: only issues opened after it count.
	Since time.Time
}

// Lookup queries every project, at most MaxConcurrent at a time, and returns
// the issues by Key. Queries that failed are left out and reported in the
// returned error, so one bad request doesn't hide the rest.
func (c *Client) Lookup(ctx context.Context, queries []Query, fresh bool) (map[string][]Issue, error) {
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		out  = map[string][]Issue{}
		errs []error
		sem  = make(chan struct{}, MaxConcurrent)
	)
	for _, q := range queries {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			issues, err := c.Issues(ctx, q.Project, q.Since, fresh)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			if len(issues) > 0 {
				out[q.Key] = issues
			}
		})
	}
	wg.Wait()
	return out, errors.Join(errs...)
}
