package ops

import (
	"fmt"
	"slices"
	"strings"

	"github.com/leNITHISH/cronen/internal/pacman"
)

// Kind is the type of write action.
type Kind int

// Kinds of write action.
const (
	KindRemove Kind = iota
	KindOrphans
	KindPacdiff
	KindDowngrade
)

// Item is one package a removal affects.
type Item struct {
	Name    string
	Version string
	// Size is the installed size freed, 0 if unknown.
	Size int64
	// From is the installed version a downgrade replaces.
	From string
	// Selected is true for packages the user picked, false for those
	// pacman pulled in (-s).
	Selected bool
	// Protected marks packages matching the protected globs.
	Protected bool
}

// Plan is everything the confirm modal shows about one action.
type Plan struct {
	Kind    Kind
	Options RemoveOptions
	// Selected are the names the user picked.
	Selected []string
	// DryRun and Command are the dry-run and hand-off command lines.
	DryRun, Command []string
	// Items are the packages the removal affects, selected first.
	Items []Item
	// Freed is the installed size removed, in bytes.
	Freed int64
	// Pacsave are config files that become .pacsave if modified. Empty
	// with NoSave.
	Pacsave []string
	// Warnings are the lines under the package list.
	Warnings []string
	// Protected are the affected packages that match the protected globs.
	Protected []string
	// Problems are why the dry run failed, parsed from pacman's error.
	Problems []Problem
	// Failure is pacman's error text when the dry run failed. A plan with
	// a Failure offers no way to proceed.
	Failure string
	// Unsigned are the package files of a downgrade with no .sig beside
	// them.
	Unsigned []string
	// Files are the .pacnew files of a pacdiff plan.
	Files []string
}

// Blocked reports that the action can't proceed.
func (p Plan) Blocked() bool { return p.Failure != "" }

// Token is what the user must type to confirm: the protected package names
// separated by spaces, or "" when a single y is enough.
func (p Plan) Token() string {
	if p.Blocked() {
		return ""
	}
	return strings.Join(p.Protected, " ")
}

// RemoveInput is what BuildRemovePlan needs.
type RemoveInput struct {
	Kind      Kind
	Options   RemoveOptions
	Names     []string
	Priv      []string
	Protected []string // globs
	DB        *pacman.LocalDB
	// Out and Stderr are the dry run's stdout and stderr; Failed reports
	// that it exited non-zero or could not run, with RunErr saying why.
	Out, Stderr []byte
	Failed      bool
	RunErr      error
	// Backups are the backup files of the packages the dry run printed, from
	// `pacman -Qii`; BackupsErr says why they could not be read. Both are
	// unused with NoSave.
	Backups    map[string][]pacman.BackupFile
	BackupsErr error
}

// BuildRemovePlan turns a finished dry run into the modal's plan. Names are
// validated first: invalid ones are an error and no plan is built.
func BuildRemovePlan(in RemoveInput) (Plan, error) {
	dry, err := RemoveDryArgv(in.Options, in.Names)
	if err != nil {
		return Plan{}, err
	}
	cmd, err := RemoveArgv(in.Priv, in.Options, in.Names)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Kind: in.Kind, Options: in.Options, Selected: slices.Clone(in.Names), DryRun: dry, Command: cmd}

	if in.Failed {
		e := ParseErrors(append(slices.Clone(in.Stderr), in.Out...))
		p.Problems = e.Problems
		switch {
		case len(e.Lines) > 0:
			p.Failure = strings.Join(e.Lines, "; ")
		case in.RunErr != nil:
			p.Failure = in.RunErr.Error()
		default:
			p.Failure = "pacman's dry run failed"
		}
		for _, pr := range e.Problems {
			p.Warnings = append(p.Warnings, fmt.Sprintf("removing %s breaks %s, required by %s", pr.Removing, pr.Dependency, pr.RequiredBy))
		}
		return p, nil
	}

	printed, err := ParsePrint(in.Out)
	if err != nil {
		p.Failure = err.Error()
		return p, nil
	}
	if len(printed) == 0 {
		p.Failure = "pacman's dry run listed nothing to remove"
		return p, nil
	}
	chosen := map[string]bool{}
	for _, n := range in.Names {
		chosen[n] = true
	}
	removed := map[string]bool{}
	for _, pr := range printed {
		it := Item{Name: pr.Name, Version: pr.Version, Selected: chosen[pr.Name], Protected: IsProtected(pr.Name, in.Protected)}
		removed[pr.Name] = true
		if in.DB != nil {
			var lp pacman.LocalPackage
			lp, _ = in.DB.Lookup(pr.Name)
			it.Size = lp.InstalledSize
		}
		p.Freed += it.Size
		p.Items = append(p.Items, it)
		if it.Protected {
			p.Protected = append(p.Protected, it.Name)
		}
		if !in.Options.NoSave && in.BackupsErr == nil {
			p.Pacsave = append(p.Pacsave, pacman.Modified(in.Backups[pr.Name])...)
		}
	}
	if !in.Options.NoSave && in.BackupsErr != nil {
		p.Warnings = append(p.Warnings, "could not read the packages' backup files, so any .pacsave files are not listed: "+in.BackupsErr.Error())
	}
	// Selected first, each group by name.
	slices.SortStableFunc(p.Items, func(a, b Item) int {
		if a.Selected != b.Selected {
			if a.Selected {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	slices.Sort(p.Protected)

	for _, n := range in.Names {
		if !removed[n] {
			p.Warnings = append(p.Warnings, n+" is not in pacman's list; it may not be installed")
		}
	}
	if len(p.Protected) > 0 {
		p.Warnings = append(p.Warnings, "protected: "+strings.Join(p.Protected, ", ")+" (base system; removing it can leave the system unbootable)")
	}
	if in.DB != nil {
		for _, it := range p.Items {
			lp, ok := in.DB.Lookup(it.Name)
			if !ok {
				continue
			}
			var others []string
			for _, o := range lp.OptionalFor {
				if !removed[o] {
					others = append(others, o)
				}
			}
			if len(others) > 0 {
				p.Warnings = append(p.Warnings, it.Name+" is an optional dependency of "+strings.Join(others, ", "))
			}
		}
	}
	if len(p.Items) > len(in.Names) {
		p.Warnings = append(p.Warnings, plural(len(p.Items)-len(in.Names), "package is", "packages are")+" pulled in by -s")
	}
	if in.Kind == KindOrphans {
		p.Warnings = append(p.Warnings, "removing orphans can create new orphans; -s already removes those it can")
	}
	return p, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// PacdiffPlan is the plan of the pacdiff hand-off: there is no dry run, but
// the modal still shows the command and the files.
func PacdiffPlan(argv, files []string) (Plan, error) {
	if len(argv) == 0 {
		return Plan{}, fmt.Errorf("no pacdiff command")
	}
	for _, a := range argv {
		if ShellMeta(a) {
			return Plan{}, fmt.Errorf("refusing command argument %q", a)
		}
	}
	if len(files) == 0 {
		return Plan{}, fmt.Errorf("no .pacnew or .pacsave files")
	}
	return Plan{Kind: KindPacdiff, Command: slices.Clone(argv), Files: slices.Clone(files)}, nil
}
