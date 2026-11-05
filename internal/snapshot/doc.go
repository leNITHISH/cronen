// Package snapshot is the snapshot helper's pure core: the Timeshift and
// snapper backends' name validation, tolerant parsing of their list output
// and of /etc/timeshift/timeshift.json, the argv builders for create, list,
// delete and restore, and Guard, which refuses every command that is not
// one of those exact forms.
//
// It takes no interfaces and touches nothing: reading the files and running
// the tools goes through internal/system, in internal/summary and
// internal/ui.
//
// Needs checking on Arch: everything here is written from the tools'
// documentation and from memory of their output, not from captures. See the
// M19 pull request.
package snapshot
