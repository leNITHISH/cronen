package aur

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/system"
)

func installedFixture(t *testing.T) []pacman.Package {
	t.Helper()
	pkgs, err := pacman.ParseQm(fixtures.Bytes(fixtures.PacmanQm))
	if err != nil {
		t.Fatal(err)
	}
	return pkgs
}

func TestCheckFixture(t *testing.T) {
	ctx := context.Background()
	runner := fixtures.Runner()
	fetcher := fixtures.Fetcher()
	c := NewClient(fetcher, runner)
	all, err := pacman.InstalledNames(ctx, runner)
	if err != nil {
		t.Fatal(err)
	}
	c.Installed = all

	installed := installedFixture(t)
	got, err := c.Check(ctx, installed, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(installed) {
		t.Fatalf("got %d packages, want %d", len(got), len(installed))
	}

	type status struct {
		kind                                  Kind
		pending, missing, orphaned, outOfDate bool
	}
	want := map[string]status{
		"gnome-shell-extension-legacy": {missing: true},
		"neovim-git":                   {kind: KindVCS},
		"paru":                         {pending: true},
		"paru-debug":                   {kind: KindDebug},
		"python-pywal":                 {orphaned: true},
		"spotify":                      {},
		"ttf-ms-fonts":                 {outOfDate: true},
		"visual-studio-code-bin":       {pending: true},
		// zstd is a repo package: only pacman -Qq knows it is installed.
		"zstd-debug": {kind: KindDebug},
	}
	for i, p := range got {
		if p.Name != installed[i].Name || p.InstalledVersion != installed[i].Version {
			t.Errorf("package %d = %s %s, want %s %s (input order)", i, p.Name, p.InstalledVersion, installed[i].Name, installed[i].Version)
		}
		s := status{kind: p.Kind, pending: p.Pending, missing: p.Missing()}
		if p.Info != nil {
			s.orphaned = p.Info.Orphaned()
			s.outOfDate = p.Info.FlaggedOutOfDate()
		}
		if s != want[p.Name] {
			t.Errorf("%s: got %+v, want %+v", p.Name, s, want[p.Name])
		}
	}

	byName := map[string]Package{}
	for _, p := range got {
		byName[p.Name] = p
	}
	paru := byName["paru"].Info
	if paru.Version != "2.1.0-2" || paru.URL != "https://github.com/morganamilo/paru" || paru.Maintainer != "Morganamilo" {
		t.Errorf("paru info = %+v", paru)
	}
	if paru.ChangelogURL() != "https://aur.archlinux.org/cgit/aur.git/log/?h=paru" {
		t.Errorf("paru changelog = %s", paru.ChangelogURL())
	}
	if want := time.Unix(1755000000, 0).UTC(); !byName["ttf-ms-fonts"].Info.OutOfDate.Equal(want) {
		t.Errorf("ttf-ms-fonts OutOfDate = %v, want %v", byName["ttf-ms-fonts"].Info.OutOfDate, want)
	}

	if vcs := byName["neovim-git"]; vcs.Info == nil || vcs.Info.URL != "https://github.com/neovim/neovim" {
		t.Errorf("neovim-git should still carry its AUR info, got %+v", vcs.Info)
	}

	for _, call := range runner.Calls() {
		if strings.Contains(call, "gnome-shell-extension-legacy") || strings.Contains(call, "3.2.0-1") {
			t.Errorf("missing package should not be compared: %q", call)
		}
		// The AUR has a newer neovim-git snapshot, but VCS packages are
		// never compared against it.
		if strings.HasPrefix(call, "vercmp") && strings.Contains(call, "0.12.0.r") {
			t.Errorf("VCS package should not be compared: %q", call)
		}
	}
	for _, call := range fetcher.Calls() {
		if strings.Contains(call.URL, "-debug") {
			t.Errorf("debug split looked up in the AUR: %s", call.URL)
		}
	}
}

func TestClassify(t *testing.T) {
	installed := func(names ...string) func(string) bool {
		return func(n string) bool {
			for _, x := range names {
				if x == n {
					return true
				}
			}
			return false
		}
	}
	tests := []struct {
		name      string
		installed []string
		want      Kind
	}{
		{"paru-debug", []string{"paru"}, KindDebug},
		{"zstd-debug", []string{"zstd"}, KindDebug},
		{"foo-git-debug", []string{"foo-git"}, KindDebug},
		// Without its base installed, -debug is just part of the name.
		{"lua-debug", nil, KindRegular},
		{"-debug", []string{""}, KindRegular},
		{"neovim-git", nil, KindVCS},
		{"foo-svn", nil, KindVCS},
		{"foo-hg", nil, KindVCS},
		{"foo-bzr", nil, KindVCS},
		{"foo-fossil", nil, KindVCS},
		{"foo-darcs", nil, KindVCS},
		{"foo-cvs", nil, KindVCS},
		{"-git", nil, KindRegular},
		{"legit", nil, KindRegular},
		{"git-extras", nil, KindRegular},
		{"visual-studio-code-bin", nil, KindRegular},
	}
	for _, tt := range tests {
		if got := Classify(tt.name, installed(tt.installed...)); got != tt.want {
			t.Errorf("Classify(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// A -debug package whose base isn't installed is looked up like any other,
// and reported missing if the AUR doesn't have it.
func TestCheckDebugWithoutBase(t *testing.T) {
	ctx := context.Background()
	f := system.NewFakeFetcher()
	f.Handler = func(string) (system.FakeResponse, bool) {
		return system.FakeResponse{Body: []byte(`{"type":"multiinfo","version":5,"resultcount":0,"results":[]}`)}, true
	}
	got, err := NewClient(f, system.NewFakeRunner()).Check(ctx, []pacman.Package{{Name: "orphan-debug", Version: "1-1"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != KindRegular || !got[0].Missing() {
		t.Fatalf("got %+v, want a regular, missing package", got)
	}
	if len(f.Calls()) != 1 {
		t.Errorf("want one AUR lookup, got %d", len(f.Calls()))
	}
}

func TestCheckNewerThanAUR(t *testing.T) {
	ctx := context.Background()
	f := system.NewFakeFetcher()
	f.Handler = func(string) (system.FakeResponse, bool) {
		return system.FakeResponse{Body: []byte(`{"type":"multiinfo","version":5,"resultcount":1,"results":[
			{"Name":"foo-git","PackageBase":"foo-git","Version":"1.0.r9.gdef-1","Maintainer":"me","OutOfDate":null}]}`)}, true
	}
	r := system.NewFakeRunner()
	r.Set("vercmp 1.0.r10.gabc-1 1.0.r9.gdef-1", system.FakeResult{Stdout: []byte("1\n")})

	got, err := NewClient(f, r).Check(ctx, []pacman.Package{{Name: "foo-git", Version: "1.0.r10.gabc-1"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Pending || got[0].Missing() {
		t.Fatalf("got %+v, want present and not pending", got)
	}
}

func TestCheckVercmpError(t *testing.T) {
	ctx := context.Background()
	r := system.NewFakeRunner() // knows no vercmp pairs
	_, err := NewClient(fixtures.Fetcher(), r).Check(ctx, installedFixture(t), false)
	if !errors.Is(err, system.ErrUnexpectedCommand) {
		t.Fatalf("err = %v, want the vercmp failure", err)
	}
}

func TestCheckNoInstalled(t *testing.T) {
	f := system.NewFakeFetcher()
	got, err := NewClient(f, system.NewFakeRunner()).Check(context.Background(), nil, false)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("made %d requests, want 0", n)
	}
}

// countingFetcher answers every info request with an empty result set and
// records the names in each request.
func countingFetcher(t *testing.T) (*system.FakeFetcher, *[][]string) {
	t.Helper()
	var batches [][]string
	f := system.NewFakeFetcher()
	f.Handler = func(raw string) (system.FakeResponse, bool) {
		u, err := url.Parse(raw)
		if err != nil {
			t.Errorf("bad URL %q: %v", raw, err)
			return system.FakeResponse{}, false
		}
		if base := u.Scheme + "://" + u.Host + u.Path; base != DefaultInfoURL {
			t.Errorf("request to %s, want %s", base, DefaultInfoURL)
		}
		batches = append(batches, u.Query()["arg[]"])
		return system.FakeResponse{Body: []byte(`{"type":"multiinfo","version":5,"resultcount":0,"results":[]}`)}, true
	}
	return f, &batches
}

func names(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("pkg%03d", i)
	}
	return out
}

func TestInfoBatching(t *testing.T) {
	tests := []struct {
		name  string
		in    []string
		sizes []int
	}{
		{name: "none", in: nil},
		{name: "one", in: names(1), sizes: []int{1}},
		{name: "exactly one batch", in: names(150), sizes: []int{150}},
		{name: "one over", in: names(151), sizes: []int{150, 1}},
		{name: "several", in: names(400), sizes: []int{150, 150, 100}},
		{name: "duplicates", in: append(names(150), names(150)...), sizes: []int{150}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, batches := countingFetcher(t)
			if _, err := NewClient(f, nil).Info(context.Background(), tt.in, false); err != nil {
				t.Fatal(err)
			}
			var sizes []int
			seen := map[string]bool{}
			for _, b := range *batches {
				sizes = append(sizes, len(b))
				for _, n := range b {
					if seen[n] {
						t.Errorf("%s requested twice", n)
					}
					seen[n] = true
				}
			}
			if fmt.Sprint(sizes) != fmt.Sprint(tt.sizes) {
				t.Fatalf("batch sizes = %v, want %v", sizes, tt.sizes)
			}
			for _, n := range tt.in {
				if !seen[n] {
					t.Errorf("%s never requested", n)
				}
			}
		})
	}
}

func TestInfoCustomBatchSize(t *testing.T) {
	f, batches := countingFetcher(t)
	c := &Client{Fetcher: f, BatchSize: 2}
	if _, err := c.Info(context.Background(), names(5), false); err != nil {
		t.Fatal(err)
	}
	if len(*batches) != 3 {
		t.Fatalf("got %d batches, want 3", len(*batches))
	}
}

func TestInfoEscapesNames(t *testing.T) {
	f, batches := countingFetcher(t)
	in := []string{"gtk+", "foo&bar", "a b"}
	if _, err := NewClient(f, nil).Info(context.Background(), in, false); err != nil {
		t.Fatal(err)
	}
	if got := (*batches)[0]; fmt.Sprint(got) != fmt.Sprint(in) {
		t.Fatalf("server saw %q, want %q", got, in)
	}
}

func TestInfoFresh(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		f := fixtures.Fetcher()
		if _, err := NewClient(f, nil).Info(context.Background(), []string{"paru"}, fresh); err != nil {
			t.Fatal(err)
		}
		calls := f.Calls()
		if len(calls) != 1 || calls[0].Fresh != fresh {
			t.Fatalf("fresh=%v: calls = %+v", fresh, calls)
		}
	}
}

func TestInfoIgnoresUnrequestedResults(t *testing.T) {
	f := system.NewFakeFetcher()
	f.Handler = func(string) (system.FakeResponse, bool) {
		return system.FakeResponse{Body: []byte(`{"type":"multiinfo","version":5,"resultcount":2,"results":[
			{"Name":"asked","Version":"1-1","Maintainer":"x"},{"Name":"other","Version":"1-1","Maintainer":"x"}]}`)}, true
	}
	got, err := NewClient(f, nil).Info(context.Background(), []string{"asked"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["other"]; ok || len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got["asked"].PackageBase != "" || got["asked"].ChangelogURL() != "https://aur.archlinux.org/cgit/aur.git/log/?h=asked" {
		t.Errorf("changelog without pkgbase = %s", got["asked"].ChangelogURL())
	}
}

func TestInfoErrors(t *testing.T) {
	tests := []struct {
		name string
		resp system.FakeResponse
		want string
	}{
		{
			name: "rpc error",
			resp: system.FakeResponse{Body: []byte(`{"error":"Too many package arguments.","resultcount":0,"results":[],"type":"error","version":5}`)},
			want: "Too many package arguments.",
		},
		{name: "not json", resp: system.FakeResponse{Body: []byte("<html>oops</html>")}, want: "decoding"},
		{name: "http error", resp: system.FakeResponse{Status: 503}, want: "503"},
		{name: "transport error", resp: system.FakeResponse{Err: errors.New("no route to host")}, want: "no route to host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := system.NewFakeFetcher()
			f.Handler = func(string) (system.FakeResponse, bool) { return tt.resp, true }
			_, err := NewClient(f, fixtures.Runner()).Check(context.Background(), installedFixture(t), false)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}

	t.Run("http error is typed", func(t *testing.T) {
		f := system.NewFakeFetcher()
		f.Handler = func(string) (system.FakeResponse, bool) { return system.FakeResponse{Status: 429}, true }
		_, err := NewClient(f, nil).Info(context.Background(), []string{"paru"}, false)
		var httpErr *system.HTTPError
		if !errors.As(err, &httpErr) || httpErr.StatusCode != 429 {
			t.Fatalf("err = %v, want 429 *HTTPError", err)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := NewClient(fixtures.Fetcher(), fixtures.Runner()).Check(ctx, installedFixture(t), false)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	})

	t.Run("later batch fails", func(t *testing.T) {
		calls := 0
		f := system.NewFakeFetcher()
		f.Handler = func(string) (system.FakeResponse, bool) {
			calls++
			if calls == 2 {
				return system.FakeResponse{Status: 500}, true
			}
			return system.FakeResponse{Body: []byte(`{"type":"multiinfo","results":[]}`)}, true
		}
		if _, err := NewClient(f, nil).Info(context.Background(), names(200), false); err == nil {
			t.Fatal("expected an error")
		}
	})
}
