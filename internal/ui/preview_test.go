package ui

import (
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/system"
)

// selectEntry moves the selection to the entry with the given key.
func selectEntry(t *testing.T, m Model, key string) Model {
	t.Helper()
	for i, e := range m.updates.entries {
		if e.key != key {
			continue
		}
		for m.updates.sel < i {
			m = send(t, m, press("j"))
		}
		for m.updates.sel > i {
			m = send(t, m, press("k"))
		}
		return m
	}
	t.Fatalf("no entry %q in %d entries", key, len(m.updates.entries))
	return m
}

const sup = "pacman -Sup --dbpath /tmp/checkup-db-1000 --logfile /dev/null --noconfirm --print-format %n %v %r %s"

func TestPreviewGroups(t *testing.T) {
	const w, h = 100, 48
	m := loaded(t, fixtureOptions(), w, h)

	// The groups follow the AUR, each with its own count.
	var sections []string
	for _, r := range m.updates.rows {
		if r.kind == rowSection {
			sections = append(sections, r.text)
		}
	}
	want := []string{"NEEDS ATTENTION (12)", "REPO (4)", "AUR (5 + 1 VCS)", "NEW (2)", "REMOVED (1)", "HELD (1)"}
	if strings.Join(sections, "|") != strings.Join(want, "|") {
		t.Errorf("sections = %q, want %q", sections, want)
	}
	// New, removed and held packages aren't updates.
	if got := len(m.result.Packages); got != 18 {
		t.Errorf("%d updates, want 18", got)
	}
	if got := len(m.result.Flagged()); got != 12 {
		t.Errorf("%d flagged, want 12: a held package must not count", got)
	}
	golden(t, "preview-groups", plain(m))
	checkSize(t, m, w, h)

	for _, tt := range []struct{ name, key, want string }{
		{"preview-new", "new:egl-wayland2", "needed by nvidia-utils"},
		{"preview-replacement", "new:iptables-nft", "replaces iptables"},
		{"preview-removed", "removed:iptables", "replaced by iptables-nft"},
		{"preview-held", "held:bash", "Held by IgnorePkg: 5.3.3-1 → 5.3.3-2"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := selectEntry(t, m, tt.key)
			out := plain(s)
			if !strings.Contains(out, tt.want) {
				t.Errorf("detail lacks %q:\n%s", tt.want, out)
			}
			golden(t, tt.name, out)
			checkSize(t, s, w, h)
		})
	}
}

func TestPreviewStatusTotals(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 30)
	if !strings.Contains(plain(m), " 18 updates · 886 MiB ↓ · +26 MiB · 12 flagged") {
		t.Errorf("status bar lacks the totals:\n%s", plain(m))
	}

	// What the rows say adds up to what the status bar says.
	var dl, delta int64
	for _, u := range m.tx.Updates {
		dl += u.DownloadSize
		delta += u.NewSize - u.OldSize
	}
	for _, n := range m.tx.New {
		dl += n.DownloadSize
		delta += n.Size
	}
	for _, r := range m.tx.Removed {
		delta -= r.Size
	}
	if dl != m.tx.DownloadBytes || delta != m.tx.SizeDelta {
		t.Errorf("rows add up to %d and %d, totals are %d and %d", dl, delta, m.tx.DownloadBytes, m.tx.SizeDelta)
	}
}

func TestPreviewDetailForUpdate(t *testing.T) {
	m := selectEntry(t, loaded(t, fixtureOptions(), 100, 30), "repo:zstd")
	out := plain(m)
	for _, want := range []string{"download: already in the cache", "installed size: 1.7 MiB → 1.7 MiB (+0 B)", "built 21 days ago"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail lacks %q:\n%s", want, out)
		}
	}
}

