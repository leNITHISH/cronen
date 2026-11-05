package ops

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildInstallPlan(t *testing.T) {
	dry, _ := UpgradeDryArgv(testChkDB, []string{"htop"})
	cmd, _ := UpgradeInstallArgv([]string{"sudo"}, "", []string{"htop"})
	base := InstallPlanInput{Option: OptionUpgrade, Names: []string{"htop"}, DryRun: dry, Command: cmd, Guard: testGuard}

	in := base
	in.Out = []byte("zstd 1.5.7-3 core 0\nhtop 3.4.1-1 extra 1000\nlibnl 3.11.0-1 core 500\n")
	p := BuildInstallPlan(in)
	if p.Blocked() || p.Packages != 3 || p.Download != 1500 || p.Items[0].Name != "htop" || !p.Items[0].Requested || p.Items[1].Requested || line(p.Command) != "sudo pacman -Syu -- htop" {
		t.Fatalf("got %+v", p)
	}

	// Missing from pacman's list: a warning, not a failure.
	in.Out = []byte("zstd 1.5.7-3 core 0\n")
	if p := BuildInstallPlan(in); p.Blocked() || len(p.Warnings) != 1 {
		t.Fatalf("got %+v", p)
	}

	for name, mod := range map[string]func(*InstallPlanInput){
		"empty output":   func(i *InstallPlanInput) { i.Out = nil },
		"garbage output": func(i *InstallPlanInput) { i.Out = []byte("nonsense\n") },
		"dry run failed": func(i *InstallPlanInput) {
			i.Failed, i.Stderr = true, []byte("error: failed to prepare transaction (could not satisfy dependencies)\n:: installing a (2-1) breaks dependency 'b' required by c\n")
		},
		"could not run":  func(i *InstallPlanInput) { i.Failed, i.RunErr = true, errors.New("boom") },
		"failed, silent": func(i *InstallPlanInput) { i.Failed = true },
		"bad command": func(i *InstallPlanInput) {
			i.Out = []byte("htop 1-1 extra 1\n")
			i.Command = []string{"sudo", "pacman", "-Sy", "--", "htop"}
		},
		"bad dry run": func(i *InstallPlanInput) {
			i.Out = []byte("htop 1-1 extra 1\n")
			i.DryRun = []string{"pacman", "-Sp", "--", "htop"}
		},
	} {
		i := base
		mod(&i)
		p := BuildInstallPlan(i)
		if !p.Blocked() || p.Command != nil {
			t.Errorf("%s: not blocked: %+v", name, p)
		}
	}
	i := base
	i.Failed, i.Stderr = true, []byte("error: failed to prepare transaction (could not satisfy dependencies)\n:: installing a (2-1) breaks dependency 'b' required by c\n")
	if p := BuildInstallPlan(i); len(p.Problems) != 1 || !strings.Contains(p.Failure, "could not satisfy") {
		t.Errorf("problems: %+v", p)
	}

	// AUR-only: the dry run is the plain preview, and an empty list is fine.
	dry0, _ := UpgradeDryArgv(testChkDB, nil)
	cmd0, _ := UpgradeInstallArgv(nil, "paru", []string{"visual-studio-code-bin"})
	if p := BuildInstallPlan(InstallPlanInput{Option: OptionUpgrade, AUR: []string{"visual-studio-code-bin"}, DryRun: dry0, Command: cmd0, Guard: testGuard}); p.Blocked() {
		t.Errorf("aur only: %+v", p)
	}
}
