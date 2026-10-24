package system

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExecRunner(t *testing.T) {
	ctx := context.Background()
	r := ExecRunner{}

	t.Run("success", func(t *testing.T) {
		out, code, err := r.Run(ctx, "sh", "-c", "echo hello")
		if err != nil || code != 0 || string(out) != "hello\n" {
			t.Fatalf("got %q, %d, %v", out, code, err)
		}
	})

	t.Run("non-zero exit", func(t *testing.T) {
		out, code, err := r.Run(ctx, "sh", "-c", "echo partial; echo boom >&2; exit 2")
		if code != 2 || string(out) != "partial\n" {
			t.Fatalf("got %q, %d", out, code)
		}
		var exitErr *ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("err = %v, want *ExitError", err)
		}
		if exitErr.Code != 2 || string(exitErr.Stderr) != "boom\n" {
			t.Fatalf("exitErr = %+v", exitErr)
		}
		if !strings.Contains(err.Error(), "boom") {
			t.Fatalf("error %q should include stderr", err)
		}
	})

	t.Run("missing command", func(t *testing.T) {
		_, code, err := r.Run(ctx, "cronen-definitely-not-a-command")
		if code != -1 || err == nil {
			t.Fatalf("got %d, %v", code, err)
		}
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("missing command should not be an *ExitError")
		}
		if !errors.Is(err, ErrCommandNotFound) {
			t.Fatalf("missing command: %v should match ErrCommandNotFound", err)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		_, code, err := r.Run(ctx, "sh", "-c", "exit 0")
		if code != -1 || !errors.Is(err, context.Canceled) {
			t.Fatalf("got %d, %v", code, err)
		}
	})
}

