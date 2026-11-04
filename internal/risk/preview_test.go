package risk

import (
	"testing"
	"time"
)

func TestFreshBuildRule(t *testing.T) {
	built := func(ago time.Duration) map[string]time.Time {
		return map[string]time.Time{"foo": now.Add(-ago)}
	}
	u := Update{Name: "foo", OldVersion: "1.0-1", NewVersion: "1.0.1-1"}
	aurUpdate := Update{Name: "foo", OldVersion: "1.0-1", NewVersion: "1.1-1", AUR: true}
	tests := []struct {
		name   string
		in     Input
		update Update
		want   string
		reason string
	}{
		{"off by default", Input{Builds: built(time.Hour)}, u, "", ""},
		{"younger than the limit", Input{Builds: built(5 * time.Hour), FreshHours: 24}, u, "fresh-build/info", "Fresh build: built 5 hours ago"},
		{"minutes", Input{Builds: built(30 * time.Minute), FreshHours: 2}, u, "fresh-build/info", "Fresh build: built 30 minutes ago"},
		{"exactly at the limit", Input{Builds: built(24 * time.Hour), FreshHours: 24}, u, "", ""},
		{"older", Input{Builds: built(72 * time.Hour), FreshHours: 24}, u, "", ""},
		{"no build date known", Input{FreshHours: 24}, u, "", ""},
		{"zero build date", Input{Builds: map[string]time.Time{"foo": {}}, FreshHours: 24}, u, "", ""},
		{"AUR packages have no build date", Input{Builds: built(time.Hour), FreshHours: 24}, aurUpdate, "", ""},
		{"negative limit", Input{Builds: built(time.Hour), FreshHours: -1}, u, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.in.Now = now
			p := assessOne(tt.update, tt.in)
			if got := summary(p); got != tt.want {
				t.Fatalf("flags = %q, want %q", got, tt.want)
			}
			if tt.reason != "" && p.Flags[0].Reason != tt.reason {
				t.Errorf("reason = %q, want %q", p.Flags[0].Reason, tt.reason)
			}
		})
	}
}

func TestHeldRule(t *testing.T) {
	res := Assess(Input{
		Updates: []Update{{Name: "a", OldVersion: "1.0-1", NewVersion: "1.1-1"}},
		Held:    []Update{{Name: "bash", OldVersion: "5.3.3-1", NewVersion: "5.3.3-2"}},
		Now:     now,
	})
	if len(res.Held) != 1 || summary(res.Held[0]) != "held/info" {
		t.Fatalf("held = %+v", res.Held)
	}
	const want = "Held by IgnorePkg: 5.3.3-1 → 5.3.3-2. Holding packages is a partial upgrade."
	if got := res.Held[0].Flags[0].Reason; got != want {
		t.Errorf("reason = %q, want %q", got, want)
	}
	// Held packages aren't pending updates and never count as flagged.
	if len(res.Packages) != 1 || len(res.Flagged()) != 0 || res.Count(Info) != 0 {
		t.Errorf("held leaked into the updates: %+v", res)
	}
}

func TestAge(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{-time.Hour, "just now"},
		{30 * time.Second, "just now"},
		{time.Minute, "1 minute ago"},
		{59 * time.Minute, "59 minutes ago"},
		{time.Hour, "1 hour ago"},
		{47 * time.Hour, "47 hours ago"},
		{48 * time.Hour, "2 days ago"},
		{10 * 24 * time.Hour, "10 days ago"},
	}
	for _, tt := range tests {
		if got := Age(now.Add(-tt.d), now); got != tt.want {
			t.Errorf("Age(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
