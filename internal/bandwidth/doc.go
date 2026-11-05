// Package bandwidth holds the pure logic of the bandwidth helper: how far
// behind the system is, the growth of the pending download, the time
// estimate built from earlier upgrades and prefetches, the "last used" date
// of a package, and the parsing of the network facts behind the metered
// check. Nothing here reads the system: callers pass parsed data in, so the
// package is tested like internal/risk.
package bandwidth