func TestFakeRunner(t *testing.T) {
	ctx := context.Background()
	f := NewFakeRunner()
	f.Set("checkupdates", FakeResult{Code: 2})
	f.Set("vercmp 1.0-1 2.0-1", FakeResult{Stdout: []byte("-1\n")})
	f.Set("broken", FakeResult{Err: errors.New("no such file")})

	out, code, err := f.Run(ctx, "vercmp", "1.0-1", "2.0-1")
	if err != nil || code != 0 || string(out) != "-1\n" {
		t.Fatalf("vercmp: got %q, %d, %v", out, code, err)
	}

	_, code, err = f.Run(ctx, "checkupdates")
	var exitErr *ExitError
	if code != 2 || !errors.As(err, &exitErr) {
		t.Fatalf("checkupdates: got %d, %v", code, err)
	}

	if _, code, err = f.Run(ctx, "broken"); code != -1 || err == nil {
		t.Fatalf("broken: got %d, %v", code, err)
	}

	if _, _, err = f.Run(ctx, "pacman", "-Syu"); !errors.Is(err, ErrUnexpectedCommand) {
		t.Fatalf("unexpected command: got %v", err)
	}

	want := []string{"vercmp 1.0-1 2.0-1", "checkupdates", "broken", "pacman -Syu"}
	if got := f.Calls(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

func testFS(t *testing.T, name string, fsys FS, root string) {
	t.Run(name, func(t *testing.T) {
		path := filepath.Join(root, "a", "b", "state.json")
		if _, err := fsys.ReadFile(path); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("missing file: got %v, want fs.ErrNotExist", err)
		}
		if err := fsys.WriteFile(path, []byte("one"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := fsys.WriteFile(path, []byte("two"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := fsys.ReadFile(path)
		if err != nil || string(got) != "two" {
			t.Fatalf("got %q, %v", got, err)
		}
		if err := fsys.MkdirAll(filepath.Join(root, "c"), 0o755); err != nil {
			t.Fatal(err)
		}
	})
}

func TestFS(t *testing.T) {
	testFS(t, "OSFS", OSFS{}, t.TempDir())
	testFS(t, "FakeFS", NewFakeFS(nil), "/tmp/x")

	f := NewFakeFS(map[string][]byte{"/var/log/pacman.log": []byte("log")})
	f.ReadErr = fs.ErrPermission
	if _, err := f.ReadFile("/var/log/pacman.log"); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("ReadErr: got %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fakeServer counts requests and serves status/body for every URL.
type fakeServer struct {
	mu     sync.Mutex
	hits   int
	status int
	body   string
	agent  string
}

func (s *fakeServer) client() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.hits++
		s.agent = r.Header.Get("User-Agent")
		return &http.Response{
			StatusCode: s.status,
			Body:       io.NopCloser(bytes.NewBufferString(s.body)),
			Header:     http.Header{},
			Request:    r,
		}, nil
	})}
}

func TestHTTPClientCache(t *testing.T) {
	ctx := context.Background()
	srv := &fakeServer{status: 200, body: "feed v1"}
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	c := &HTTPClient{
		Client:    srv.client(),
		FS:        NewFakeFS(nil),
		CacheDir:  "/cache/cronen",
		TTL:       DefaultTTL,
		UserAgent: "cronen-test",
		Now:       func() time.Time { return now },
	}
	const url = "https://archlinux.org/feeds/news/"

	get := func(fresh bool) string {
		t.Helper()
		b, err := c.Get(ctx, url, fresh)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	if got := get(false); got != "feed v1" || srv.hits != 1 {
		t.Fatalf("first get: %q, hits %d", got, srv.hits)
	}
	if srv.agent != "cronen-test" {
		t.Fatalf("User-Agent = %q", srv.agent)
	}

	srv.body = "feed v2"
	now = now.Add(DefaultTTL - time.Second)
	if got := get(false); got != "feed v1" || srv.hits != 1 {
		t.Fatalf("within TTL: %q, hits %d", got, srv.hits)
	}

	if got := get(true); got != "feed v2" || srv.hits != 2 {
		t.Fatalf("fresh: %q, hits %d", got, srv.hits)
	}

	srv.body = "feed v3"
	now = now.Add(DefaultTTL)
	if got := get(false); got != "feed v3" || srv.hits != 3 {
		t.Fatalf("expired: %q, hits %d", got, srv.hits)
	}
}

func TestHTTPClientErrors(t *testing.T) {
	ctx := context.Background()
	srv := &fakeServer{status: 503, body: "down"}
	fsys := NewFakeFS(nil)
	c := &HTTPClient{Client: srv.client(), FS: fsys, CacheDir: "/cache", TTL: DefaultTTL}

	_, err := c.Get(ctx, "https://aur.archlinux.org/rpc/v5/info", false)
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != 503 {
		t.Fatalf("got %v, want 503 HTTPError", err)
	}

	// Error responses must not be cached.
	srv.status, srv.body = 200, "ok"
	b, err := c.Get(ctx, "https://aur.archlinux.org/rpc/v5/info", false)
	if err != nil || string(b) != "ok" || srv.hits != 2 {
		t.Fatalf("got %q, %v, hits %d", b, err, srv.hits)
	}

	failing := &HTTPClient{Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network unreachable")
	})}}
	if _, err := failing.Get(ctx, "https://example.invalid/", false); err == nil {
		t.Fatal("expected transport error")
	}
}

func TestHTTPClientCorruptCache(t *testing.T) {
	ctx := context.Background()
	srv := &fakeServer{status: 200, body: "fresh"}
	fsys := NewFakeFS(nil)
	c := &HTTPClient{Client: srv.client(), FS: fsys, CacheDir: "/cache", TTL: DefaultTTL}
	const url = "https://archlinux.org/feeds/news/"
	if err := fsys.WriteFile(c.cachePath(url), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := c.Get(ctx, url, false)
	if err != nil || string(b) != "fresh" {
		t.Fatalf("got %q, %v", b, err)
	}
}

func TestFakeFetcher(t *testing.T) {
	ctx := context.Background()
	f := NewFakeFetcher()
	f.Set("https://a/", FakeResponse{Body: []byte("a")})
	f.Set("https://down/", FakeResponse{Status: 500})
	f.Set("https://offline/", FakeResponse{Err: errors.New("dial tcp: no route")})
	f.Handler = func(url string) (FakeResponse, bool) {
		if strings.HasPrefix(url, "https://h/") {
			return FakeResponse{Body: []byte("handled")}, true
		}
		return FakeResponse{}, false
	}

	if b, err := f.Get(ctx, "https://a/", false); err != nil || string(b) != "a" {
		t.Fatalf("a: %q, %v", b, err)
	}
	if b, err := f.Get(ctx, "https://h/x?y", true); err != nil || string(b) != "handled" {
		t.Fatalf("handler: %q, %v", b, err)
	}
	var httpErr *HTTPError
	if _, err := f.Get(ctx, "https://down/", false); !errors.As(err, &httpErr) {
		t.Fatalf("down: %v", err)
	}
	if _, err := f.Get(ctx, "https://offline/", false); err == nil {
		t.Fatal("offline: expected error")
	}
	if _, err := f.Get(ctx, "https://nope/", false); !errors.Is(err, ErrUnexpectedURL) {
		t.Fatalf("nope: %v", err)
	}
	if calls := f.Calls(); len(calls) != 5 || !calls[1].Fresh {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestInteractiveCommand(t *testing.T) {
	cmd := InteractiveCommand([]string{"sudo", "pacman", "-Syu"})
	if got := strings.Join(cmd.Args, " "); got != "sudo pacman -Syu" {
		t.Errorf("args = %q", got)
	}
	if cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil {
		t.Error("stdio must be left for Bubble Tea to hand over the terminal")
	}
}
