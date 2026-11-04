package ops

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/leNITHISH/cronen/internal/pacman"
)

// checkPackageFiles refuses an empty list and any path that may not be given
// to `pacman -U` (safety invariant 3): see pacman.CheckCachePath.
func checkPackageFiles(files, cacheDirs []string) error {
	if len(files) == 0 {
		return errors.New("no package files selected")
	}
	for _, f := range files {
		if err := pacman.CheckCachePath(f, cacheDirs); err != nil {
			return err
		}
	}
	return nil
}

// DowngradeDryArgv is the dry run, run as the user:
// pacman -Up --print-format '%n %v' -- <files>.
func DowngradeDryArgv(files, cacheDirs []string) ([]string, error) {
	if err := checkPackageFiles(files, cacheDirs); err != nil {
		return nil, err
	}
	argv := []string{"pacman", "-Up", "--print-format", "%n %v", "--"}
	return append(argv, files...), nil
}

// DowngradeArgv is the hand-off: <privilege> pacman -U -- <files>.
func DowngradeArgv(priv, files, cacheDirs []string) ([]string, error) {
	if err := checkPackageFiles(files, cacheDirs); err != nil {
		return nil, err
	}
	if len(priv) == 0 {
		return nil, errors.New("no privilege command")
	}
	argv := slices.Clone(priv)
	argv = append(argv, "pacman", "-U", "--")
	return append(argv, files...), nil
}

// DowngradeTarget is one package file to install over the installed
// version.
type DowngradeTarget struct {
	Name string
	// From is the installed version, Version the one in the file.
	From, Version string
	// Path is the package file.
	Path string
	// HasSig reports a .sig file beside it.
	HasSig bool
}

// DowngradeInput is what BuildDowngradePlan needs.
type DowngradeInput struct {
	Targets   []DowngradeTarget
	Priv      []string
	Protected []string // globs
	CacheDirs []string
	DB        *pacman.LocalDB
	// Out and Stderr are the dry run's stdout and stderr; Failed reports
	// that it exited non-zero or could not run, with RunErr saying why.
	Out, Stderr []byte
	Failed      bool
	RunErr      error
}

// BuildDowngradePlan turns a finished `pacman -Up` dry run into the modal's
// plan. Paths are validated first: a refused one is an error and no plan is
// built.
func BuildDowngradePlan(in DowngradeInput) (Plan, error) {
	files := make([]string, len(in.Targets))
	names := make([]string, len(in.Targets))
	for i, t := range in.Targets {
		files[i], names[i] = t.Path, t.Name
		if !ValidName(t.Name) {
			return Plan{}, fmt.Errorf("refusing package name %q", t.Name)
		}
	}
	dry, err := DowngradeDryArgv(files, in.CacheDirs)
	if err != nil {
		return Plan{}, err
	}
	cmd, err := DowngradeArgv(in.Priv, files, in.CacheDirs)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Kind: KindDowngrade, Selected: names, DryRun: dry, Command: cmd, Files: files}

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
			p.Warnings = append(p.Warnings, pr.Text)
		}
		return p, nil
	}

	printed, err := ParsePrint(in.Out)
	if err != nil {
		p.Failure = err.Error()
		return p, nil
	}
	if len(printed) == 0 {
		p.Failure = "pacman's dry run listed nothing to install"
		return p, nil
	}
	byName := map[string]DowngradeTarget{}
	for _, t := range in.Targets {
		byName[t.Name] = t
	}
	listed := map[string]bool{}
	for _, pr := range printed {
		t, selected := byName[pr.Name]
		it := Item{Name: pr.Name, Version: pr.Version, From: t.From, Selected: selected, Protected: IsProtected(pr.Name, in.Protected)}
		listed[pr.Name] = true
		p.Items = append(p.Items, it)
		if it.Protected {
			p.Protected = append(p.Protected, it.Name)
		}
	}
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

	for _, t := range in.Targets {
		if !listed[t.Name] {
			p.Warnings = append(p.Warnings, t.Name+" is not in pacman's list; the file may hold another package")
		}
		if !t.HasSig {
			p.Unsigned = append(p.Unsigned, filepath.Base(t.Path))
		}
	}
	if len(p.Unsigned) > 0 {
		p.Warnings = append(p.Warnings, "no .sig file beside "+strings.Join(p.Unsigned, ", ")+": its signature can't be checked (pacman's LocalFileSigLevel decides whether that is allowed)")
	}
	if len(p.Protected) > 0 {
		p.Warnings = append(p.Warnings, "protected: "+strings.Join(p.Protected, ", ")+" (base system; a bad version can leave the system unbootable)")
	}
	if in.DB != nil {
		for _, t := range in.Targets {
			lp, ok := in.DB.Lookup(t.Name)
			if !ok {
				p.Warnings = append(p.Warnings, t.Name+" is not installed, so this installs it rather than downgrading it")
				continue
			}
			var others []string
			for _, r := range lp.RequiredBy {
				if !slices.Contains(names, r) {
					others = append(others, r)
				}
			}
			if len(others) > 0 {
				p.Warnings = append(p.Warnings, "required by "+strings.Join(others, ", ")+": check they still work with "+t.Name+" "+t.Version)
			}
		}
	}
	if len(p.Items) > len(in.Targets) {
		p.Warnings = append(p.Warnings, plural(len(p.Items)-len(in.Targets), "package is", "packages are")+" pulled in by pacman")
	}
	p.Warnings = append(p.Warnings,
		"the next -Syu upgrades "+strings.Join(names, ", ")+" again unless you add it to IgnorePkg in pacman.conf; cronen won't do that")
	return p, nil
}
