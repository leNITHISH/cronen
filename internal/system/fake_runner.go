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

// MustStdout returns the stdout registered for a command line, or nil.
func (f *FakeRunner) MustStdout(commandLine string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.results[commandLine].Stdout...)
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

	// The Runner is for reading. A privilege command is only ever run through
	// the hand-off, except for the one opt-in read `sudo -n timeshift --list`.
	if (name == "sudo" || name == "doas" || name == "run0") && line != "sudo -n timeshift --list" {
		f.mu.Lock()
		f.calls = append(f.calls, line)
		f.mu.Unlock()
		return nil, -1, fmt.Errorf("fake runner: %q is a privileged command and the read-only Runner never runs one", line)
	}
	if msg := InvalidCall(name, args); msg != "" {
		f.mu.Lock()
		f.calls = append(f.calls, line)
		f.mu.Unlock()
		return nil, 1, &ExitError{Command: line, Code: 1, Stderr: []byte("error: " + msg + "\n")}
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

// invalidPacmanCall checks a pacman call, or a `fakeroot -- pacman ...` call
// (which is how cronen syncs a temporary database as a normal user). Without
// fakeroot, pacman refuses the operations that need root, as real pacman
// does when run by a normal user.
func invalidPacmanCall(name string, args []string) string {
	if name == "fakeroot" {
		rest := args
		if len(rest) > 0 && rest[0] == "--" {
			rest = rest[1:]
		}
		if len(rest) == 0 || rest[0] != "pacman" {
			return ""
		}
		return invalidPacman(rest[1:], true)
	}
	return invalidPacman(args, false)
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
//     (and the like) are invalid with it;
//   - -S with -y (a database refresh) or -c (clean) needs root, even with
//     -p: "you cannot perform this operation unless you are root". Run as a
//     normal user it fails; under fakeroot it works. That is why the
//     install helper only ever refreshes a temporary database, through
//     fakeroot, and why a plain `pacman -Sy` can't be produced by a test;
//   - -p and -w can't be used together ("--print" and "--downloadonly");
//   - only the sync options cronen knows (-y -u -p -w -d -v -s -c -i -l -g
//     -q) are accepted after -S, so a typo in an argv builder shows up.
func invalidPacman(args []string, fakeroot bool) string {
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
		case 'S':
			// The cluster after S holds the sync options. A cluster of its
			// own (-y, -u) counts too.
			has := func(c string) bool { return strings.Contains(cluster, c) }
			if (has("y") || has("c")) && !fakeroot {
				return "you cannot perform this operation unless you are root."
			}
			if has("p") && has("w") {
				return "invalid option: '--print' and '--downloadonly' may not be used together"
			}
			for _, c := range []byte(strings.TrimPrefix(cluster, "S")) {
				if !strings.ContainsRune("yupwdvscilgq", rune(c)) && c != 'S' {
					return "invalid option: '-" + string(c) + "' is not valid with -S"
				}
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
