package archweb

import (
	"context"
	"errors"
	"testing"

	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/system"
)

func TestLookupFixtures(t *testing.T) {
	c := NewClient(fixtures.Fetcher())
	tests := []struct {
		name string
		want Package
	}{
		{
			name: "linux-headers",
			want: Package{
				Name: "linux-headers", Base: "linux", Repo: "core", Arch: "x86_64",
				Version: "6.18.1.arch1-1", URL: "https://github.com/archlinux/linux",
			},
		},
		{
			name: "nvidia-utils",
			want: Package{
				Name: "nvidia-utils", Base: "nvidia-utils", Repo: "extra", Arch: "x86_64",
				Version: "590.44.01-1", URL: "https://www.nvidia.com/",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.Lookup(context.Background(), tt.name, false)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}

	if _, err := c.Lookup(context.Background(), "not-a-package", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing package: got %v", err)
	}
}

func TestLookupRequest(t *testing.T) {
	f := fixtures.Fetcher()
	c := NewClient(f)
	if _, err := c.Lookup(context.Background(), "python", true); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 1 || calls[0].URL != SearchURL+"?name=python" || !calls[0].Fresh {
		t.Fatalf("calls = %+v", calls)
	}

	// Names are query-escaped.
	f = system.NewFakeFetcher()
	f.Set(SearchURL+"?name=libc%2B%2B", system.FakeResponse{Body: []byte(`{"valid":true,"results":[]}`)})
	if _, err := NewClient(f).Lookup(context.Background(), "libc++", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("libc++: got %v", err)
	}
}

func TestLookupSelection(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    Package
		wantErr error
	}{
		{
			name: "stable repo preferred over testing and staging",
			body: `{"valid":true,"results":[
				{"pkgname":"glibc","pkgbase":"glibc","repo":"core-staging","arch":"x86_64","pkgver":"2.43","pkgrel":"1","epoch":0},
				{"pkgname":"glibc","pkgbase":"glibc","repo":"core-testing","arch":"x86_64","pkgver":"2.42","pkgrel":"5","epoch":0},
				{"pkgname":"glibc","pkgbase":"glibc","repo":"core","arch":"x86_64","pkgver":"2.42","pkgrel":"3","epoch":0,"url":"https://www.gnu.org/software/libc"}
			]}`,
			want: Package{Name: "glibc", Base: "glibc", Repo: "core", Arch: "x86_64", Version: "2.42-3", URL: "https://www.gnu.org/software/libc"},
		},
		{
			name: "testing preferred over staging",
			body: `{"valid":true,"results":[
				{"pkgname":"glibc","pkgbase":"glibc","repo":"core-staging","arch":"x86_64","pkgver":"2.43","pkgrel":"1"},
				{"pkgname":"glibc","pkgbase":"glibc","repo":"core-testing","arch":"x86_64","pkgver":"2.42","pkgrel":"5"}
			]}`,
			want: Package{Name: "glibc", Base: "glibc", Repo: "core-testing", Arch: "x86_64", Version: "2.42-5"},
		},
		{
			name: "only exact name matches",
			body: `{"valid":true,"results":[
				{"pkgname":"glibc-locales","pkgbase":"glibc","repo":"core","arch":"x86_64","pkgver":"2.42","pkgrel":"3"}
			]}`,
			wantErr: ErrNotFound,
		},
		{
			name: "epoch",
			body: `{"valid":true,"results":[
				{"pkgname":"glibc","pkgbase":"glibc","repo":"core","arch":"x86_64","pkgver":"25.2.4","pkgrel":"1","epoch":1}
			]}`,
			want: Package{Name: "glibc", Base: "glibc", Repo: "core", Arch: "x86_64", Version: "1:25.2.4-1"},
		},
		{
			name: "missing pkgbase falls back to name",
			body: `{"valid":true,"results":[{"pkgname":"glibc","repo":"core","arch":"x86_64","pkgver":"2.42","pkgrel":"3"}]}`,
			want: Package{Name: "glibc", Base: "glibc", Repo: "core", Arch: "x86_64", Version: "2.42-3"},
		},
		{name: "no results", body: `{"valid":true,"results":[]}`, wantErr: ErrNotFound},
		{name: "null results", body: `{"valid":true,"results":null}`, wantErr: ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := system.NewFakeFetcher()
			f.Set(SearchURL+"?name=glibc", system.FakeResponse{Body: []byte(tt.body)})
			got, err := NewClient(f).Lookup(context.Background(), "glibc", false)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLookupErrors(t *testing.T) {
	tests := []struct {
		name string
		resp system.FakeResponse
	}{
		{name: "http error", resp: system.FakeResponse{Status: 503}},
		{name: "transport error", resp: system.FakeResponse{Err: errors.New("dial tcp: no route to host")}},
		{name: "malformed json", resp: system.FakeResponse{Body: []byte(`{"valid":true,"results":[`)}},
		{name: "html instead of json", resp: system.FakeResponse{Body: []byte(`<html>maintenance</html>`)}},
		{name: "invalid query", resp: system.FakeResponse{Body: []byte(`{"valid":false,"results":[]}`)}},
		{name: "empty body", resp: system.FakeResponse{Body: nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := system.NewFakeFetcher()
			f.Set(SearchURL+"?name=glibc", tt.resp)
			_, err := NewClient(f).Lookup(context.Background(), "glibc", false)
			if err == nil {
				t.Fatal("expected an error")
			}
			if errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v, should not be ErrNotFound", err)
			}
		})
	}

	var httpErr *system.HTTPError
	f := system.NewFakeFetcher()
	f.Set(SearchURL+"?name=glibc", system.FakeResponse{Status: 503})
	if _, err := NewClient(f).Lookup(context.Background(), "glibc", false); !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want it to wrap *system.HTTPError", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewClient(fixtures.Fetcher()).Lookup(ctx, "linux", false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: got %v", err)
	}
}

func TestZeroClientUsesDefaultURL(t *testing.T) {
	c := &Client{Fetcher: fixtures.Fetcher()}
	if got, err := c.Lookup(context.Background(), "python", false); err != nil || got.Version != "3.14.0-1" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestChangelogURL(t *testing.T) {
	p := Package{Name: "linux-headers", Base: "linux"}
	if got, want := p.ChangelogURL(), "https://gitlab.archlinux.org/archlinux/packaging/packages/linux/-/commits/main"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
