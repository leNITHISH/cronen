package ops

import (
	"fmt"
	"slices"
	"strings"

	"github.com/leNITHISH/cronen/internal/pacman"
)

// InstallOption is which of the install helper's two options a plan is for.
type InstallOption int

// The install helper's options. There is no third.
const (
	// OptionUpgrade (A) is the full upgrade plus the package.
	OptionUpgrade InstallOption = iota
	// OptionArchive (B) installs the package from the archive snapshot of
	// the last full upgrade.
	OptionArchive
)

// InstallItem is one package an install transaction would bring in.
type InstallItem struct {
	Name, Version, Repo string
	// Size is the bytes to download, 0 when already in the cache.
	Size int64
	// Requested is true for the packages the user asked for.
	Requested bool
}

// InstallPlan is what the install modal shows about one option.
type InstallPlan struct {
	Option InstallOption
	// Names are the repo packages asked for, AUR the ones only an AUR
	// helper can build (option A with aur_helper set).
	Names, AUR []string
	// DryRun and Command are the dry run and hand-off command lines.
	DryRun, Command []string
	Items           []InstallItem
	// Download is the whole transaction's download, Packages how many
	// packages it holds.
	Download int64
	Packages int
	Problems []Problem
	Warnings []string
	// Failure is why the option can't proceed; a plan with one offers no
	// way to run.
	Failure string
}

// Blocked reports that the option can't proceed.
func (p InstallPlan) Blocked() bool { return p.Failure != "" }

// InstallPlanInput is what BuildInstallPlan needs.
type InstallPlanInput struct {
	Option     InstallOption
	Names, AUR []string
	DryRun     []string
	Command    []string
	Guard      Guard
	// Out and Stderr are the dry run's output; Failed reports it exited
	// non-zero or could not run, with RunErr saying why.
	Out, Stderr []byte
	Failed      bool
	RunErr      error
}

// BuildInstallPlan turns a finished dry run into the option's plan. Every
// command is checked by the guard first, and a failed dry run (or a failed
// check) leaves a plan with a Failure and nothing to run.
func BuildInstallPlan(in InstallPlanInput) InstallPlan {
	p := InstallPlan{Option: in.Option, Names: slices.Clone(in.Names), AUR: slices.Clone(in.AUR), DryRun: in.DryRun, Command: in.Command}
	block := func(msg string) InstallPlan {
		p.Failure, p.Command = msg, nil
		return p
	}
	for _, argv := range [][]string{in.DryRun, in.Command} {
		if err := in.Guard.Verify(argv); err != nil {
			return block(err.Error())
		}
	}
	if in.Failed {
		e := ParseErrors(append(slices.Clone(in.Stderr), in.Out...))
		p.Problems = e.Problems
		for _, pr := range e.Problems {
			p.Warnings = append(p.Warnings, pr.Text)
		}
		switch {
		case len(e.Lines) > 0:
			return block(strings.Join(e.Lines, "; "))
		case in.RunErr != nil:
			return block(in.RunErr.Error())
		}
		return block("pacman's dry run failed")
	}
	printed, err := pacman.ParsePrint(in.Out)
	if err != nil {
		return block(err.Error())
	}
	if len(printed) == 0 && len(in.Names) > 0 {
		return block("pacman's dry run listed nothing to install")
	}
	asked := map[string]bool{}
	for _, n := range in.Names {
		asked[n] = true
	}
	seen := map[string]bool{}
	for _, pr := range printed {
		seen[pr.Name] = true
		p.Items = append(p.Items, InstallItem{Name: pr.Name, Version: pr.Version, Repo: pr.Repo, Size: pr.DownloadSize, Requested: asked[pr.Name]})
		p.Download += pr.DownloadSize
	}
	p.Packages = len(printed)
	slices.SortStableFunc(p.Items, func(a, b InstallItem) int {
		if a.Requested != b.Requested {
			if a.Requested {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	for _, n := range in.Names {
		if !seen[n] {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s is not in pacman's list", n))
		}
	}
	return p
}
