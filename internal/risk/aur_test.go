package risk

import (
	"slices"
	"strings"
	"testing"
)

func TestMaintainerReasons(t *testing.T) {
	m := func(name string, co ...string) *Maintainers { return &Maintainers{Maintainer: name, Co: co} }
	tests := []struct {
		name string
		rec  *Maintainers
		cur  Maintainers
		want []string // substrings, one per reason
	}{
		{"nothing recorded is the baseline", nil, Maintainers{Maintainer: "bob"}, nil},
		{"unchanged", m("alice", "x", "y"), Maintainers{Maintainer: "alice", Co: []string{"y", "x"}}, nil},
		{"orphan stays orphaned", m(""), Maintainers{}, nil},
		{"maintainer changed", m("alice"), Maintainers{Maintainer: "bob"}, []string{"alice → bob"}},
		{"orphan adopted", m(""), Maintainers{Maintainer: "bob"}, []string{"adopted by bob"}},
		{"maintainer left", m("alice"), Maintainers{}, []string{"orphaned by its maintainer alice"}},
		{"co-maintainer added", m("alice"), Maintainers{Maintainer: "alice", Co: []string{"eve"}}, []string{"+eve"}},
		{"co-maintainer swapped", m("alice", "x"), Maintainers{Maintainer: "alice", Co: []string{"y"}}, []string{"+y -x"}},
		{"both changed", m("alice"), Maintainers{Maintainer: "bob", Co: []string{"eve"}}, []string{"alice → bob", "+eve"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaintainerReasons(MaintainerCheck{Recorded: tt.rec, Current: tt.cur})
			if len(got) != len(tt.want) {
				t.Fatalf("got %q, want %d reasons %q", got, len(tt.want), tt.want)
			}
			for i, w := range tt.want {
				if !strings.Contains(got[i], w) {
					t.Errorf("reason %d = %q, want %q in it", i, got[i], w)
				}
			}
		})
	}
}

func TestAddAUR(t *testing.T) {
	res := Assess(Input{Updates: []Update{
		{Name: "foo", OldVersion: "1-1", NewVersion: "2-1", AUR: true},
		{Name: "bar", OldVersion: "1-1", NewVersion: "1-2", AUR: true},
		{Name: "repopkg", OldVersion: "1-1", NewVersion: "1-2"},
	}})
	if len(res.Flagged()) != 1 {
		t.Fatalf("base flags: %+v", res.Flagged()) // foo: major bump
	}

	got := AddAUR(res, AURInput{
		Maintainers: map[string]MaintainerCheck{
			"foo":     {Recorded: &Maintainers{Maintainer: "a"}, Current: Maintainers{Maintainer: "b"}},
			"repopkg": {Recorded: &Maintainers{Maintainer: "a"}, Current: Maintainers{Maintainer: "b"}},
		},
		DiffReasons: map[string][]string{
			"bar":     {"pipes a download into a shell", "pipes a download into a shell", "adds an install script"},
			"repopkg": {"x"},
		},
	})

	byName := map[string]Package{}
	for _, p := range got.Packages {
		byName[p.Name] = p
	}
	foo := byName["foo"]
	if len(foo.Flags) != 2 {
		t.Errorf("foo flags = %+v", foo.Flags)
	}
	if !slices.ContainsFunc(foo.Flags, func(f Flag) bool { return f.Rule == RuleMaintainer && f.Severity == Warn }) {
		t.Errorf("foo has no maintainer flag: %+v", foo.Flags)
	}
	bar := byName["bar"]
	if len(bar.Flags) != 2 {
		t.Fatalf("bar flags = %+v (reasons deduplicated, one flag each)", bar.Flags)
	}
	for _, f := range bar.Flags {
		if f.Rule != RuleSuspicious || f.Severity != Warn || !strings.HasPrefix(f.Reason, "Suspicious PKGBUILD change: ") {
			t.Errorf("bar flag %+v", f)
		}
	}
	if n := len(byName["repopkg"].Flags); n != 0 {
		t.Errorf("repo package got %d AUR flags", n)
	}
	if len(got.Flagged()) != 2 {
		t.Errorf("flagged = %d, want 2", len(got.Flagged()))
	}
	// The input is untouched.
	if len(res.Flagged()) != 1 {
		t.Error("AddAUR modified its input")
	}
	if RuleMaintainer.String() != "aur-maintainer" || RuleSuspicious.String() != "aur-suspicious" {
		t.Error("rule names")
	}
}
