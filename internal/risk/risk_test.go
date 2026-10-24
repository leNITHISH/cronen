package risk

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/news"
)

var now = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func fixtureNews(t *testing.T) []news.Item {
	t.Helper()
	items, err := news.Parse(fixtures.Bytes(fixtures.News))
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// fixtureUpdates parses checkupdates.txt. Bases come from the archweb
// fixture where it has them.
func fixtureUpdates(t *testing.T) []Update {
	t.Helper()
	bases := map[string]string{"linux-headers": "linux"}
	var ups []Update
	for line := range strings.Lines(string(fixtures.Bytes(fixtures.Checkupdates))) {
		var u Update
		var arrow string
		if _, err := fmt.Sscan(line, &u.Name, &u.OldVersion, &arrow, &u.NewVersion); err != nil {
			t.Fatalf("bad line %q: %v", line, err)
		}
		u.Base = bases[u.Name]
		ups = append(ups, u)
	}
	return ups
}

func item(title, text string, published time.Time) news.Item {
	return news.Item{GUID: title, Title: title, Text: text, Published: published}
}

// summary renders a package's flags as "rule/severity" strings.
func summary(p Package) string {
	var s []string
	for _, f := range p.Flags {
		s = append(s, f.Rule.String()+"/"+f.Severity.String())
	}
	return strings.Join(s, " ")
}

func assessOne(u Update, in Input) Package {
	in.Updates = []Update{u}
	if in.Now.IsZero() {
		in.Now = now
	}
	return Assess(in).Packages[0]
}

func TestNewsRule(t *testing.T) {
	day := 24 * time.Hour
	nvidiaPost := item("NVIDIA 590 driver drops Pascal support", "The nvidia 590 driver series no longer supports Pascal.", now.Add(-2*day-time.Hour))
	firmwarePost := item("linux-firmware >= 20251021-1 upgrade requires manual intervention",
		"With 20251021-1, the firmware files have been split. pacman -Rdd linux-firmware", now.Add(-26*time.Hour))

	tests := []struct {
		name string
		u    Update
		news []news.Item
		want string
	}{
		{"mentioned by name in body", Update{Name: "nvidia-utils"},
			[]news.Item{item("Driver news", "Upgrade nvidia-utils first.", now)}, "news/warn"},
		{"mentioned by name in title", Update{Name: "wine"},
			[]news.Item{item("wine is now WoW64", "", now)}, "news/warn"},
		{"mentioned by pkgbase", Update{Name: "nvidia-utils", Base: "nvidia"},
			[]news.Item{nvidiaPost}, "news/warn"},
		{"case-insensitive", Update{Name: "nvidia-utils", Base: "nvidia"},
			[]news.Item{item("NVIDIA drops Pascal", "", now)}, "news/warn"},
		{"sentence-ending period", Update{Name: "zstd"},
			[]news.Item{item("x", "Please upgrade zstd.", now)}, "news/warn"},
		{"inside punctuation", Update{Name: "zstd"},
			[]news.Item{item("x", "Packages (zstd, xz) changed", now)}, "news/warn"},
		{"manual intervention mention is critical", Update{Name: "linux-firmware"},
			[]news.Item{firmwarePost}, "news/critical"},
		{"two posts, two flags", Update{Name: "nvidia-utils", Base: "nvidia"},
			[]news.Item{nvidiaPost, item("nvidia-utils needs manual intervention", "", now)}, "news/critical news/warn"},

		// False positives.
		{"linux-firmware news doesn't mention linux", Update{Name: "linux-headers", Base: "linux"},
			[]news.Item{firmwarePost}, ""},
		{"linux-firmware news doesn't mention linux-api-headers", Update{Name: "linux-api-headers"},
			[]news.Item{firmwarePost}, ""},
		{"prefix of a longer name", Update{Name: "nvidia"},
			[]news.Item{item("x", "switch to nvidia-580xx-dkms", now)}, ""},
		{"suffix of a longer name", Update{Name: "utils"},
			[]news.Item{item("x", "upgrade nvidia-utils", now)}, ""},
		{"inside a word", Update{Name: "vim"},
			[]news.Item{item("x", "neovim and vimb are fine", now)}, ""},
		{"dot inside a token", Update{Name: "glibc"},
			[]news.Item{item("x", "glibc.so.6 moved", now)}, ""},
		{"Arch Linux is the distribution", Update{Name: "linux"},
			[]news.Item{item("Arch Linux news", "Welcome to arch linux", now)}, "kernel/info"},
		{"no news", Update{Name: "nvidia-utils"}, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := assessOne(tt.u, Input{UnreadNews: tt.news})
			if got := summary(p); got != tt.want {
				t.Errorf("flags = %q, want %q (%+v)", got, tt.want, p.Flags)
			}
		})
	}
}

