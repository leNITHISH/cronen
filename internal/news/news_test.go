package news

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/system"
)

func parseFixture(t *testing.T) []Item {
	t.Helper()
	items, err := Parse(fixtures.Bytes(fixtures.News))
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestParse(t *testing.T) {
	items := parseFixture(t)
	want := []struct {
		title     string
		guid      string
		published time.Time
	}{
		{
			"linux-firmware >= 20251021-1 upgrade requires manual intervention",
			"tag:archlinux.org,2026-10-22:/news/linux-firmware-20251021-1-upgrade-requires-manual-intervention/",
			time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC),
		},
		{
			"NVIDIA 590 driver drops Pascal support",
			"tag:archlinux.org,2026-10-21:/news/nvidia-590-driver-drops-pascal-support/",
			time.Date(2026, 10, 21, 16, 30, 0, 0, time.UTC),
		},
		{
			"Python 3.14 is coming to [core]",
			"tag:archlinux.org,2026-09-15:/news/python-314-is-coming-to-core/",
			time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
		},
		{
			"Transition to the new WoW64 wine and wine-staging",
			"tag:archlinux.org,2025-06-24:/news/transition-to-the-new-wow64-wine-and-wine-staging/",
			time.Date(2025, 6, 24, 8, 0, 0, 0, time.UTC),
		},
	}
	if len(items) != len(want) {
		t.Fatalf("got %d items, want %d", len(items), len(want))
	}
	for i, w := range want {
		it := items[i]
		if it.Title != w.title || it.GUID != w.guid || !it.Published.Equal(w.published) {
			t.Errorf("item %d = %q %q %v, want %q %q %v", i, it.Title, it.GUID, it.Published, w.title, w.guid, w.published)
		}
		if !strings.HasPrefix(it.Link, "https://archlinux.org/news/") {
			t.Errorf("item %d link = %q", i, it.Link)
		}
	}
}

func TestParseBody(t *testing.T) {
	nvidia := parseFixture(t)[1]

	if !strings.Contains(nvidia.HTML, `<a href="https://wiki.archlinux.org/title/NVIDIA">`) {
		t.Errorf("HTML lost markup: %q", nvidia.HTML)
	}

	wantText := "The nvidia 590 driver series no longer supports Pascal (GTX 10xx) GPUs. " +
		"If you own one, switch to nvidia-580xx-dkms from the AUR before upgrading nvidia-utils. " +
		"Turing and newer cards are unaffected. See the wiki for details."
	if nvidia.Text != wantText {
		t.Errorf("Text =\n%q\nwant\n%q", nvidia.Text, wantText)
	}
	if strings.Contains(nvidia.Text, "wiki.archlinux.org") {
		t.Error("Text should not contain link targets")
	}

	for _, want := range []string{"`nvidia-utils`", "**before**", "[wiki](https://wiki.archlinux.org/title/NVIDIA)"} {
		if !strings.Contains(nvidia.Markdown, want) {
			t.Errorf("Markdown lacks %q:\n%s", want, nvidia.Markdown)
		}
	}

	firmware := parseFixture(t)[0]
	if !strings.Contains(firmware.Markdown, "```\npacman -Rdd linux-firmware\npacman -Syu linux-firmware\n```") {
		t.Errorf("code block not converted:\n%s", firmware.Markdown)
	}
}

func TestParseFallbacks(t *testing.T) {
	const feed = `<?xml version="1.0"?><rss version="2.0"><channel><title>t</title>
<item><title> Untagged </title><link>https://example.org/a</link><description>a &amp;amp; b</description></item>
</channel></rss>`
	items, err := Parse([]byte(feed))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items", len(items))
	}
	it := items[0]
	if it.GUID != "https://example.org/a" || it.Title != "Untagged" || !it.Published.IsZero() || it.Text != "a & b" {
		t.Errorf("got %+v", it)
	}
}

func TestParseErrors(t *testing.T) {
	for _, in := range []string{"", "not a feed", "<html><body>hi</body></html>", "<rss><channel><item>"} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("Parse(%q) should fail", in)
		}
	}
}

