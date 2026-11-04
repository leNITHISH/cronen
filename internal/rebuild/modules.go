package rebuild

import (
	"strconv"
	"strings"
)

// Interpreter describes an updated package that other packages install
// version-specific directories for. When the package's directory token
// changes (python 3.13.7 → 3.14.0), whatever sits under the old directories
// is no longer found by the new interpreter.
type Interpreter struct {
	// Package is the interpreter's package name.
	Package string
	// Parts is how many leading version components form the token: 2 turns
	// 3.13.7 into "3.13".
	Parts int
	// Suffix is appended to the token: ".0" turns ruby 3.4.1 into "3.4.0".
	Suffix string
	// Dirs are the directories tied to a version, with %s for the token.
	Dirs []string
}

// Interpreters is the table of interpreters whose module directories are
// versioned. Add a line here to teach cronen another one.
var Interpreters = []Interpreter{
	{Package: "python", Parts: 2, Dirs: []string{"/usr/lib/python%s/"}},
	{Package: "perl", Parts: 2, Dirs: []string{"/usr/lib/perl5/%s/"}},
	{Package: "ruby", Parts: 2, Suffix: ".0", Dirs: []string{"/usr/lib/ruby/%s/", "/usr/lib/ruby/gems/%s/", "/usr/lib/ruby/vendor_ruby/%s/"}},
	{Package: "lua", Parts: 2, Dirs: []string{"/usr/lib/lua/%s/", "/usr/share/lua/%s/"}},
}

// token is the version's directory token, or "" if v has too few numeric
// components.
func (i Interpreter) token(v string) string {
	parts := strings.Split(upstream(v), ".")
	if len(parts) < i.Parts {
		return ""
	}
	for _, p := range parts[:i.Parts] {
		if _, err := strconv.Atoi(p); err != nil {
			return ""
		}
	}
	return strings.Join(parts[:i.Parts], ".") + i.Suffix
}

func (i Interpreter) dirs(token string) []string {
	out := make([]string, len(i.Dirs))
	for n, d := range i.Dirs {
		out[n] = strings.ReplaceAll(d, "%s", token)
	}
	return out
}

// Moved returns the old version's directories when u updates an interpreter
// to a version with a different directory token.
func Moved(u Update) ([]string, bool) {
	for _, i := range Interpreters {
		if i.Package != u.Name {
			continue
		}
		oldT, newT := i.token(u.OldVersion), i.token(u.NewVersion)
		if oldT == "" || newT == "" || oldT == newT {
			return nil, false
		}
		return i.dirs(oldT), true
	}
	return nil, false
}

// upstream strips the epoch and pkgrel from an Arch version:
// "1:2.4.8-2" becomes "2.4.8".
func upstream(v string) string {
	if i := strings.IndexByte(v, ':'); i >= 0 {
		if _, err := strconv.Atoi(v[:i]); err == nil {
			v = v[i+1:]
		}
	}
	if i := strings.LastIndexByte(v, '-'); i >= 0 {
		v = v[:i]
	}
	return v
}
