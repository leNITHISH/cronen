//go:build race

package ui

// raceSlowdown scales time budgets: the race detector makes code several
// times slower.
const raceSlowdown = 8
