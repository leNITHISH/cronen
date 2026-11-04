// Package ops builds and checks the argument vectors of cronen's write
// actions, parses pacman's dry-run output and errors, and assembles the
// plan the confirm modal shows.
//
// It is pure: it takes parsed data, never touches the system, and never
// builds a shell string. Every command is an argv slice, package names are
// validated before they reach one, and "--" always precedes the names.
package ops
