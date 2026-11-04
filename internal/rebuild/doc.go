// Package rebuild finds installed foreign (AUR) packages that an upgrade
// will leave needing a rebuild: they link against a shared library version
// that no longer exists, or install into a directory tied to the old version
// of an interpreter.
//
// Detection is pure over parsed data (Detect). Reading ELF files goes
// through system.Opener, and its results are cached by package name and
// version.
package rebuild
