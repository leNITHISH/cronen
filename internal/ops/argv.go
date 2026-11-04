package ops

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

// namePattern is what a package name must match before cronen passes it to
// pacman (safety invariant 3).
var namePattern = regexp.MustCompile(`^[a-z0-9@._+][a-z0-9@._+-]*$`)

// ValidName reports whether name is a package name cronen will use.
func ValidName(name string) bool {
	return namePattern.MatchString(name)
}

// CheckNames refuses an empty list and any invalid name. It never tries to
// escape a name: it refuses.
func CheckNames(names []string) error {
	if len(names) == 0 {
		return errors.New("no packages selected")
	}
	for _, n := range names {
		if !ValidName(n) {
			return fmt.Errorf("refusing package name %q", n)
		}
	}
	return nil
}

// RemoveOptions are the toggles of the removal modal. There is no cascade
// option (-c): it does not exist.
type RemoveOptions struct {
	// Recursive is -s: also remove dependencies nothing else needs.
	Recursive bool
	// NoSave is -n: don't keep .pacsave copies of modified config files.
	NoSave bool
}

// DefaultRemoveOptions is -Rns.
func DefaultRemoveOptions() RemoveOptions {
	return RemoveOptions{Recursive: true, NoSave: true}
}

// Flags are the option letters after -R, for example "ns".
func (o RemoveOptions) Flags() string {
	var f string
	if o.NoSave {
		f += "n"
	}
	if o.Recursive {
		f += "s"
	}
	return f
}

// RemoveDryArgv is the dry run, run as the user:
// pacman -R<flags>p --print-format '%n %v' -- <names>.
func RemoveDryArgv(o RemoveOptions, names []string) ([]string, error) {
	if err := CheckNames(names); err != nil {
		return nil, err
	}
	argv := []string{"pacman", "-R" + o.Flags() + "p", "--print-format", "%n %v", "--"}
	return append(argv, names...), nil
}

// RemoveArgv is the hand-off: <privilege> pacman -R<flags> -- <names>.
func RemoveArgv(priv []string, o RemoveOptions, names []string) ([]string, error) {
	if err := CheckNames(names); err != nil {
		return nil, err
	}
	if len(priv) == 0 {
		return nil, errors.New("no privilege command")
	}
	argv := slices.Clone(priv)
	argv = append(argv, "pacman", "-R"+o.Flags(), "--")
	return append(argv, names...), nil
}

// IsProtected reports whether name matches one of the protected globs.
func IsProtected(name string, globs []string) bool {
	for _, g := range globs {
		if ok, err := path.Match(g, name); err == nil && ok {
			return true
		}
	}
	return false
}

// ShellMeta reports whether s holds a character that has no business in a
// command cronen builds; used as a belt-and-braces check on hand-off argv.
func ShellMeta(s string) bool {
	return strings.ContainsAny(s, "\x00\n")
}
