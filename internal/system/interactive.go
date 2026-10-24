package system

import "os/exec"

// InteractiveCommand returns a command meant to take over the terminal, for
// tea.ExecProcess: its stdin, stdout and stderr are left unset so Bubble
// Tea hands it the terminal. It deliberately has no context: cancelling an
// upgrade halfway through a pacman transaction would be worse than letting
// it finish. argv must not be empty.
func InteractiveCommand(argv []string) *exec.Cmd {
	return exec.Command(argv[0], argv[1:]...)
}
