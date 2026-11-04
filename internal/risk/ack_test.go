package risk

import (
	"slices"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/news"
)

// acks is a fake acknowledgement store: name → version → rule IDs.
func acks(m map[string]map[string][]string) Acknowledged {
	return func(name, version string) []string { return m[name][version] }
}

func ruleSet(fs []Flag) []Rule {
	var out []Rule
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	return out
}

func TestAcknowledge(t *testing.T) {
	// linux: kernel + watchlist (info), plus a major bump (warn).
	linux := Update{Name: "linux", OldVersion: "5.1-1", NewVersion: "6.1-1"}
	manual := news.Item{GUID: "g", Title: "linux requires manual intervention", Text: "do it", Published: time.Unix(0, 0)}
	in := Input{
		Updates:   []Update{linux, {Name: "glibc", OldVersion: "2.40-1", NewVersion: "2.41-1"}},
		Watchlist: []string{"linux", "glibc"},
		Now:       time.Unix(86400, 0),
	}
	base := Assess(in)
	if got := base.Packages[0].Flags; len(got) != 3 {
		t.Fatalf("setup: flags = %v", ruleSet(got))
	}
	critIn := in
	critIn.UnreadNews = []news.Item{manual}
	crit := Assess(critIn)

	tests := []struct {
		name       string
		res        Result
		acked      map[string]map[string][]string
		wantFlags  []Rule // flags left on linux
		wantAcked  []Rule // flags moved on linux
		wantPkgOut []string
	}{
		{"nothing acknowledged", base, nil, ruleSet(base.Packages[0].Flags), nil, nil},
		{
			"every rule for this version",
			base,
			map[string]map[string][]string{"linux": {"6.1-1": {"kernel", "major-bump", "watchlist"}}},
			nil, []Rule{RuleMajorBump, RuleWatchlist, RuleKernel}, []string{"linux"},
		},
		{
			"a new version brings the flags back",
			base,
			map[string]map[string][]string{"linux": {"6.0-9": {"kernel", "major-bump", "watchlist"}}},
			[]Rule{RuleMajorBump, RuleWatchlist, RuleKernel}, nil, nil,
		},
		{
			"a rule that wasn't acknowledged stays flagged",
			base,
			map[string]map[string][]string{"linux": {"6.1-1": {"kernel", "watchlist"}}},
			[]Rule{RuleMajorBump}, []Rule{RuleWatchlist, RuleKernel}, nil,
		},
		{
			"critical flags can't be acknowledged even if their rule is",
			crit,
			map[string]map[string][]string{"linux": {"6.1-1": {"news", "kernel", "major-bump", "watchlist"}}},
			[]Rule{RuleNews}, []Rule{RuleMajorBump, RuleWatchlist, RuleKernel}, nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := tt.res.Packages[0].Flags
			got := Acknowledge(tt.res, acks(tt.acked))
			p := got.Packages[0]
			if !slices.Equal(ruleSet(p.Flags), tt.wantFlags) {
				t.Errorf("flags = %v, want %v", ruleSet(p.Flags), tt.wantFlags)
			}
			if !slices.Equal(ruleSet(p.Acknowledged), tt.wantAcked) {
				t.Errorf("acknowledged = %v, want %v", ruleSet(p.Acknowledged), tt.wantAcked)
			}
			var out []string
			for _, q := range got.AcknowledgedPackages() {
				out = append(out, q.Name)
			}
			if !slices.Equal(out, tt.wantPkgOut) {
				t.Errorf("AcknowledgedPackages = %v, want %v", out, tt.wantPkgOut)
			}
			// The input isn't modified, and other packages are untouched.
			if !slices.Equal(ruleSet(tt.res.Packages[0].Flags), ruleSet(before)) {
				t.Error("Acknowledge modified its input")
			}
			if len(got.Packages[1].Acknowledged) != 0 {
				t.Errorf("glibc was acknowledged: %v", got.Packages[1].Acknowledged)
			}
		})
	}
}

func TestAcknowledgeableRules(t *testing.T) {
	manual := news.Item{GUID: "g", Title: "linux requires manual intervention", Published: time.Unix(0, 0)}
	res := Assess(Input{
		Updates:    []Update{{Name: "linux", OldVersion: "5.1-1", NewVersion: "6.1-1"}},
		Watchlist:  []string{"linux"},
		UnreadNews: []news.Item{manual},
		Now:        time.Unix(86400, 0),
	})
	p := res.Packages[0]
	if p.Severity() != Critical {
		t.Fatalf("setup: severity %v", p.Severity())
	}
	got := AcknowledgeableRules(p)
	want := []string{"kernel", "major-bump", "watchlist"}
	if !slices.Equal(got, want) {
		t.Errorf("rules = %v, want %v (no news: that flag is critical)", got, want)
	}
	// A package with only a critical flag has nothing to acknowledge.
	only := Package{Flags: []Flag{{Rule: RuleNews, Severity: Critical}}}
	if got := AcknowledgeableRules(only); len(got) != 0 {
		t.Errorf("rules = %v, want none", got)
	}
}
