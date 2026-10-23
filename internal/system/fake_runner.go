package system

import (
	"context"
	"errors"
	"fmt"
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

	f.mu.Lock()
	f.calls = append(f.calls, line)
	r, ok := f.results[line]
	f.mu.Unlock()

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
