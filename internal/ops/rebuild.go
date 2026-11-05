package ops

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
)

// helperName is what an AUR helper command may look like: a bare program
// name, never a path or anything a shell would read.
var helperName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// RebuildArgv is the hand-off that rebuilds foreign packages whose
// libraries are gone: <aur helper> -S -- <names>. Names are validated and
// follow "--".
func RebuildArgv(helper string, names []string) ([]string, error) {
	if helper == "" {
		return nil, errors.New("no AUR helper configured (aur_helper)")
	}
	if !helperName.MatchString(helper) {
		return nil, fmt.Errorf("refusing command %q", helper)
	}
	if err := CheckNames(names); err != nil {
		return nil, err
	}
	argv := []string{helper, "-S", "--"}
	return append(argv, names...), nil
}

// RebuildPlan is the plan of the rebuild hand-off. An AUR helper has no
// dry run of the kind pacman's --print gives, so, as with pacdiff, the modal
// shows the command and the packages and nothing runs before the user
// confirms.
func RebuildPlan(helper string, names []string) (Plan, error) {
	argv, err := RebuildArgv(helper, names)
	if err != nil {
		return Plan{}, err
	}
	return Plan{Kind: KindRebuild, Selected: slices.Clone(names), Command: argv}, nil
}
