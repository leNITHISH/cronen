package linkage

import (
	"fmt"
	"slices"
	"strings"

	"github.com/leNITHISH/cronen/internal/ops"
)

// FixKind says what a suggested fix is.
type FixKind int

// The fixes Suggest offers.
const (
	// FixUpgrade: the full upgrade, for broken repo packages.
	FixUpgrade FixKind = iota + 1
	// FixReinstall: reinstall a repo package from the cache at the installed
	// version.
	FixReinstall
	// FixRebuild: rebuild foreign packages with the AUR helper.
	FixRebuild
	// FixUpgradeFirst: foreign packages need a rebuild, but repo updates are
	// pending, so the rebuild would only break again.
	FixUpgradeFirst
)

// Fix is one suggestion.
type Fix struct {
	Kind     FixKind
	Packages []string
	// Argv is the command to hand off (FixRebuild) or to show
	// (FixReinstall); nil when the fix is another key or can't be built.
	Argv []string
	// Text is the suggestion in words.
	Text string
}

// FixInput is what Suggest needs.
type FixInput struct {
	Broken []Broken
	// Foreign are the installed foreign packages.
	Foreign map[string]bool
	// PendingRepo counts the pending repo updates. Any makes a rebuild
	// pointless.
	PendingRepo int
	// Cached maps a repo package to its package file at the installed
	// version, when the cache holds it.
	Cached map[string]string
	// CacheDirs are the directories the files may come from.
	CacheDirs []string
	Priv      []string
	AURHelper string
}

// Suggest turns the broken files into fixes:
//
//   - a broken repo package: the full upgrade, or, when the package file at
//     the installed version is still cached, a reinstall from it;
//   - a broken foreign package: a rebuild with the AUR helper, but only
//     when no repo updates are pending. Otherwise "upgrade first, then
//     rebuild": rebuilding against the old libraries would break again.
func Suggest(in FixInput) []Fix {
	var repo, foreign []string
	for _, b := range in.Broken {
		list := &repo
		if in.Foreign[b.Package] {
			list = &foreign
		}
		if !slices.Contains(*list, b.Package) {
			*list = append(*list, b.Package)
		}
	}
	slices.Sort(repo)
	slices.Sort(foreign)

	var fixes []Fix
	if len(repo) > 0 {
		fixes = append(fixes, Fix{
			Kind: FixUpgrade, Packages: repo,
			Text: "upgrade the system (U) to bring " + strings.Join(repo, ", ") + " and their libraries up to date",
		})
		for _, n := range repo {
			file, ok := in.Cached[n]
			if !ok {
				continue
			}
			argv, err := ops.DowngradeArgv(in.Priv, []string{file}, in.CacheDirs)
			if err != nil {
				continue
			}
			fixes = append(fixes, Fix{
				Kind: FixReinstall, Packages: []string{n}, Argv: argv,
				Text: fmt.Sprintf("or reinstall %s from the cache at the installed version: %s", n, strings.Join(argv, " ")),
			})
		}
	}
	if len(foreign) > 0 {
		switch {
		case in.PendingRepo > 0:
			fixes = append(fixes, Fix{
				Kind: FixUpgradeFirst, Packages: foreign,
				Text: fmt.Sprintf("upgrade first, then rebuild %s: %s pending, and a rebuild against the old libraries would break again",
					strings.Join(foreign, ", "), plural(in.PendingRepo, "repo update is", "repo updates are")),
			})
		default:
			argv, err := ops.RebuildArgv(in.AURHelper, foreign)
			text := "rebuild " + strings.Join(foreign, ", ")
			if err != nil {
				text += " with your AUR helper (set aur_helper to hand it off from here)"
				argv = nil
			} else {
				text += ": " + strings.Join(argv, " ")
			}
			fixes = append(fixes, Fix{Kind: FixRebuild, Packages: foreign, Argv: argv, Text: text})
		}
	}
	return fixes
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
