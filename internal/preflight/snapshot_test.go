package preflight

import (
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/risk"
)

func TestSnapshotProblem(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	max := 7 * day
	cases := []struct {
		name string
		f    SnapshotFacts
		ok   bool
		sev  risk.Severity
		want []string
	}{
		{"recent", SnapshotFacts{Tool: "Timeshift", Now: now, Last: now.Add(-2 * day), MaxAge: max}, false, 0, nil},
		{"exactly at the limit", SnapshotFacts{Tool: "Timeshift", Now: now, Last: now.Add(-max), MaxAge: max}, false, 0, nil},
		{"just over", SnapshotFacts{Tool: "Timeshift", Now: now, Last: now.Add(-max - time.Hour), MaxAge: max}, true, risk.Warn, []string{"7 days old", "limit 7 days"}},
		{"old", SnapshotFacts{Tool: "snapper", Now: now, Last: now.Add(-31 * day), MaxAge: max}, true, risk.Warn, []string{"snapper snapshot is 31 days old"}},
		{"none known", SnapshotFacts{Tool: "Timeshift", Now: now, MaxAge: max}, true, risk.Info, []string{"No Timeshift snapshot is known", "press S"}},
		{"only records", SnapshotFacts{Tool: "Timeshift", Now: now, Last: now.Add(-9 * day), MaxAge: max, FromRecords: true}, true, risk.Warn, []string{"Only the snapshots cronen took are known"}},
		{"with a hook", SnapshotFacts{Tool: "snapper", Now: now, Last: now.Add(-9 * day), MaxAge: max, Hook: "snap-pac"}, true, risk.Warn, []string{"snap-pac snapshots on every pacman transaction"}},
		{"no limit", SnapshotFacts{Tool: "snapper", Now: now, Last: now.Add(-900 * day)}, false, 0, nil},
	}
	for _, c := range cases {
		p, ok := SnapshotProblem(c.f)
		if ok != c.ok {
			t.Errorf("%s: ok = %v", c.name, ok)
			continue
		}
		if !ok {
			continue
		}
		if p.Severity != c.sev || p.Check != CheckSnapshot {
			t.Errorf("%s: %+v", c.name, p)
		}
		for _, w := range c.want {
			if !strings.Contains(p.Message, w) {
				t.Errorf("%s: %q lacks %q", c.name, p.Message, w)
			}
		}
	}
}

func TestSnapshotProblemSortsLast(t *testing.T) {
	ps := Add([]Problem{{risk.Info, CheckKeyring, "k"}}, Problem{risk.Info, CheckSnapshot, "s"})
	if ps[0].Check != CheckKeyring || ps[1].Check != CheckSnapshot {
		t.Errorf("%+v", ps)
	}
	ps = Add(ps, Problem{risk.Warn, CheckSnapshot, "old"})
	if ps[0].Check != CheckSnapshot || ps[0].Severity != risk.Warn {
		t.Errorf("a warning goes first: %+v", ps)
	}
}