func TestFetch(t *testing.T) {
	if FeedURL != fixtures.NewsURL {
		t.Fatalf("FeedURL = %q, fixtures serve %q", FeedURL, fixtures.NewsURL)
	}
	ctx := context.Background()

	f := fixtures.Fetcher()
	items, err := Fetch(ctx, f, false)
	if err != nil || len(items) != 4 {
		t.Fatalf("got %d items, %v", len(items), err)
	}
	if _, err := Fetch(ctx, f, true); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 2 || calls[0].Fresh || !calls[1].Fresh || calls[0].URL != FeedURL {
		t.Errorf("calls = %+v", calls)
	}

	down := system.NewFakeFetcher()
	down.Set(FeedURL, system.FakeResponse{Status: 503})
	var httpErr *system.HTTPError
	if _, err := Fetch(ctx, down, false); !errors.As(err, &httpErr) || httpErr.StatusCode != 503 {
		t.Errorf("503: got %v", err)
	}

	offline := system.NewFakeFetcher()
	netErr := errors.New("dial tcp: no route to host")
	offline.Set(FeedURL, system.FakeResponse{Err: netErr})
	if _, err := Fetch(ctx, offline, false); !errors.Is(err, netErr) {
		t.Errorf("offline: got %v", err)
	}

	garbage := system.NewFakeFetcher()
	garbage.Set(FeedURL, system.FakeResponse{Body: []byte("<html>captive portal</html>")})
	if _, err := Fetch(ctx, garbage, false); err == nil {
		t.Error("garbage: expected error")
	}
}

func TestUnread(t *testing.T) {
	items := parseFixture(t)
	const (
		firmware = "tag:archlinux.org,2026-10-22:/news/linux-firmware-20251021-1-upgrade-requires-manual-intervention/"
		nvidia   = "tag:archlinux.org,2026-10-21:/news/nvidia-590-driver-drops-pascal-support/"
		python   = "tag:archlinux.org,2026-09-15:/news/python-314-is-coming-to-core/"
		wine     = "tag:archlinux.org,2025-06-24:/news/transition-to-the-new-wow64-wine-and-wine-staging/"
	)
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		state ReadState
		want  []string
	}{
		{
			name:  "after last upgrade",
			state: ReadState{LastUpgrade: time.Date(2026, 10, 20, 13, 32, 11, 0, time.UTC), Now: now},
			want:  []string{firmware, nvidia},
		},
		{
			name:  "upgrade at exact publish time counts as read",
			state: ReadState{LastUpgrade: time.Date(2026, 10, 21, 16, 30, 0, 0, time.UTC), Now: now},
			want:  []string{firmware},
		},
		{
			name:  "marked read",
			state: ReadState{LastUpgrade: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), Read: map[string]bool{firmware: true}, Now: now},
			want:  []string{nvidia},
		},
		{
			name:  "old upgrade ignores the unknown window",
			state: ReadState{LastUpgrade: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Read: map[string]bool{python: true}, Now: now},
			want:  []string{firmware, nvidia, wine},
		},
		{
			name:  "unknown last upgrade: last 30 days",
			state: ReadState{Now: now},
			want:  []string{firmware, nvidia},
		},
		{
			name:  "unknown last upgrade: custom window",
			state: ReadState{Now: now, UnknownWindow: 60 * 24 * time.Hour, Read: map[string]bool{nvidia: true}},
			want:  []string{firmware, python},
		},
		{
			name:  "everything read",
			state: ReadState{Now: now, Read: map[string]bool{firmware: true, nvidia: true}},
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, it := range tt.state.Unread(items) {
				got = append(got, it.GUID)
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("unread =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

func TestRecent(t *testing.T) {
	items := parseFixture(t)
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	if got := Recent(items, now, 30); len(got) != 2 {
		t.Errorf("30 days: %d items", len(got))
	}
	if got := Recent(items, now, 60); len(got) != 3 {
		t.Errorf("60 days: %d items", len(got))
	}
	if got := Recent(items, now, 0); len(got) != 4 {
		t.Errorf("0 days: %d items", len(got))
	}
}
