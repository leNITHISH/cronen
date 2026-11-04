package pacman

import (
	"bytes"
	"slices"
	"strings"
	"time"
)

// Action is what a transaction did to one package.
type Action string

// The package actions pacman.log records.
const (
	ActionInstalled   Action = "installed"
	ActionUpgraded    Action = "upgraded"
	ActionDowngraded  Action = "downgraded"
	ActionReinstalled Action = "reinstalled"
	ActionRemoved     Action = "removed"
)

var actions = []Action{ActionInstalled, ActionUpgraded, ActionDowngraded, ActionReinstalled, ActionRemoved}

// Change is one "[ALPM] upgraded name (old -> new)" line.
type Change struct {
	Action Action
	Name   string
	// Old is the version before: set for upgraded, downgraded, reinstalled
	// and removed. New is the version after: set for installed, upgraded,
	// downgraded and reinstalled.
	Old, New string
}

// LogTransaction is one pacman transaction from pacman.log.
type LogTransaction struct {
	// Start is when "transaction started" was logged, End when "transaction
	// completed" was. End is zero for an interrupted transaction.
	Start, End time.Time
	// Command is the line of the "[PACMAN] Running '…'" entry that
	// preceded the transaction, without the quotes, or "" if there was
	// none (a library user, or an old log).
	Command string
	Changes []Change
	// Interrupted reports a transaction with no "transaction completed":
	// the log shows the next transaction or command (or nothing) after it.
	Interrupted bool
	// Implicit reports a transaction whose "transaction started" line is
	// missing: older logs have only the package lines and "completed".
	Implicit bool
}

// Count is how many changes have the given action.
func (t LogTransaction) Count(a Action) int {
	n := 0
	for _, c := range t.Changes {
		if c.Action == a {
			n++
		}
	}
	return n
}

// Touches reports whether the transaction changed the package called name.
func (t LogTransaction) Touches(name string) bool {
	return slices.ContainsFunc(t.Changes, func(c Change) bool { return c.Name == name })
}

// ParseHistory parses a pacman.log into its transactions, oldest first.
//
// A transaction starts at "[ALPM] transaction started" and holds the
// installed, upgraded, downgraded, reinstalled and removed lines up to
// "[ALPM] transaction completed". It is labelled with the "[PACMAN]
// Running '…'" line before it. A transaction that is followed by another
// "transaction started", by another "Running" line or by the end of the log
// has no completed line: it is Interrupted. Both timestamp formats are read
// (see parseTimestamp); zoneless ones are in loc. Lines it doesn't know,
// and lines with a timestamp it can't read, are skipped.
func ParseHistory(log []byte, loc *time.Location) []LogTransaction {
	var (
		out     []LogTransaction
		cur     *LogTransaction
		command string
	)
	closeCur := func(interrupted bool, end time.Time) {
		if cur == nil {
			return
		}
		cur.Interrupted = interrupted
		if !interrupted {
			cur.End = end
		}
		out = append(out, *cur)
		cur = nil
	}
	for line := range bytes.Lines(log) {
		ts, source, msg, ok := splitLogLine(line)
		if !ok {
			continue
		}
		t, err := parseTimestamp(ts, loc)
		if err != nil {
			continue
		}
		switch {
		case source == "PACMAN":
			if cmd, ok := runningCommand(msg); ok {
				// A new pacman process: a transaction still open was cut off.
				closeCur(true, time.Time{})
				command = cmd
			}
		case source != "ALPM":
			// ALPM-SCRIPTLET and the like belong to the open transaction
			// but say nothing about packages.
		case msg == "transaction started":
			closeCur(true, time.Time{})
			cur = &LogTransaction{Start: t, Command: command}
			command = ""
		case msg == "transaction completed":
			if cur == nil {
				// Completed with no started line: nothing to report.
				continue
			}
			closeCur(false, t)
		default:
			c, ok := parseChange(msg)
			if !ok {
				continue
			}
			if cur == nil {
				cur = &LogTransaction{Start: t, Command: command, Implicit: true}
				command = ""
			}
			cur.Changes = append(cur.Changes, c)
		}
	}
	closeCur(true, time.Time{})
	return out
}

// splitLogLine splits "[ts] [SOURCE] message" (the two bracketed fields are
// separated by a space) into its parts.
func splitLogLine(line []byte) (ts, source, msg string, ok bool) {
	s := strings.TrimRight(string(line), "\r\n")
	rest, found := strings.CutPrefix(s, "[")
	if !found {
		return "", "", "", false
	}
	ts, rest, found = strings.Cut(rest, "] ")
	if !found {
		return "", "", "", false
	}
	rest, found = strings.CutPrefix(rest, "[")
	if !found {
		return "", "", "", false
	}
	source, msg, found = strings.Cut(rest, "] ")
	if !found {
		return "", "", "", false
	}
	return ts, source, strings.TrimSpace(msg), true
}

// runningCommand extracts the command from "Running 'pacman -Syu'".
func runningCommand(msg string) (string, bool) {
	rest, ok := strings.CutPrefix(msg, "Running '")
	if !ok {
		return "", false
	}
	cmd, ok := strings.CutSuffix(rest, "'")
	if !ok {
		return "", false
	}
	return cmd, true
}

// parseChange parses "upgraded name (old -> new)", "installed name (new)"
// and the like. Names that pacman couldn't have written are refused.
func parseChange(msg string) (Change, bool) {
	verb, rest, ok := strings.Cut(msg, " ")
	if !ok {
		return Change{}, false
	}
	act := Action(verb)
	if !slices.Contains(actions, act) {
		return Change{}, false
	}
	name, vers, ok := strings.Cut(rest, " (")
	vers, closed := strings.CutSuffix(vers, ")")
	if !ok || !closed || !ValidName(name) {
		return Change{}, false
	}
	c := Change{Action: act, Name: name}
	switch act {
	case ActionUpgraded, ActionDowngraded:
		old, nw, ok := strings.Cut(vers, " -> ")
		if !ok || !validVersion(old) || !validVersion(nw) {
			return Change{}, false
		}
		c.Old, c.New = old, nw
	case ActionInstalled:
		if !validVersion(vers) {
			return Change{}, false
		}
		c.New = vers
	case ActionReinstalled:
		// "reinstalled name (ver)" in current pacman; some versions print
		// "(old -> new)".
		if old, nw, ok := strings.Cut(vers, " -> "); ok {
			c.Old, c.New = old, nw
		} else {
			c.Old, c.New = vers, vers
		}
		if !validVersion(c.Old) || !validVersion(c.New) {
			return Change{}, false
		}
	case ActionRemoved:
		if !validVersion(vers) {
			return Change{}, false
		}
		c.Old = vers
	}
	return c, true
}

func validVersion(v string) bool {
	return v != "" && !strings.ContainsAny(v, " \t()")
}

// ReverseTransactions returns a copy of txs, newest first.
func ReverseTransactions(txs []LogTransaction) []LogTransaction {
	out := slices.Clone(txs)
	slices.Reverse(out)
	return out
}