func TestNewsReasons(t *testing.T) {
	tests := []struct {
		published time.Time
		want      string
	}{
		{now.Add(-2*24*time.Hour - time.Hour), "Mentioned in news: 'NVIDIA 590 driver drops Pascal support' (2 days ago)"},
		{now.Add(-30 * time.Hour), "Mentioned in news: 'NVIDIA 590 driver drops Pascal support' (yesterday)"},
		{now.Add(-time.Hour), "Mentioned in news: 'NVIDIA 590 driver drops Pascal support' (today)"},
		{now.Add(time.Hour), "Mentioned in news: 'NVIDIA 590 driver drops Pascal support' (today)"},
		{time.Time{}, "Mentioned in news: 'NVIDIA 590 driver drops Pascal support'"},
	}
	for _, tt := range tests {
		it := item("NVIDIA 590 driver drops Pascal support", "", tt.published)
		it.GUID = "guid-1"
		p := assessOne(Update{Name: "nvidia-utils", Base: "nvidia"}, Input{UnreadNews: []news.Item{it}})
		if len(p.Flags) != 1 || p.Flags[0].Reason != tt.want || p.Flags[0].NewsGUID != "guid-1" {
			t.Errorf("published %v: flags = %+v, want reason %q", tt.published, p.Flags, tt.want)
		}
	}

	p := assessOne(Update{Name: "linux-firmware"}, Input{UnreadNews: []news.Item{
		item("linux-firmware upgrade requires manual intervention", "", now.Add(-26*time.Hour)),
	}})
	if want := "Manual intervention in news: 'linux-firmware upgrade requires manual intervention' (yesterday)"; p.Flags[0].Reason != want {
		t.Errorf("reason = %q, want %q", p.Flags[0].Reason, want)
	}
}

