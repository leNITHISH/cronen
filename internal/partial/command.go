package partial

import (
	"path"
	"strings"
)

// Command is a pacman, paru or yay command line, as far as telling a sync
// without upgrade from a full upgrade needs it.
type Command struct {
	// Program is the base name of the program: "pacman", "paru" or "yay"
	// (or whatever was run). Privilege prefixes (sudo, doas, run0) and
	// environment assignments are skipped.
	Program string
	// Sync reports the sync operation (-S, --sync).
	Sync bool
	// Refresh counts the -y / --refresh options: 1 refreshes the databases
	// that changed, 2 (-Syy) forces a refresh of all of them.
	Refresh int
	// Upgrade reports -u / --sysupgrade.
	Upgrade bool
	// Targets are the non-option arguments, and everything after "--".
	Targets []string
}

// SyncsWithoutUpgrade reports a command that refreshes the sync databases
// and installs targets without upgrading the system: pacman -Sy X,
// -Syy X, -S -y X, -yS X and --sync --refresh X. A full upgrade
// (-Syu, -Syyu, -Suy, --sync --refresh --sysupgrade) does not match, and
// neither does -Sy with nothing to install, which changes nothing.
func (c Command) SyncsWithoutUpgrade() bool {
	return c.Sync && c.Refresh > 0 && !c.Upgrade && len(c.Targets) > 0
}

// programs are the commands that take pacman's sync options, the ones the
// pacman.log "Running" line or a user's history can show.
var programs = map[string]bool{"pacman": true, "paru": true, "yay": true}

// wrappers are prefixes that run the next word as a command.
var wrappers = map[string]bool{"sudo": true, "doas": true, "run0": true, "env": true, "fakeroot": true}

// shortWithArg are the short options that take a value: -b (--dbpath) and
// -r (--root). The value is the rest of the cluster, or the next word.
var shortWithArg = map[byte]bool{'b': true, 'r': true}

// longWithArg are the long options that take a value. A value given as the
// next word (--config /etc/pacman.conf) must not be taken for a target.
// --opt=value needs no entry.
var longWithArg = map[string]bool{
	"config": true, "dbpath": true, "root": true, "cachedir": true, "logfile": true,
	"gpgdir": true, "arch": true, "hookdir": true, "sysroot": true, "color": true,
	"ask": true, "assume-installed": true, "ignore": true, "ignoregroup": true,
	"overwrite": true, "print-format": true,
	// paru and yay options with a value.
	"aur-helper": true, "mflags": true, "makepkg": true, "pacman": true,
	"sudo": true, "chrootpkgs": true, "builddir": true, "makepkgconf": true,
	"pacmanconf": true, "editor": true, "answerclean": true, "answerdiff": true,
	"answeredit": true, "answerupgrade": true,
}

// ParseCommand parses a command line. ok is false when the line is not a
// pacman, paru or yay invocation. Quoting is not interpreted: pacman.log
// writes the arguments separated by single spaces.
func ParseCommand(line string) (c Command, ok bool) {
	words := strings.Fields(line)
	// Skip privilege prefixes and VAR=value assignments.
	for len(words) > 0 {
		w := words[0]
		switch {
		case wrappers[path.Base(w)], strings.Contains(w, "=") && !strings.HasPrefix(w, "-"):
			words = words[1:]
			continue
		}
		break
	}
	if len(words) == 0 || !programs[path.Base(words[0])] {
		return Command{}, false
	}
	c.Program = path.Base(words[0])
	words = words[1:]

	for i := 0; i < len(words); i++ {
		w := words[i]
		switch {
		case w == "--":
			c.Targets = append(c.Targets, words[i+1:]...)
			return c, true
		case strings.HasPrefix(w, "--"):
			name, _, hasValue := strings.Cut(w[2:], "=")
			switch name {
			case "sync":
				c.Sync = true
			case "refresh":
				c.Refresh++
			case "sysupgrade":
				c.Upgrade = true
			}
			if !hasValue && longWithArg[name] && i+1 < len(words) {
				i++
			}
		case len(w) > 1 && w[0] == '-':
			// A cluster of short options: -Sy, -yS, -Syu. A value-taking
			// option ends it, and takes the rest of the word or the next one.
			cluster := w[1:]
			for j := 0; j < len(cluster); j++ {
				ch := cluster[j]
				switch ch {
				case 'S':
					c.Sync = true
				case 'y':
					c.Refresh++
				case 'u':
					c.Upgrade = true
				}
				if shortWithArg[ch] {
					if j == len(cluster)-1 && i+1 < len(words) {
						i++
					}
					break
				}
			}
		default:
			c.Targets = append(c.Targets, w)
		}
	}
	return c, true
}
