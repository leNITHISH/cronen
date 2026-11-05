package ops

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode"
)

// PrefetchInput is what PrefetchPlan needs.
type PrefetchInput struct {
	// Priv is the privilege command, used only when NeedsRoot is set.
	Priv []string
	// NeedsRoot reports that the current user can't write to the package
	// cache, so checkupdates -d must run as root.
	NeedsRoot bool
	// Override is prefetch_command: when set it replaces the command
	// entirely and is run exactly as written.
	Override string
	// Previewed reports that the transaction preview is available, and
	// Download is the bytes it says are still to download. The preview is
	// the prefetch's dry run: without it cronen doesn't know what the
	// hand-off would fetch.
	Previewed bool
	Download  int64
}

// shells are the interpreters a prefetch command may not start with: cronen
// builds argv slices and never a shell string.
var shells = []string{"sh", "bash", "dash", "zsh", "fish", "ksh", "csh", "tcsh"}

// PrefetchArgv builds the prefetch hand-off: `checkupdates -d`, prefixed
// with the privilege command only when needsRoot, or the override command
// as written. It checks every argument and refuses, never escapes.
func PrefetchArgv(priv []string, needsRoot bool, override string) ([]string, error) {
	var argv []string
	if fields := strings.Fields(override); len(fields) > 0 {
		argv = fields
	} else {
		argv = []string{"checkupdates", "-d"}
		if needsRoot {
			if len(priv) == 0 {
				return nil, errors.New("no privilege command")
			}
			argv = append(slices.Clone(priv), argv...)
		}
	}
	for i, a := range argv {
		if a == "" || ShellMeta(a) || strings.IndexFunc(a, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("refusing command argument %q", a)
		}
		if i < 2 && slices.Contains(shells, path.Base(a)) {
			return nil, fmt.Errorf("refusing to run a shell (%q): commands are argv, never shell strings", a)
		}
	}
	return argv, nil
}

// PrefetchPlan is the plan of the prefetch hand-off. Like every hand-off it
// comes after a preview: with no preview, or nothing left to download,
// there is no plan and nothing runs.
func PrefetchPlan(in PrefetchInput) (Plan, error) {
	switch {
	case !in.Previewed:
		return Plan{}, errors.New("transaction preview unavailable: not prefetching what cronen can't list")
	case in.Download <= 0:
		return Plan{}, errors.New("nothing to download: everything is already in the cache")
	}
	argv, err := PrefetchArgv(in.Priv, in.NeedsRoot, in.Override)
	if err != nil {
		return Plan{}, err
	}
	return Plan{Kind: KindPrefetch, Command: argv}, nil
}
