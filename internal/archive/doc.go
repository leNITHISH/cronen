// Package archive plans the install helper's option B: installing a package
// from the Arch Linux Archive snapshot of the day of the last full upgrade.
// conf.go and choose.go are pure (they take bytes and parsed data); prepare.go
// sets the directory up through system.FS and system.Runner, and nothing in
// it touches os, os/exec or the network itself.
package archive
