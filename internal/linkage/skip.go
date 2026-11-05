package linkage

import "path"

// SkipRule says that ELF files under a directory aren't checked, and why.
type SkipRule struct {
	// Pattern is a path.Match pattern for a directory (or a file). Every
	// file below a matching directory is skipped.
	Pattern string
	Reason  string
}

// DefaultSkip lists where ELF files are known to be loaded in a way the
// plain library search can't judge: plugins dlopened by a host that
// provides their libraries or search path, and trees with their own
// loader. It is data, not logic: extend it with the linkage_skip config
// key, or edit the table.
//
// The entries are a first guess from the layout of Arch packages and are
// pending tuning against a real system (see the M18 pull request).
var DefaultSkip = []SkipRule{
	{"/usr/lib/gstreamer-*", "GStreamer plugins, dlopened by GStreamer"},
	{"/usr/lib/gdk-pixbuf-*", "gdk-pixbuf loaders, dlopened"},
	{"/usr/lib/gio/modules", "GIO modules, dlopened"},
	{"/usr/lib/gtk-*", "GTK modules and input methods, dlopened"},
	{"/usr/lib/qt*/plugins", "Qt plugins, dlopened by Qt"},
	{"/usr/lib/qt*/qml", "QML modules, dlopened by Qt"},
	{"/usr/lib/wine", "Wine's own libraries, loaded by wine"},
	{"/usr/lib32/wine", "Wine's own libraries, loaded by wine"},
	{"/usr/lib/valgrind", "valgrind tool and preload libraries"},
	{"/usr/lib/jvm", "the JVM loads its own libraries"},
	{"/usr/lib/node_modules", "Node.js native add-ons, dlopened by Node"},
	{"/usr/lib/firefox", "Firefox keeps its libraries in a private directory"},
	{"/usr/lib/thunderbird", "Thunderbird keeps its libraries in a private directory"},
	{"/usr/lib/chromium", "Chromium keeps its libraries in a private directory"},
	{"/usr/lib/electron*", "Electron keeps its libraries in a private directory"},
}

// Skipped reports whether file is under a directory (or is a file) matched
// by one of patterns.
func Skipped(file string, patterns []string) bool {
	for p := file; p != "/" && p != "." && p != ""; p = path.Dir(p) {
		for _, pat := range patterns {
			if ok, err := path.Match(pat, p); err == nil && ok {
				return true
			}
		}
	}
	return false
}

// SkipPatterns returns the patterns of DefaultSkip followed by extra.
func SkipPatterns(extra []string) []string {
	out := make([]string, 0, len(DefaultSkip)+len(extra))
	for _, r := range DefaultSkip {
		out = append(out, r.Pattern)
	}
	return append(out, extra...)
}
