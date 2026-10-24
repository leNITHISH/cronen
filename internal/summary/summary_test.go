package summary

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/news"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/system"
)

const (
	firmwareGUID = "tag:archlinux.org,2026-10-22:/news/linux-firmware-20251021-1-upgrade-requires-manual-intervention/"
	nvidiaGUID   = "tag:archlinux.org,2026-10-21:/news/nvidia-590-driver-drops-pascal-support/"
)

func fixtureSources() Sources {
	return Sources{
		Runner:   fixtures.Runner(),
		Fetcher:  fixtures.Fetcher(),
		FS:       fixtures.FS(),
		Config:   config.Default(),
		Now:      func() time.Time { return fixtures.Now },
		Location: time.UTC,
	}
}

func TestGatherFixtures(t *testing.T) {
	s := Gather(context.Background(), fixtureSources())
	if len(s.Errors) > 0 {
		t.Fatalf("errors: %v", s.Errors)
	}
	// 16 repo updates and 2 regular AUR ones; the VCS package and debug
	// splits don't count.
	if s.Repo != 16 || s.AUR != 2 {
		t.Errorf("repo, aur = %d, %d; want 16, 2", s.Repo, s.AUR)
	}
	if len(s.Interventions) != 1 || s.Interventions[0].GUID != firmwareGUID {
		t.Errorf("interventions = %v", s.Interventions)
	}
	if s.UnreadNews != 2 || s.Pacnew != 1 || !s.Reboot {
		t.Errorf("unread %d, pacnew %d, reboot %v; want 2, 1, true", s.UnreadNews, s.Pacnew, s.Reboot)
	}
	if len(s.Flagged) == 0 || s.Flagged[0].Severity() == risk.None {
		t.Errorf("flagged = %v", s.Flagged)
	}
	if got := s.ExitCode(); got != ExitIntervention {
		t.Errorf("exit code %d, want %d", got, ExitIntervention)
	}
}

func TestExitCodes(t *testing.T) {
	allRead := func(src *Sources) {
		src.State.MarkRead(firmwareGUID, nvidiaGUID)
	}
	only := func(lines string) func(*Sources) {
		return func(src *Sources) {
			r := fixtures.Runner()
			r.Set("checkupdates", system.FakeResult{Stdout: []byte(lines)})
			src.Runner = r
			src.Config.AUR = false
			allRead(src)
		}
	}
	tests := []struct {
		name  string
		setup func(*Sources)
		want  int
		class string
	}{
		{"intervention", func(*Sources) {}, ExitIntervention, "critical"},
		{"flagged", allRead, ExitAttention, "attention"},
		{"plain updates", only("zstd 1.5.7-2 -> 1.5.7-3\n"), ExitUpdates, "updates"},
		{"watchlist only", only("systemd 258.1-1 -> 258.2-1\n"), ExitAttention, "attention"},
		{"nothing", func(src *Sources) {
			r := fixtures.Runner()
			r.Set("checkupdates", fixtures.CheckupdatesNoneResult())
			src.Runner = r
			src.Config.AUR = false
			allRead(src)
		}, ExitNothing, "none"},
		{"unread news alone is nothing to do", func(src *Sources) {
			r := fixtures.Runner()
			r.Set("checkupdates", fixtures.CheckupdatesNoneResult())
			src.Runner = r
			src.Config.AUR = false
			src.State.MarkRead(firmwareGUID)
		}, ExitNothing, "none"},
		{"checkupdates fails", func(src *Sources) {
			r := fixtures.Runner()
			r.Set("checkupdates", fixtures.CheckupdatesErrorResult())
			src.Runner = r
		}, ExitError, "error"},
		{"news fails", func(src *Sources) {
			f := fixtures.Fetcher()
			f.Set(fixtures.NewsURL, system.FakeResponse{Status: 500})
			src.Fetcher = f
		}, ExitError, "error"},
		{"AUR fails", func(src *Sources) {
			r := fixtures.Runner()
			r.Set("pacman -Qm", system.FakeResult{Code: 1, Stderr: []byte("boom")})
			src.Runner = r
		}, ExitError, "error"},
		{"AUR disabled is not consulted", func(src *Sources) {
			r := fixtures.Runner()
			r.Set("pacman -Qm", system.FakeResult{Code: 1, Stderr: []byte("boom")})
			src.Runner = r
			src.Config.AUR = false
		}, ExitIntervention, "critical"},
		{"unreadable log and missing pacdiff are not errors", func(src *Sources) {
			fs := fixtures.FS()
			fs.ReadErr = errors.New("permission denied")
			src.FS = fs
			r := fixtures.Runner()
			r.Set("pacdiff -o", system.FakeResult{Err: system.ErrCommandNotFound})
			src.Runner = r
		}, ExitIntervention, "critical"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := fixtureSources()
			tt.setup(&src)
			s := Gather(context.Background(), src)
			if got := s.ExitCode(); got != tt.want {
				t.Errorf("exit code %d, want %d; summary %+v", got, tt.want, s)
			}
			if got := s.Class(); got != tt.class {
				t.Errorf("class %q, want %q", got, tt.class)
			}
		})
	}
}

