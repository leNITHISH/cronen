package summary

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/aur"
	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/risk"
)

func fixtureAUR(t *testing.T) []aur.Package {
	t.Helper()
	pkgs, err := LoadAUR(context.Background(), fixtures.Runner(), fixtures.Fetcher(), false)
	if err != nil {
		t.Fatal(err)
	}
	return pkgs
}

func flagRules(res risk.Result, name string) []risk.Rule {
	for _, p := range res.Packages {
		if p.Name == name {
			var rules []risk.Rule
			for _, f := range p.Flags {
				rules = append(rules, f.Rule)
			}
			return rules
		}
	}
	return nil
}

func assessFixtures(t *testing.T, pkgs []aur.Package, st config.State, reviews map[string]aur.Review) risk.Result {
	t.Helper()
	res := Assess(Updates(nil, pkgs), nil, nil, config.Default(), fixtures.Now)
	return AssessAUR(res, pkgs, st, reviews)
}

func TestMaintainerBaselineAndChange(t *testing.T) {
	pkgs := fixtureAUR(t)
	var st config.State

	// Nothing recorded: the current maintainers are the baseline, and
	// nothing is flagged.
	if res := assessFixtures(t, pkgs, st, nil); slices.Contains(flagRules(res, "visual-studio-code-bin"), risk.RuleMaintainer) {
		t.Fatal("flagged with nothing recorded")
	}
	if !RecordMaintainers(&st, pkgs, false) {
		t.Fatal("baseline not recorded")
	}
	if RecordMaintainers(&st, pkgs, false) {
		t.Error("recording twice reported a change")
	}
	if rec, ok := st.Maintainer("visual-studio-code-bin"); !ok || rec.Maintainer != "dcelasun" || !slices.Equal(rec.CoMaintainers, []string{"dallas"}) {
		t.Fatalf("recorded %+v, %v", rec, ok)
	}
	if res := assessFixtures(t, pkgs, st, nil); slices.Contains(flagRules(res, "visual-studio-code-bin"), risk.RuleMaintainer) {
		t.Fatal("flagged right after recording")
	}

	// The maintainer changes upstream (here: the recorded one differs).
	st.SetMaintainer(config.Maintainer{PkgBase: "visual-studio-code-bin", Maintainer: "someone-else", CoMaintainers: []string{"dallas"}})
	// A baseline pass must not overwrite what is recorded.
	if RecordMaintainers(&st, pkgs, false) {
		t.Error("a baseline pass overwrote a recorded maintainer")
	}
	res := assessFixtures(t, pkgs, st, nil)
	var reason string
	for _, p := range res.Packages {
		for _, f := range p.Flags {
			if p.Name == "visual-studio-code-bin" && f.Rule == risk.RuleMaintainer {
				reason = f.Reason
			}
		}
	}
	if !strings.Contains(reason, "someone-else → dcelasun") {
		t.Fatalf("reason = %q", reason)
	}

	// The flag survives a restart: save, load, assess again.
	fsys := fixtures.FS()
	if err := config.SaveState(fsys, "/state.json", st); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadState(fsys, "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(flagRules(assessFixtures(t, pkgs, loaded, nil), "visual-studio-code-bin"), risk.RuleMaintainer) {
		t.Fatal("flag lost across a restart")
	}

	// A successful upgrade (or an acknowledgement) records the current
	// maintainers and clears it.
	if !RecordMaintainers(&loaded, pkgs, true) {
		t.Error("forced recording reported no change")
	}
	if slices.Contains(flagRules(assessFixtures(t, pkgs, loaded, nil), "visual-studio-code-bin"), risk.RuleMaintainer) {
		t.Error("flag survived the upgrade")
	}
}

func TestLoadAURReview(t *testing.T) {
	pkgs := fixtureAUR(t)
	var vscode aur.Package
	for _, p := range pkgs {
		if p.Name == "visual-studio-code-bin" {
			vscode = p
		}
	}
	rev, err := LoadAURReview(context.Background(), fixtures.Runner(), fixtures.Fetcher(), fixtureSources().Location, vscode, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !rev.HasBase || rev.Base.Title != "Update to 1.105.0" || len(rev.Findings) != 1 {
		t.Fatalf("review = %+v", rev)
	}
	res := assessFixtures(t, pkgs, config.State{}, map[string]aur.Review{vscode.Name: rev})
	var found bool
	for _, p := range res.Packages {
		for _, f := range p.Flags {
			if p.Name == vscode.Name && f.Rule == risk.RuleSuspicious && strings.Contains(f.Reason, "host of a source URL") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("no suspicious flag: %v", flagRules(res, vscode.Name))
	}
}

func TestGatherReviewModes(t *testing.T) {
	src := fixtureSources()
	lazy := Gather(context.Background(), src)
	for _, p := range lazy.Flagged {
		for _, fl := range p.Flags {
			if fl.Rule == risk.RuleSuspicious {
				t.Fatalf("lazy mode loaded a diff: %+v", p)
			}
		}
	}

	// Eager mode fetches the diffs and flags the suspicious one.
	fetcher := fixtures.Fetcher()
	src = fixtureSources()
	src.Fetcher = fetcher
	src.Config.AURReview = config.AURReviewEager
	eager := Gather(context.Background(), src)
	if len(eager.Errors) > 0 {
		t.Fatalf("errors: %v", eager.Errors)
	}
	var found bool
	for _, p := range eager.Flagged {
		for _, fl := range p.Flags {
			found = found || (p.Name == "visual-studio-code-bin" && fl.Rule == risk.RuleSuspicious)
		}
	}
	if !found {
		t.Errorf("eager mode flagged nothing suspicious: %+v", eager.Flagged)
	}

	// Lazy mode makes no diff requests at all.
	fetcher = fixtures.Fetcher()
	src = fixtureSources()
	src.Fetcher = fetcher
	Gather(context.Background(), src)
	for _, c := range fetcher.Calls() {
		if strings.Contains(c.URL, "cgit") {
			t.Errorf("lazy mode fetched %s", c.URL)
		}
	}
}
