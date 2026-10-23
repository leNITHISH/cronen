package system

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrUnexpectedURL is returned by FakeFetcher for URLs it has no response
// for.
var ErrUnexpectedURL = errors.New("fake fetcher: unexpected URL")

// FakeResponse is a canned response for one URL. A Status of 0 means 200.
type FakeResponse struct {
	Body   []byte
	Status int
	// Err, if set, simulates a transport failure.
	Err error
}

// FakeFetcher is a Fetcher that returns canned responses. Exact URL matches
// in the response table win; otherwise Handler, if set, is consulted. It is
// safe for concurrent use.
type FakeFetcher struct {
	mu        sync.Mutex
	responses map[string]FakeResponse
	calls     []FakeCall

	// Handler answers URLs missing from the table. It reports ok=false for
	// URLs it doesn't know.
	Handler func(url string) (resp FakeResponse, ok bool)
}

// FakeCall records one Get call.
type FakeCall struct {
	URL   string
	Fresh bool
}

// NewFakeFetcher returns an empty FakeFetcher.
func NewFakeFetcher() *FakeFetcher {
	return &FakeFetcher{responses: map[string]FakeResponse{}}
}

// Set registers the response for an exact URL.
func (f *FakeFetcher) Set(url string, r FakeResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[url] = r
}

// Calls returns the Get calls made so far, in order.
func (f *FakeFetcher) Calls() []FakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeCall(nil), f.calls...)
}

// Get implements Fetcher.
func (f *FakeFetcher) Get(ctx context.Context, url string, fresh bool) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, FakeCall{URL: url, Fresh: fresh})
	r, ok := f.responses[url]
	handler := f.Handler
	f.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	if !ok && handler != nil {
		r, ok = handler(url)
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnexpectedURL, url)
	}
	if r.Err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, r.Err)
	}
	if r.Status != 0 && (r.Status < 200 || r.Status > 299) {
		return nil, &HTTPError{URL: url, StatusCode: r.Status}
	}
	return append([]byte(nil), r.Body...), nil
}