// Marking news read in state, or a later upgrade cronen ran, makes it read.
func TestGatherState(t *testing.T) {
	src := fixtureSources()
	src.State.LastUpgrade = fixtures.Now.Add(-time.Hour)
	s := Gather(context.Background(), src)
	if s.UnreadNews != 0 || len(s.Interventions) != 0 {
		t.Errorf("unread %d, interventions %d after a recent upgrade", s.UnreadNews, len(s.Interventions))
	}
}

func TestJSON(t *testing.T) {
	s := Gather(context.Background(), fixtureSources())
	var got map[string]any
	if err := json.Unmarshal(s.JSON(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"updates": 18.0, "aur": 2.0, "flagged": float64(len(s.Flagged)), "critical": 1.0,
		"unread_news": 2.0, "pacnew": 1.0, "reboot": true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	s.Errors = []error{errors.New("checkupdates: boom")}
	if err := json.Unmarshal(s.JSON(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got["errors"], []any{"checkupdates: boom"}) {
		t.Errorf("errors = %v", got["errors"])
	}
}

func TestText(t *testing.T) {
	s := Gather(context.Background(), fixtureSources())
	text := s.Text()
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if want := "18 updates (2 AUR) · " + itoa(len(s.Flagged)) + " flagged · 2 unread news · 1 .pacnew"; lines[0] != want {
		t.Errorf("headline %q, want %q", lines[0], want)
	}
	if !strings.HasPrefix(lines[1], "‼ manual intervention: linux-firmware >= 20251021-1") {
		t.Errorf("intervention not listed first: %q", lines[1])
	}
	if !strings.Contains(text, "‼ linux-firmware: ") || !strings.Contains(text, "reboot after upgrading") {
		t.Errorf("text lacks the flagged package or reboot note:\n%s", text)
	}
	if strings.Contains(text, "\x1b") {
		t.Errorf("text contains escape sequences: %q", text)
	}
	// Severity order: no info line before a warn line.
	seenInfo := false
	for _, l := range lines[2:] {
		switch {
		case strings.HasPrefix(l, "ℹ"):
			seenInfo = true
		case strings.HasPrefix(l, "⚠") && seenInfo:
			t.Errorf("warn after info:\n%s", text)
		}
	}
}

func TestTextStripsControlCharacters(t *testing.T) {
	s := Summary{Interventions: []news.Item{{Title: "evil\ntitle\x1b[31m"}}}
	if got := s.Text(); strings.Count(got, "\n") != 2 || strings.Contains(got, "\x1b") {
		t.Errorf("text = %q", got)
	}
}

func TestWaybar(t *testing.T) {
	type out struct{ Text, Alt, Tooltip, Class string }
	decode := func(s Summary) out {
		var o out
		if err := json.Unmarshal(s.Waybar(), &o); err != nil {
			t.Fatal(err)
		}
		return o
	}

	s := Summary{
		Repo: 3,
		Flagged: []risk.Package{{
			Update: risk.Update{Name: "glibc"},
			Flags:  []risk.Flag{{Severity: risk.Warn, Reason: "Mentioned in news: 'a <b> & c'"}},
		}},
		Interventions: []news.Item{{Title: "x >= 2 & <y> requires manual intervention"}},
	}
	o := decode(s)
	if o.Text != "3" || o.Class != "critical" || o.Alt != "critical" {
		t.Errorf("got %+v", o)
	}
	if strings.ContainsAny(strings.NewReplacer("&amp;", "", "&lt;", "", "&gt;", "").Replace(o.Tooltip), "<>&") {
		t.Errorf("tooltip not Pango-escaped: %q", o.Tooltip)
	}
	if !strings.Contains(o.Tooltip, "x &gt;= 2 &amp; &lt;y&gt;") {
		t.Errorf("tooltip = %q", o.Tooltip)
	}

	if o := decode(Summary{}); o.Text != "" || o.Class != "none" {
		t.Errorf("nothing to do: %+v, want empty text so waybar hides the module", o)
	}
	if o := decode(Summary{Repo: 2, Errors: []error{errors.New("boom")}}); o.Text != "!" || o.Class != "error" {
		t.Errorf("error: %+v", o)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
