// Package partial finds partial upgrades in pacman.log: transactions whose
// command refreshed the sync databases without upgrading (pacman -Sy X) and
// installed something. It is pure: it takes parsed log transactions and
// knows nothing about the system.
//
// The command line is parsed properly, short-option clusters and long
// options included, never matched with a regular expression on the whole
// line. See ParseCommand.
package partial
