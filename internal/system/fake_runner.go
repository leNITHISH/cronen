package system

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// ErrUnexpectedCommand is returned by FakeRunner for commands it has no
// response for.
var ErrUnexpectedCommand = errors.New("fake runner: unexpected command")

// FakeResult is a canned result for one command line.
type FakeResult struct {
	Stdout []byte
	Stderr []byte
	Code   int
	// Err, if set, simulates a command that could not be started.
	Err error
}

// FakeRunner is a Runner that returns canned results keyed by the full
// command line, for example "vercmp 1.0-1 2.0-1" or "pacman -Qi foo".
// It is safe for concurrent use.
type FakeRunner struct {
	mu      sync.Mutex
	results map[string]FakeResult
	calls   []string

	// Handler, if set, answers command lines that have no exact result
	// registered with Set. It reports false for lines it doesn't know.
	Handler func(commandLine string) (FakeResult, bool)
}

// NewFakeRunner returns an empty FakeRunner.
func NewFakeRunner() *FakeRunner {
	return &FakeRunner{results: map[string]FakeResult{}}
}

// Set registers the result for a command line.
func (f *FakeRunner) Set(commandLine string, r FakeResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results[commandLine] = r
}

// Calls returns the command lines run so far, in order.
func (f *FakeRunner) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// Run implements Runner, following the same error contract as ExecRunner.
func (f *FakeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, int, error) {
	line := commandLine(name, args)

	if name == "pacman" && nosavePrint(args) {
		f.mu.Lock()
		f.calls = append(f.calls, line)
		f.mu.Unlock()
		return nil, 1, &ExitError{Command: line, Code: 1,
			Stderr: []byte("error: invalid option: '--nosave' and '--print' may not be used together\n")}
	}

	f.mu.Lock()
	f.calls = append(f.calls, line)
	r, ok := f.results[line]
	handler := f.Handler
	f.mu.Unlock()
	if !ok && handler != nil {
		r, ok = handler(line)
	}

	if err := ctx.Err(); err != nil {
		return nil, -1, fmt.Errorf("%s: %w", line, err)
	}
	if !ok {
		return nil, -1, fmt.Errorf("%w: %q", ErrUnexpectedCommand, line)
	}
	if r.Err != nil {
		return nil, -1, fmt.Errorf("%s: %w", line, r.Err)
	}
	if r.Code != 0 {
		return r.Stdout, r.Code, &ExitError{Command: line, Code: r.Code, Stderr: r.Stderr}
	}
	return r.Stdout, 0, nil
}

// nosavePrint reports an -R option cluster with both n and p, which real
// pacman refuses, so a dry run built that way fails in tests too.
func nosavePrint(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if f, ok := strings.CutPrefix(a, "-R"); ok && !strings.HasPrefix(a, "--") &&
			strings.Contains(f, "n") && strings.Contains(f, "p") {
			return true
		}
	}
	return false
}