func TestInterventions(t *testing.T) {
	tests := []struct {
		name string
		it   news.Item
		want bool
	}{
		{"title", item("foo upgrade requires manual intervention", "", now), true},
		{"body", item("foo 2.0", "This upgrade needs manual intervention.", now), true},
		{"intervention required", item("foo", "Intervention required before upgrading.", now), true},
		{"requires manual", item("foo", "The switch requires manual steps.", now), true},
		{"case-insensitive", item("MANUAL INTERVENTION", "", now), true},
		{"plain news", item("Python 3.14 is coming to [core]", "Packages will need to be rebuilt.", now), false},
		{"intervention alone", item("foo", "No intervention is needed.", now), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RequiresManualIntervention(tt.it); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
			res := Assess(Input{UnreadNews: []news.Item{tt.it}, Now: now})
			if got := len(res.Interventions) == 1; got != tt.want {
				t.Errorf("banner with no updates: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInterventionsOnlyFromUnread(t *testing.T) {
	items := fixtureNews(t)
	firmwareGUID := items[0].GUID

	unread := news.ReadState{Now: now}.Unread(items)
	if res := Assess(Input{UnreadNews: unread, Now: now}); len(res.Interventions) != 1 || res.Interventions[0].GUID != firmwareGUID {
		t.Errorf("unread: interventions = %+v", res.Interventions)
	}

	read := news.ReadState{Now: now, Read: map[string]bool{firmwareGUID: true}}.Unread(items)
	if res := Assess(Input{UnreadNews: read, Now: now}); len(res.Interventions) != 0 {
		t.Errorf("read: interventions = %+v", res.Interventions)
	}
}

func TestWatchlistRule(t *testing.T) {
	tests := []struct {
		name      string
		u         Update
		watchlist []string
		want      string
	}{
		{"exact", Update{Name: "glibc"}, DefaultWatchlist, "On your watchlist (glibc)"},
		{"glob", Update{Name: "nvidia-utils"}, DefaultWatchlist, "On your watchlist (nvidia*)"},
		{"glob with dash", Update{Name: "vulkan-icd-loader"}, DefaultWatchlist, "On your watchlist (vulkan-*)"},
		{"pkgbase", Update{Name: "libfoo-docs", Base: "foo"}, []string{"foo"}, "On your watchlist (foo)"},
		{"first matching pattern wins", Update{Name: "linux"}, []string{"linux", "linux*"}, "On your watchlist (linux)"},
		{"exact doesn't glob", Update{Name: "mesa-utils"}, DefaultWatchlist, ""},
		{"vulkan-* needs the dash", Update{Name: "vulkan"}, []string{"vulkan-*"}, ""},
		{"empty watchlist", Update{Name: "glibc"}, nil, ""},
		{"malformed pattern is skipped", Update{Name: "glibc"}, []string{"[", "glib?"}, "On your watchlist (glib?)"},
		{"malformed pattern alone", Update{Name: "["}, []string{"["}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, ok := watchlistFlag(tt.u, tt.watchlist)
			if got := map[bool]string{true: f.Reason}[ok]; got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if ok && f.Severity != Info {
				t.Errorf("severity = %v", f.Severity)
			}
		})
	}
}

func TestMajorBumpRule(t *testing.T) {
	tests := []struct {
		old, new string
		want     string
	}{
		{"14.1.1-1", "15.0.0-1", "Major version bump (14 → 15)"},
		{"1.9-1", "2.0-1", "Major version bump (1 → 2)"},
		{"1:1.9-1", "1:2.0-1", "Major version bump (1 → 2)"},
		{"1.9-1", "1:2.0-1", "Major version bump (1 → 2)"},
		{"9.0-1", "10.0-1", "Major version bump (9 → 10)"},
		{"48.2-1", "49.0-1", "Major version bump (48 → 49)"},
		{"49.1-1", "50.0-1", "Major version bump (49 → 50)"},

		{"50.0-1", "51.0-1", ""},                   // at the cap
		{"141.0.7390.65-1", "142.0.7444.59-1", ""}, // chromium
		{"126-1", "127-1", ""},                     // polkit
		{"580.95.05-1", "590.44.01-1", ""},         // nvidia
		{"2024.1-1", "2025.1-1", ""},               // year version
		{"99999999999999999999-1", "1:1-1", ""},    // doesn't fit an int

		{"2.4.8-1", "1:2.4.8-2", ""},               // epoch only
		{"1.0-1", "1:0.5-1", ""},                   // epoch restarts numbering lower
		{"1:25.2.4-1", "1:25.2.5-1", ""},           // epoch, minor change
		{"6.17.4.arch1-1", "6.18.1.arch1-1", ""},   // minor
		{"20251011-1", "20251021-1", ""},           // date version
		{"20251011.abc-1", "20260101.def-1", ""},   // date version, new year
		{"10.0p1-3", "10.2p1-1", ""},               // letters in a component
		{"2.42+r17+g6f0-1", "2.42+r33+gc7c-1", ""}, // git suffix
		{"258.1-1", "258.2-1", ""},                 // pkgrel and minor
		{"1.5.7-2", "1.5.7-3", ""},                 // pkgrel only
		{"r123.abc-1", "r130.def-1", ""},           // no leading number
		{"", "2.0-1", ""},                          // missing version
	}
	for _, tt := range tests {
		f, ok := majorBumpFlag(Update{Name: "pkg", OldVersion: tt.old, NewVersion: tt.new}, nil)
		if got := map[bool]string{true: f.Reason}[ok]; got != tt.want {
			t.Errorf("%s → %s: got %q, want %q", tt.old, tt.new, got, tt.want)
		}
		if ok && f.Severity != Warn {
			t.Errorf("%s → %s: severity = %v", tt.old, tt.new, f.Severity)
		}
	}
}

func TestIgnoreMajor(t *testing.T) {
	u := Update{Name: "ripgrep-all", Base: "ripgrep", OldVersion: "1.0-1", NewVersion: "2.0-1"}
	tests := []struct {
		ignore []string
		want   bool
	}{
		{nil, true},
		{[]string{"firefox"}, true},
		{[]string{"ripgrep-all"}, false},
		{[]string{"ripgrep"}, false}, // pkgbase
		{[]string{"rip*"}, false},
		{[]string{"[bad"}, true}, // malformed patterns never match
	}
	for _, tt := range tests {
		if _, got := majorBumpFlag(u, tt.ignore); got != tt.want {
			t.Errorf("ignore %q: flagged = %v, want %v", tt.ignore, got, tt.want)
		}
	}
	// Ignoring the major bump leaves the other rules alone.
	p := assessOne(Update{Name: "linux", OldVersion: "6.17-1", NewVersion: "7.0-1"}, Input{
		Watchlist: []string{"linux*"}, IgnoreMajor: []string{"linux"},
	})
	if got := summary(p); got != "watchlist/info kernel/info" {
		t.Errorf("linux flags = %q", got)
	}
}

func TestPythonRule(t *testing.T) {
	tests := []struct {
		name       string
		u          Update
		dependents []string
		want       string
	}{
		{"minor bump with dependents", Update{Name: "python", OldVersion: "3.13.7-1", NewVersion: "3.14.0-1"},
			[]string{"python-pywal", "gnome-shell-extension-legacy"},
			"Python 3.13 → 3.14: rebuild these AUR packages: python-pywal, gnome-shell-extension-legacy"},
		{"minor bump without dependents", Update{Name: "python", OldVersion: "3.13.7-1", NewVersion: "3.14.0-1"},
			nil, "Python 3.13 → 3.14: AUR packages that use Python may need rebuilding"},
		{"major bump", Update{Name: "python", OldVersion: "3.14.1-1", NewVersion: "4.0.0-1"},
			nil, "Python 3.14 → 4.0: AUR packages that use Python may need rebuilding"},
		{"two-component version", Update{Name: "python", OldVersion: "3.13-1", NewVersion: "3.14-1"},
			nil, "Python 3.13 → 3.14: AUR packages that use Python may need rebuilding"},
		{"patch release", Update{Name: "python", OldVersion: "3.14.0-1", NewVersion: "3.14.1-1"}, nil, ""},
		{"pkgrel", Update{Name: "python", OldVersion: "3.14.0-1", NewVersion: "3.14.0-2"}, nil, ""},
		{"other python package", Update{Name: "python-requests", OldVersion: "2.31-1", NewVersion: "3.0-1"}, nil, ""},
		{"unparseable", Update{Name: "python", OldVersion: "3-1", NewVersion: "3.14-1"}, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, ok := pythonFlag(tt.u, tt.dependents)
			if got := map[bool]string{true: f.Reason}[ok]; got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if ok && f.Severity != Warn {
				t.Errorf("severity = %v", f.Severity)
			}
		})
	}
}

func TestKernelRule(t *testing.T) {
	for name, want := range map[string]bool{
		"linux":               true,
		"linux-lts":           true,
		"linux-zen":           true,
		"linux-hardened":      true,
		"linux-rt-lts":        true,
		"linux-headers":       false,
		"linux-lts-headers":   false,
		"linux-docs":          false,
		"linux-api-headers":   false,
		"linux-firmware":      false,
		"linux-firmware-qcom": false,
		"linux-tools-meta":    false,
		"linuxconsoletools":   false,
		"util-linux":          false,
	} {
		if got := IsKernel(name); got != want {
			t.Errorf("IsKernel(%q) = %v, want %v", name, got, want)
		}
		res := Assess(Input{Updates: []Update{{Name: name}}, Now: now})
		if res.RebootRequired != want {
			t.Errorf("%s: RebootRequired = %v", name, res.RebootRequired)
		}
		if got := summary(res.Packages[0]) == "kernel/info"; got != want {
			t.Errorf("%s: flags = %q", name, summary(res.Packages[0]))
		}
	}

	if res := Assess(Input{Updates: []Update{{Name: "zstd"}}, Now: now}); res.RebootRequired {
		t.Error("no kernel: RebootRequired should be false")
	}
}

// TestFixtureSystem runs the whole fixture system through Assess, the way the
// app will.
func TestFixtureSystem(t *testing.T) {
	lastUpgrade := time.Date(2026, 10, 20, 13, 32, 11, 0, time.UTC)
	unread := news.ReadState{LastUpgrade: lastUpgrade, Now: now}.Unread(fixtureNews(t))

	tests := []struct {
		name      string
		watchlist []string
		want      map[string]string
	}{
		{
			name:      "default watchlist",
			watchlist: DefaultWatchlist,
			want: map[string]string{
				"glibc":             "watchlist/info",
				"linux":             "watchlist/info kernel/info",
				"linux-api-headers": "watchlist/info",
				"linux-firmware":    "news/critical watchlist/info",
				"linux-headers":     "watchlist/info",
				"mesa":              "watchlist/info",
				"nvidia-utils":      "news/warn watchlist/info",
				"python":            "python-bump/warn watchlist/info",
				"ripgrep":           "major-bump/warn",
				"systemd":           "watchlist/info",
				"vulkan-icd-loader": "watchlist/info",
			},
		},
		{
			// linux-firmware news must not flag other linux-* packages when
			// the watchlist doesn't cover them.
			name:      "empty watchlist",
			watchlist: nil,
			want: map[string]string{
				"linux":          "kernel/info",
				"linux-firmware": "news/critical",
				"nvidia-utils":   "news/warn",
				"python":         "python-bump/warn",
				"ripgrep":        "major-bump/warn",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Assess(Input{
				Updates:          fixtureUpdates(t),
				UnreadNews:       unread,
				Watchlist:        tt.watchlist,
				PythonDependents: []string{"python-pywal"},
				Now:              now,
			})
			got := map[string]string{}
			for _, p := range res.Flagged() {
				got[p.Name] = summary(p)
			}
			for name, want := range tt.want {
				if got[name] != want {
					t.Errorf("%s: flags = %q, want %q", name, got[name], want)
				}
			}
			for name, flags := range got {
				if _, ok := tt.want[name]; !ok {
					t.Errorf("%s: unexpected flags %q", name, flags)
				}
			}
			if len(res.Packages) != len(fixtureUpdates(t)) {
				t.Errorf("got %d packages", len(res.Packages))
			}
			if len(res.Interventions) != 1 || !res.RebootRequired {
				t.Errorf("interventions = %d, reboot = %v", len(res.Interventions), res.RebootRequired)
			}
			if c := res.Count(Critical); c != 1 {
				t.Errorf("critical = %d", c)
			}
		})
	}
}

func TestSeverity(t *testing.T) {
	p := assessOne(Update{Name: "zstd"}, Input{})
	if p.Severity() != None || len(p.Flags) != 0 {
		t.Errorf("unflagged: %v %+v", p.Severity(), p.Flags)
	}
	p = assessOne(Update{Name: "linux", OldVersion: "6.18-1", NewVersion: "7.0-1"}, Input{Watchlist: DefaultWatchlist})
	if p.Severity() != Warn || summary(p) != "major-bump/warn watchlist/info kernel/info" {
		t.Errorf("got %v %q", p.Severity(), summary(p))
	}
	for s, want := range map[Severity]string{None: "none", Info: "info", Warn: "warn", Critical: "critical", 9: "Severity(9)"} {
		if s.String() != want {
			t.Errorf("%d.String() = %q", int(s), s.String())
		}
	}
	if Rule(0).String() != "Rule(0)" {
		t.Errorf("Rule(0) = %q", Rule(0).String())
	}
}