func TestPreviewFailure(t *testing.T) {
	opts := fixtureOptions()
	r := fixtures.Runner()
	r.Set(sup, system.FakeResult{Code: 1, Stderr: []byte("error: failed to prepare transaction (could not satisfy dependencies)")})
	opts.Runner = r
	m := loaded(t, opts, 120, 48)

	// The rest of the UI is untouched.
	if len(m.result.Packages) != 18 || m.repoSec.err != nil {
		t.Errorf("%d updates, repo err %v", len(m.result.Packages), m.repoSec.err)
	}
	out := plain(m)
	for _, want := range []string{"TRANSACTION", "could not satisfy dependencies", "preview unavailable"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "NEW (") || strings.Contains(out, "↓") {
		t.Errorf("a failed preview shows groups or totals:\n%s", out)
	}
	golden(t, "preview-error", plain(loaded(t, opts, 100, 48)))
	checkSize(t, m, 120, 48)
}

func TestPreviewMissingDatabase(t *testing.T) {
	opts := fixtureOptions()
	opts.FS = system.NewFakeFS(nil)
	m := loaded(t, opts, 140, 48)
	if !strings.Contains(plain(m), "transaction preview unavailable") {
		t.Errorf("no unavailable note:\n%s", plain(m))
	}
}

func TestPreviewDisabled(t *testing.T) {
	opts := fixtureOptions()
	cfg := config.Default()
	cfg.Preview = false
	opts.Config = &cfg
	r := fixtures.Runner()
	opts.Runner = r
	m := loaded(t, opts, 100, 48)
	out := plain(m)
	for _, bad := range []string{"NEW (", "HELD (", "TRANSACTION", "↓"} {
		if strings.Contains(out, bad) {
			t.Errorf("view contains %q with the preview off:\n%s", bad, out)
		}
	}
	for _, c := range r.Calls() {
		if strings.Contains(c, "-Sup") || strings.Contains(c, "-Si") {
			t.Errorf("ran %q", c)
		}
	}
	if m.anyLoading() {
		t.Error("still loading with the preview off")
	}
}

func TestPreviewLoading(t *testing.T) {
	m := sized(t, fixtureOptions(), 100, 30)
	next, _ := m.Update(repoMsg{gen: m.gen, updates: nil})
	m = next.(Model)
	if !m.previewSec.loading || !m.anyLoading() {
		t.Fatal("preview isn't loading after the repo updates arrived")
	}
	out := plain(m)
	if !strings.Contains(out, "TRANSACTION") || !strings.Contains(out, "previewing the transaction") {
		t.Errorf("no preview spinner:\n%s", out)
	}
}

func TestPreviewStaleGenerationIgnored(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 30)
	m = send(t, m, press("r"))
	before := m.tx.DownloadBytes
	m.tx.DownloadBytes = 1
	m = send(t, m, previewMsg{gen: 1})
	if m.tx.DownloadBytes != 1 {
		t.Error("a stale preview was applied")
	}
	_ = before
}

func TestPreviewRefresh(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 48)
	m = send(t, m, press("r"))
	// Everything reloads, the preview after the repo updates.
	for _, c := range m.loadCmds(true) {
		m = run(t, m, c)
	}
	if m.anyLoading() || !m.previewed || len(m.tx.New) != 2 {
		t.Errorf("loading %v, previewed %v, new %d", m.anyLoading(), m.previewed, len(m.tx.New))
	}
}

func TestPreviewFilter(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 48)
	m = send(t, m, press("/"))
	for _, r := range "iptables" {
		m = send(t, m, press(string(r)))
	}
	out := plain(m)
	if !strings.Contains(out, "NEW (1)") || !strings.Contains(out, "REMOVED (1)") || strings.Contains(out, "HELD (") {
		t.Errorf("filter didn't narrow the groups:\n%s", out)
	}
}

func TestFreshBuildFlagInDetail(t *testing.T) {
	opts := fixtureOptions()
	cfg := config.Default()
	cfg.FlagFreshHours = 24
	opts.Config = &cfg
	m := selectEntry(t, loaded(t, opts, 100, 40), "repo:linux")
	if out := plain(m); !strings.Contains(out, "Fresh build: built 18 hours ago") {
		t.Errorf("detail lacks the fresh build reason:\n%s", out)
	}
}
