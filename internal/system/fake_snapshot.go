package system

import (
	"slices"
	"strings"
)

// InvalidCall reports why a real tool would refuse the command, or "" when
// the fakes have no objection. It covers pacman and fakeroot (see
// invalidPacman) and the snapshot tools, so a test fails on a flag
// combination the real tool rejects, wherever the command came from: the
// FakeRunner applies it to every Run, and tests of the hand-off apply it to
// the argv they are given.
//
// The snapshot rules are cronen's model of the tools' command lines, from
// their documentation, not captured from a real system:
//
//   - timeshift: one operation (--list, --create, --delete, --restore,
//     --check, --version) at a time; --comments belongs to --create and
//     --snapshot to --delete and --restore; unknown options are refused; it
//     needs root for everything but --version and --help;
//   - snapper: the global -c/--config comes before the command and takes a
//     value; list takes none of create's options; create's cleanup
//     algorithm is one of number, timeline or empty-pre-post; delete needs
//     snapshot numbers; rollback and the rest are unknown to cronen;
//   - a privilege command (sudo, doas, run0) wraps one of the above. The
//     read-only Runner may only be given `sudo -n timeshift --list`: it is
//     the single privileged read cronen allows itself.
func InvalidCall(name string, args []string) string {
	switch name {
	case "pacman", "fakeroot":
		return invalidPacmanCall(name, args)
	case "timeshift":
		return invalidTimeshift(args, false)
	case "snapper":
		return invalidSnapper(args)
	case "sudo", "doas", "run0":
		return invalidPrivileged(name, args)
	}
	return ""
}

// invalidPrivileged checks a command wrapped in a privilege command, as the
// hand-off gives it to the terminal.
func invalidPrivileged(name string, args []string) string {
	rest := args
	if name == "sudo" && len(rest) > 0 && rest[0] == "-n" {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return name + ": a command is required"
	}
	switch rest[0] {
	case "timeshift":
		return invalidTimeshift(rest[1:], true)
	case "snapper":
		return invalidSnapper(rest[1:])
	case "pacman":
		return invalidPacman(rest[1:], true)
	}
	return ""
}

var timeshiftOps = []string{"--list", "--create", "--delete", "--restore", "--check", "--version", "--help", "--delete-all"}

func invalidTimeshift(args []string, root bool) string {
	var ops []string
	have := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case slices.Contains(timeshiftOps, a):
			ops = append(ops, a)
		case a == "--comments" || a == "--snapshot" || a == "--tags":
			if i+1 >= len(args) {
				return "Option " + a + " requires an argument"
			}
			have[a] = true
			i++
		case a == "--yes" || a == "--scripted" || a == "--skip-grub" || a == "--btrfs" || a == "--rsync":
		default:
			return "Unknown option: " + a
		}
	}
	if len(ops) != 1 {
		return "timeshift: exactly one operation is required"
	}
	op := ops[0]
	if have["--comments"] && op != "--create" {
		return "Option --comments is only valid with --create"
	}
	if have["--snapshot"] && op != "--delete" && op != "--restore" {
		return "Option --snapshot is only valid with --delete and --restore"
	}
	if (op == "--delete" || op == "--restore") && !have["--snapshot"] {
		return "timeshift: --snapshot is required (cronen never lets Timeshift ask which one)"
	}
	if !root && op != "--version" && op != "--help" {
		return "You need to run this command as root"
	}
	return ""
}

func invalidSnapper(args []string) string {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		switch args[i] {
		case "-c", "--config":
			if i+1 >= len(args) {
				return "option '" + args[i] + "' requires an argument"
			}
			i += 2
		case "--version", "--help", "-h", "-q", "--quiet", "-v", "--verbose":
			i++
		default:
			return "unknown option '" + args[i] + "'"
		}
	}
	if i >= len(args) {
		for _, a := range args {
			if a == "--version" || a == "--help" {
				return ""
			}
		}
		return "No command provided"
	}
	cmd, rest := args[i], args[i+1:]
	switch cmd {
	case "list":
		for _, a := range rest {
			if strings.HasPrefix(a, "-") && a != "--disable-used-space" && a != "-t" && a != "--type" {
				return "unknown option '" + a + "' for command 'list'"
			}
		}
	case "create":
		for j := 0; j < len(rest); j++ {
			switch rest[j] {
			case "-d", "--description", "-u", "--userdata", "-t", "--type", "--command":
				if j+1 >= len(rest) {
					return "option '" + rest[j] + "' requires an argument"
				}
				j++
			case "-c", "--cleanup-algorithm":
				if j+1 >= len(rest) {
					return "option '" + rest[j] + "' requires an argument"
				}
				j++
				if !slices.Contains([]string{"number", "timeline", "empty-pre-post"}, rest[j]) {
					return "Unknown cleanup algorithm '" + rest[j] + "'"
				}
			case "-p", "--print-number", "--read-only", "--from":
			default:
				return "unknown option '" + rest[j] + "' for command 'create'"
			}
		}
	case "delete":
		n := 0
		for _, a := range rest {
			if strings.HasPrefix(a, "-") {
				if a != "--sync" && a != "-s" {
					return "unknown option '" + a + "' for command 'delete'"
				}
				continue
			}
			n++
		}
		if n == 0 {
			return "Command 'delete' needs at least one argument."
		}
	case "rollback", "modify", "get-config", "status", "diff", "undochange", "cleanup":
	default:
		return "Unknown command '" + cmd + "'"
	}
	return ""
}
