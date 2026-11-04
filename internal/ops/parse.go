package ops

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// Printed is one line of --print-format '%n %v'.
type Printed struct {
	Name, Version string
}

// ParsePrint parses the dry run's stdout. A line that isn't "name version"
// is an error, so unexpected output never looks like an empty plan.
func ParsePrint(data []byte) ([]Printed, error) {
	var out []Printed
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 || !ValidName(f[0]) {
			return nil, fmt.Errorf("unexpected line in pacman's dry run: %q", line)
		}
		out = append(out, Printed{Name: f[0], Version: f[1]})
	}
	return out, nil
}

// Problem is one thing pacman said stops the transaction.
type Problem struct {
	// Removing is the package whose removal breaks something, Dependency
	// the dependency string it provides, and RequiredBy the installed
	// package that needs it. Either may be empty when pacman omits it.
	Removing, Dependency, RequiredBy string
	// Text is the line as pacman printed it.
	Text string
}

// breaksPatterns are the spellings of the "breaks dependency" line. Needs
// checking on Arch: which one the current pacman prints.
var breaksPatterns = []*regexp.Regexp{
	// :: removing X breaks dependency 'Y' required by Z
	regexp.MustCompile(`^(?:::\s*)?removing (\S+) breaks dependency '([^']+)' required by (\S+)$`),
	// :: Z: removing X breaks dependency 'Y'
	regexp.MustCompile(`^(?:::\s*)?(\S+): removing (\S+) breaks dependency '([^']+)'$`),
}

// installBreaks is the spelling of a downgrade (or any install) that breaks
// a dependency: ":: installing X (1.0-1) breaks dependency 'Y' required by Z".
// Needs checking on Arch: the exact wording.
var installBreaks = regexp.MustCompile(`^(?:::\s*)?installing (\S+) \(([^)]+)\) breaks dependency '([^']+)' required by (\S+)$`)

// Errors is what pacman's stderr said.
type Errors struct {
	// Problems are the dependency breaks.
	Problems []Problem
	// Unsatisfied reports "could not satisfy dependencies".
	Unsatisfied bool
	// NotFound are targets pacman doesn't know ("target not found: X").
	NotFound []string
	// Lines are the "error:" lines, without the prefix.
	Lines []string
}

// ParseErrors reads pacman's stderr (or stdout and stderr together).
func ParseErrors(data []byte) Errors {
	var e Errors
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if rest, ok := strings.CutPrefix(line, "error: "); ok {
			e.Lines = append(e.Lines, rest)
			if strings.Contains(rest, "could not satisfy dependencies") {
				e.Unsatisfied = true
			}
			if n, ok := strings.CutPrefix(rest, "target not found: "); ok {
				e.NotFound = append(e.NotFound, n)
			}
			continue
		}
		if strings.Contains(line, "could not satisfy dependencies") {
			e.Unsatisfied = true
		}
		if m := installBreaks.FindStringSubmatch(line); m != nil {
			e.Problems = append(e.Problems, Problem{Removing: m[1], Dependency: m[3], RequiredBy: m[4], Text: line})
			continue
		}
		if m := breaksPatterns[0].FindStringSubmatch(line); m != nil {
			e.Problems = append(e.Problems, Problem{Removing: m[1], Dependency: m[2], RequiredBy: m[3], Text: line})
		} else if m := breaksPatterns[1].FindStringSubmatch(line); m != nil {
			e.Problems = append(e.Problems, Problem{Removing: m[2], Dependency: m[3], RequiredBy: m[1], Text: line})
		}
	}
	return e
}
