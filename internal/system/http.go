package system

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"
)

// Fetcher performs HTTP GET requests.
type Fetcher interface {
	// Get returns the body of a successful (2xx) response. Non-2xx
	// responses report an *HTTPError. If fresh is true, any cached copy is
	// ignored (but a successful response still refreshes the cache).
	Get(ctx context.Context, url string, fresh bool) ([]byte, error)
}

// HTTPError reports a non-2xx response.
type HTTPError struct {
	URL        string
	StatusCode int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("GET %s: %d %s", e.URL, e.StatusCode, http.StatusText(e.StatusCode))
}

const (
	// DefaultTimeout bounds every request made by HTTPClient.
	DefaultTimeout = 15 * time.Second
	// DefaultTTL is how long cached responses are served without refetching.
	DefaultTTL = 15 * time.Minute

	maxBodySize = 16 << 20
)

// HTTPClient is the real Fetcher. Successful responses are cached on disk
// under CacheDir for TTL; an empty CacheDir disables caching.
type HTTPClient struct {
	Client    *http.Client
	FS        FS
	CacheDir  string
	TTL       time.Duration
	UserAgent string
	// Now returns the current time. It defaults to time.Now and exists for
	// tests.
	Now func() time.Time
}

// NewHTTPClient returns an HTTPClient with the default timeout and TTL,
// caching under cacheDir.
func NewHTTPClient(cacheDir string) *HTTPClient {
	return &HTTPClient{
		Client:    &http.Client{Timeout: DefaultTimeout},
		FS:        OSFS{},
		CacheDir:  cacheDir,
		TTL:       DefaultTTL,
		UserAgent: "cronen",
		Now:       time.Now,
	}
}

type cacheEntry struct {
	URL     string    `json:"url"`
	Fetched time.Time `json:"fetched"`
	Body    []byte    `json:"body"`
}

// Get implements Fetcher.
func (c *HTTPClient) Get(ctx context.Context, url string, fresh bool) ([]byte, error) {
	if !fresh {
		if body, ok := c.cached(url); ok {
			return body, nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &HTTPError{URL: url, StatusCode: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	c.store(url, body)
	return body, nil
}

func (c *HTTPClient) now() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

func (c *HTTPClient) cachePath(url string) string {
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(c.CacheDir, "http", hex.EncodeToString(sum[:])+".json")
}

func (c *HTTPClient) cached(url string) ([]byte, bool) {
	if c.CacheDir == "" || c.FS == nil {
		return nil, false
	}
	data, err := c.FS.ReadFile(c.cachePath(url))
	if err != nil {
		return nil, false
	}
	var e cacheEntry
	if err := json.Unmarshal(data, &e); err != nil || e.URL != url {
		return nil, false
	}
	age := c.now().Sub(e.Fetched)
	if age < 0 || age >= c.TTL {
		return nil, false
	}
	return e.Body, true
}

// store writes a cache entry. Cache write failures are not fatal: the
// response is still returned to the caller.
func (c *HTTPClient) store(url string, body []byte) {
	if c.CacheDir == "" || c.FS == nil {
		return
	}
	data, err := json.Marshal(cacheEntry{URL: url, Fetched: c.now(), Body: body})
	if err != nil {
		return
	}
	_ = c.FS.WriteFile(c.cachePath(url), data, 0o644)
}
