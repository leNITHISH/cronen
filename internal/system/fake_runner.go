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

	if name == "pacman" {
		if msg := invalidPacman(args); msg != "" {
			f.mu.Lock()
			f.calls = append(f.calls, line)
			f.mu.Unlock()
			return nil, 1, &ExitError{Command: line, Code: 1, Stderr: []byte("error: " + msg + "\n")}
		}
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

// invalidPacman returns the error real pacman gives for an option
// combination it refuses, or "" for a combination it accepts or that the
// fake doesn't know about. It makes a dry run or hand-off built with a
// flag pacman rejects fail in tests too. The rules are the ones pacman
// enforces in its option parsing:
//
//   - only one operation (-D -F -Q -R -S -T -U) may be used at a time;
//   - -n (--nosave) belongs to -R and can't be combined with -p (--print);
//   - -U takes none of the sync and remove options: -y, -u, -s, -n, -c
//     (and the like) are invalid with it.
func invalidPacman(args []string) string {
	var ops []byte
	var cluster string
	for _, a := range args {
		if a == "--" {
			break
		}
		if len(a) < 2 || a[0] != '-' || a[1] == '-' {
			continue
		}
		// A short-option cluster. Letters after an operation letter are its
		// options; a following argument is never a cluster of its own here
		// because cronen passes values only to long options.
		for _, c := range []byte(a[1:]) {
			if c >= 'A' && c <= 'Z' {
				ops = append(ops, c)
			}
		}
		cluster += a[1:]
	}
	if len(ops) > 1 {
		return "only one operation may be used at a time"
	}
	if len(ops) == 1 {
		switch ops[0] {
		case 'R':
			if strings.Contains(cluster, "n") && strings.Contains(cluster, "p") {
				return "invalid option: '--nosave' and '--print' may not be used together"
			}
		case 'U':
			for _, c := range []byte(strings.TrimPrefix(cluster, "U")) {
				if !strings.ContainsRune("pdvw", rune(c)) {
					return "invalid option: '-" + string(c) + "' is not valid with -U"
				}
			}
		}
	}
	return ""
}
