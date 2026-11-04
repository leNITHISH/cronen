package risk

import (
	"reflect"
	"slices"
	"testing"

	"github.com/leNITHISH/cronen/internal/rebuild"
)

func rebuildReasons(p Package) []string {
	var out []string
	for _, f := range p.Flags {
		if f.Rule == RuleRebuild {
			if f.Severity != Warn {
				panic("rebuild flags are warn")
			}
			out = append(out, f.Reason)
		}
	}
	return out
}

func TestRebuildFlags(t *testing.T) {
	icu := Update{Name: "icu", OldVersion: "75.1-1", NewVersion: "76.1-1"}
	find := func(pkg string, s rebuild.Signal, detail string) rebuild.Finding {
		return rebuild.Finding{Package: pkg, Signal: s, Culprit: "icu", CulpritOld: "75.1-1", CulpritNew: "76.1-1", Detail: detail}
	}
	tests := []struct {
		name     string
		findings []rebuild.Finding
		want     []string
	}{
		{"none", nil, nil},
		{
			"elf",
			[]rebuild.Finding{find("foo", rebuild.SignalELF, "libicuuc.so.75")},
			[]string{"Needs rebuild: foo links against libicuuc.so.75 (icu 75.1 → 76.1)"},
		},
		{
			"metadata, several packages share a flag",
			[]rebuild.Finding{find("b", rebuild.SignalMetadata, "libicuuc.so.75"), find("a", rebuild.SignalMetadata, "libicuuc.so.75")},
			[]string{"Needs rebuild: a, b depend on libicuuc.so.75 (icu 75.1 → 76.1)"},
		},
		{
			"both signals show",
			[]rebuild.Finding{find("x", rebuild.SignalELF, "libicuuc.so.75"), find("x", rebuild.SignalMetadata, "libicuuc.so.75")},
			[]string{
				"Needs rebuild: x depends on libicuuc.so.75 (icu 75.1 → 76.1)",
				"Needs rebuild: x links against libicuuc.so.75 (icu 75.1 → 76.1)",
			},
		},
		{
			"another culprit",
			[]rebuild.Finding{{Package: "foo", Signal: rebuild.SignalELF, Culprit: "zstd", Detail: "libzstd.so.1"}},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Assess(Input{Updates: []Update{icu}, Rebuilds: tt.findings})
			got := rebuildReasons(res.Packages[0])
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("reasons = %q, want %q", got, tt.want)
			}
			if len(tt.want) > 0 && res.Packages[0].Severity() != Warn {
				t.Errorf("severity = %v, want warn", res.Packages[0].Severity())
			}
			if len(res.Rebuilds) != len(tt.findings) {
				t.Errorf("Result.Rebuilds = %d, want %d", len(res.Rebuilds), len(tt.findings))
			}
		})
	}
}

func TestPythonBumpFoldsInModuleDirs(t *testing.T) {
	py := Update{Name: "python", OldVersion: "3.13.7-1", NewVersion: "3.14.0-1"}
	module := func(pkg string) rebuild.Finding {
		return rebuild.Finding{Package: pkg, Signal: rebuild.SignalModuleDir, Culprit: "python", Detail: "/usr/lib/python3.13/"}
	}

	// The findings name the packages in the one Python flag, and don't get
	// a second flag of their own.
	res := Assess(Input{Updates: []Update{py}, Rebuilds: []rebuild.Finding{module("python-pywal"), module("python-foo")}})
	var python []string
	for _, f := range res.Packages[0].Flags {
		switch f.Rule {
		case RulePythonBump:
			python = append(python, f.Reason)
		case RuleRebuild:
			t.Errorf("unexpected rebuild flag: %s", f.Reason)
		}
	}
	want := []string{"Python 3.13 → 3.14: rebuild these AUR packages: python-foo, python-pywal"}
	if !slices.Equal(python, want) {
		t.Errorf("python flags = %q, want %q", python, want)
	}

	// The warning stays when nothing is found.
	res = Assess(Input{Updates: []Update{py}})
	if got := res.Packages[0].Flags[0].Reason; got != "Python 3.13 → 3.14: AUR packages that use Python may need rebuilding" {
		t.Errorf("reason = %q", got)
	}

	// Other interpreters get the generic flag.
	perl := Update{Name: "perl", OldVersion: "5.40.2-1", NewVersion: "5.42.0-1"}
	res = Assess(Input{Updates: []Update{perl}, Rebuilds: []rebuild.Finding{
		{Package: "perl-foo", Signal: rebuild.SignalModuleDir, Culprit: "perl", Detail: "/usr/lib/perl5/5.40/"},
	}})
	got := rebuildReasons(res.Packages[0])
	if want := []string{"Needs rebuild: perl-foo installs into /usr/lib/perl5/5.40/ (perl 5.40.2 → 5.42.0)"}; !slices.Equal(got, want) {
		t.Errorf("perl reasons = %q, want %q", got, want)
	}
}
