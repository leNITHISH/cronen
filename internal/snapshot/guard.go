package snapshot

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// Guard verifies snapshot commands before they run. It is the second line
// of defence behind the builders: whatever produced an argv, only the exact
// forms below get through. Anything else (extra flags such as --yes or
// --scripted, a shell metacharacter in a name, a newline, an option-like
// name, snapper rollback) is refused.
//
//	<priv> timeshift --create --comments <comment>
//	<priv> timeshift --delete --snapshot <name>
//	<priv> timeshift --restore --snapshot <name>
//	sudo -n timeshift --list
//	<priv> snapper -c <config> create --description <comment> --cleanup-algorithm number
//	<priv> snapper -c <config> delete <number>
//	snapper -c <config> list
//
// Programs are matched by their exact name, never by a path.
type Guard struct {
	// Config, if set, is the only snapper config allowed.
	Config string
}

// Verify returns nil only for the forms above.
func (g Guard) Verify(argv []string) error {
	refuse := func(why string) error {
		return fmt.Errorf("refusing %q: %s", strings.Join(argv, " "), why)
	}
	for _, a := range argv {
		if a == "" || strings.IndexFunc(a, unicode.IsControl) >= 0 {
			return fmt.Errorf("refusing command argument %q", a)
		}
	}
	rest := argv
	priv := false
	if len(rest) > 0 && slices.Contains(privileges, rest[0]) {
		priv, rest = true, rest[1:]
	}
	if priv && argv[0] == "sudo" && slices.Equal(rest, []string{"-n", "timeshift", "--list"}) {
		return nil
	}
	if len(rest) == 0 {
		return errors.New("refusing an empty snapshot command")
	}
	switch rest[0] {
	case "timeshift":
		if !priv {
			return refuse("timeshift needs the privilege command")
		}
		return g.timeshift(rest[1:], refuse)
	case "snapper":
		return g.snapper(rest[1:], priv, refuse)
	}
	return refuse("not a snapshot command")
}

func (g Guard) timeshift(a []string, refuse func(string) error) error {
	switch {
	case len(a) == 3 && a[0] == "--create" && a[1] == "--comments":
		if !ValidComment(a[2]) {
			return refuse("bad snapshot comment")
		}
		return nil
	case len(a) == 3 && (a[0] == "--delete" || a[0] == "--restore") && a[1] == "--snapshot":
		if !ValidName(Timeshift, a[2]) {
			return refuse("bad snapshot name")
		}
		return nil
	}
	return refuse("only --create --comments, --delete --snapshot and --restore --snapshot are allowed")
}

func (g Guard) snapper(a []string, priv bool, refuse func(string) error) error {
	if len(a) < 3 || a[0] != "-c" {
		return refuse("snapper must be called as `snapper -c <config> …`")
	}
	if !ValidConfig(a[1]) {
		return refuse("bad snapper config")
	}
	if g.Config != "" && a[1] != g.Config {
		return refuse("not the configured snapper config")
	}
	cmd, rest := a[2], a[3:]
	switch {
	case cmd == "list" && len(rest) == 0:
		if priv {
			return refuse("listing never runs with the privilege command")
		}
		return nil
	case cmd == "rollback":
		return refuse("snapper rollback is shown as a command to copy, never run")
	}
	if !priv {
		return refuse("snapper needs the privilege command for this")
	}
	switch {
	case cmd == "create" && len(rest) == 4 && rest[0] == "--description" && rest[2] == "--cleanup-algorithm" && rest[3] == "number":
		if !ValidComment(rest[1]) {
			return refuse("bad snapshot comment")
		}
		return nil
	case cmd == "delete" && len(rest) == 1:
		if !ValidName(Snapper, rest[0]) {
			return refuse("bad snapshot number")
		}
		return nil
	}
	return refuse("only create, delete and list are allowed")
}
