// Package system is the only package in cronen that touches os/exec, the
// filesystem, or the network. Everything else depends on the interfaces
// defined here so it can be tested with the fakes in this package.
package system

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Runner runs external commands.
//
// Run returns the command's stdout and its exit code. The error contract is:
//
//   - exit code 0: err is nil.
//   - non-zero exit code: err is an *ExitError carrying the code and the
//     command's stderr. Some commands use non-zero codes for normal results
//     (checkupdates exits 2 when there are no updates), so callers should
//     check the code before treating err as a failure.
//   - the command could not be started, or ctx was cancelled: the code is -1
//     and err describes why. If the command isn't installed, err matches
//     ErrCommandNotFound.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, int, error)
}

// ErrCommandNotFound reports a command that isn't installed. Runners wrap
// it into the error they return, so callers can test with errors.Is.
var ErrCommandNotFound = errors.New("command not found")

// ExitError reports a command that ran but exited with a non-zero code.
type ExitError struct {
	Command string
	Code    int
	Stderr  []byte
}

func (e *ExitError) Error() string {
	msg := strings.TrimSpace(string(e.Stderr))
	if msg == "" {
		return fmt.Sprintf("%s: exit status %d", e.Command, e.Code)
	}
	return fmt.Sprintf("%s: exit status %d: %s", e.Command, e.Code, msg)
}

// ExecRunner is the real Runner, backed by os/exec.
type ExecRunner struct{}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, int, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), 0, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return stdout.Bytes(), -1, fmt.Errorf("%s: %w", commandLine(name, args), ctxErr)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() >= 0 {
		return stdout.Bytes(), exitErr.ExitCode(), &ExitError{
			Command: commandLine(name, args),
			Code:    exitErr.ExitCode(),
			Stderr:  stderr.Bytes(),
		}
	}
	if errors.Is(err, exec.ErrNotFound) {
		return stdout.Bytes(), -1, fmt.Errorf("%s: %w: %w", commandLine(name, args), ErrCommandNotFound, err)
	}
	return stdout.Bytes(), -1, fmt.Errorf("%s: %w", commandLine(name, args), err)
}

func commandLine(name string, args []string) string {
	return strings.Join(append([]string{name}, args...), " ")
}
