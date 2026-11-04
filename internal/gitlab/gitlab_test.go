package gitlab

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/system"
)

// The JSON below is HAND-WRITTEN from GitLab's documented issues API, and
// is pending capture from the real Arch GitLab.
const issuesJSON = `[
 {"id":1,"iid":7,"title":"Crash on startup with Wayland","web_url":"https://gitlab.archlinux.org/archlinux/packaging/packages/linux/-/issues/7",
  "labels":["severity::high"],"created_at":"2026-10-22T08:00:00.000Z","state":"opened"},
 {"id":2,"iid":8,"title":"Docs typo\u0007","web_url":"https://gitlab.archlinux.org/archlinux/packaging/packages/linux/-/issues/8",
  "labels":[],"created_at":"2026-10-22T09:00:00.000Z","state":"opened"}
]`

var since = time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

func TestIssuesURL(t *testing.T) {
	c := NewClient(nil)
	want := "https://gitlab.archlinux.org/api/v4/projects/archlinux%2Fpackaging%2Fpackages%2Flibsigcplusplus/issues" +
		"?created_after=2026-10-21T10%3A00%3A00Z&per_page=20&state=opened"
	if got := c.IssuesURL("libsigcplusplus", since.In(time.FixedZone("x", 3600))); got != want {
		t.Errorf("url =\n%s\nwant\n%s", got, want)
	}
}

func TestIssues(t *testing.T) {
	ctx := context.Background()
	f := system.NewFakeFetcher()
	c := NewClient(f)
	f.Set(c.IssuesURL("linux", since), system.FakeResponse{Body: []byte(issuesJSON)})
	f.Set(c.IssuesURL("gone", since), system.FakeResponse{Status: 404})
	f.Set(c.IssuesURL("down", since), system.FakeResponse{Status: 500})

	issues, err := c.Issues(ctx, "linux", since, false)
	if err != nil || len(issues) != 2 {
		t.Fatalf("%v, %v", issues, err)
	}
	if issues[1].Title != "Docs typo" || !issues[0].Severe() || issues[1].Severe() {
		t.Errorf("issues = %+v", issues)
	}

	// A 404 means "no project", not an error.
	if issues, err := c.Issues(ctx, "gone", since, false); err != nil || issues != nil {
		t.Errorf("404: %v, %v", issues, err)
	}
	if _, err := c.Issues(ctx, "down", since, false); err == nil {
		t.Error("a 500 must be an error")
	}
}

func TestSevere(t *testing.T) {
	for _, tt := range []struct {
		labels []string
		want   bool
	}{
		{nil, false},
		{[]string{"bug"}, false},
		{[]string{"high"}, true},
		{[]string{"Critical"}, true},
		{[]string{"severity::high"}, true},
		{[]string{"priority::critical"}, true},
		{[]string{"severity::low"}, false},
		{[]string{"bug", "severity/critical"}, true},
		{[]string{"area::high-level"}, false},
		{[]string{"area::high"}, false},
	} {
		if got := (Issue{Labels: tt.labels}).Severe(); got != tt.want {
			t.Errorf("Severe(%v) = %v, want %v", tt.labels, got, tt.want)
		}
	}
}

func TestLookup(t *testing.T) {
	var inFlight, peak atomic.Int32
	var mu sync.Mutex
	hit := map[string]int{}
	f := system.NewFakeFetcher()
	c := NewClient(f)
	f.Handler = func(u string) (system.FakeResponse, bool) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		hit[u]++
		mu.Unlock()
		switch {
		case strings.Contains(u, "%2Fp0/"):
			return system.FakeResponse{Body: []byte(issuesJSON)}, true
		case strings.Contains(u, "%2Fbad/"):
			return system.FakeResponse{Status: 500}, true
		case strings.Contains(u, "%2Fp1/"):
			return system.FakeResponse{Status: 404}, true
		}
		return system.FakeResponse{Body: []byte("[]")}, true
	}

	var qs []Query
	for _, p := range []string{"p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "bad"} {
		qs = append(qs, Query{Key: p, Project: p, Since: since})
	}
	got, err := c.Lookup(context.Background(), qs, false)
	if err == nil || !strings.Contains(err.Error(), "bad") {
		t.Errorf("err = %v, want the failing project named", err)
	}
	if len(got) != 1 || len(got["p0"]) != 2 {
		t.Errorf("results = %v", got)
	}
	if peak.Load() > MaxConcurrent {
		t.Errorf("%d requests in flight, want at most %d", peak.Load(), MaxConcurrent)
	}
	if len(hit) != len(qs) {
		t.Errorf("%d distinct requests, want %d", len(hit), len(qs))
	}
}
