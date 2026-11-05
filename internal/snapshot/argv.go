package snapshot

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// privileges are the privilege commands cronen prefixes write actions with.
var privileges = []string{"sudo", "doas", "run0"}

func checkPriv(priv []string) error {
	if len(priv) != 1 || !slices.Contains(privileges, priv[0]) {
		return fmt.Errorf("refusing privilege command %q", strings.Join(priv, " "))
	}
	return nil
}

// Step is one command of a hand-off, with what the modal calls it.
type Step struct {
	Argv  []string
	Label string
}

// CreateArgv is the snapshot taken before an upgrade:
//
//	<priv> timeshift --create --comments "<comment>"
//	<priv> snapper -c <config> create --description "<comment>" --cleanup-algorithm number
//
// The comment is a separate argv element after the flag; nothing is joined
// into a string.
func CreateArgv(t Tool, priv []string, config, comment string) ([]string, error) {
	if err := checkPriv(priv); err != nil {
		return nil, err
	}
	if !ValidComment(comment) {
		return nil, fmt.Errorf("refusing snapshot comment %q", comment)
	}
	argv := slices.Clone(priv)
	switch t {
	case Timeshift:
		return append(argv, "timeshift", "--create", "--comments", comment), nil
	case Snapper:
		if !ValidConfig(config) {
			return nil, fmt.Errorf("refusing snapper config %q", config)
		}
		return append(argv, "snapper", "-c", config, "create", "--description", comment, "--cleanup-algorithm", "number"), nil
	}
	return nil, fmt.Errorf("unknown snapshot tool %q", t)
}

// ListArgv is the read-only listing, or ok == false when the tool can't be
// listed without root and snapshot_list_sudo is off. Timeshift lists with
// `sudo -n timeshift --list` (the one read-only exception to "nothing runs
// as root without a hand-off"; -n never prompts), snapper as the user.
func ListArgv(t Tool, config string, listSudo bool) (argv []string, ok bool, err error) {
	switch t {
	case Timeshift:
		if !listSudo {
			return nil, false, nil
		}
		return []string{"sudo", "-n", "timeshift", "--list"}, true, nil
	case Snapper:
		if !ValidConfig(config) {
			return nil, false, fmt.Errorf("refusing snapper config %q", config)
		}
		return []string{"snapper", "-c", config, "list"}, true, nil
	}
	return nil, false, fmt.Errorf("unknown snapshot tool %q", t)
}

// DeleteArgv deletes one snapshot:
//
//	<priv> timeshift --delete --snapshot <name>
//	<priv> snapper -c <config> delete <number>
func DeleteArgv(t Tool, priv []string, config, name string) ([]string, error) {
	if err := checkPriv(priv); err != nil {
		return nil, err
	}
	if !ValidName(t, name) {
		return nil, fmt.Errorf("refusing snapshot name %q", name)
	}
	argv := slices.Clone(priv)
	switch t {
	case Timeshift:
		return append(argv, "timeshift", "--delete", "--snapshot", name), nil
	case Snapper:
		if !ValidConfig(config) {
			return nil, fmt.Errorf("refusing snapper config %q", config)
		}
		return append(argv, "snapper", "-c", config, "delete", name), nil
	}
	return nil, fmt.Errorf("unknown snapshot tool %q", t)
}

// RestoreArgv is the Timeshift restore hand-off:
//
//	<priv> timeshift --restore --snapshot <name>
//
// Timeshift asks its own questions after that. snapper has none: its
// rollback model depends on how the system boots, so RollbackDisplay is
// shown for the user to copy, and no argv is ever built.
func RestoreArgv(t Tool, priv []string, name string) ([]string, error) {
	if t != Timeshift {
		return nil, errors.New("only Timeshift restores are handed off")
	}
	if err := checkPriv(priv); err != nil {
		return nil, err
	}
	if !ValidName(t, name) {
		return nil, fmt.Errorf("refusing snapshot name %q", name)
	}
	return append(slices.Clone(priv), "timeshift", "--restore", "--snapshot", name), nil
}

// RollbackDisplay is the command a snapper restore shows for the user to
// copy. It is text for the screen, never an argv.
func RollbackDisplay(priv []string, config, name string) (string, error) {
	if err := checkPriv(priv); err != nil {
		return "", err
	}
	if !ValidConfig(config) || !ValidName(Snapper, name) {
		return "", fmt.Errorf("refusing snapper config %q or snapshot %q", config, name)
	}
	return strings.Join(append(slices.Clone(priv), "snapper", "-c", config, "rollback", name), " "), nil
}
