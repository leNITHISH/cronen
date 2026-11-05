package archive

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Host is the Arch Linux Archive.
const Host = "https://archive.archlinux.org"

// OfficialRepos are the only repositories the generated config enables, in
// this order. The archive keeps daily snapshots of them.
var OfficialRepos = []string{"core", "extra", "multilib"}

// DayFormat is how a day is written in the UI and in state.
const DayFormat = "2006-01-02"

// ServerURL is the archive Server line for a day. $repo and $arch are left
// for pacman to expand.
func ServerURL(day time.Time) string {
	return Host + "/repos/" + day.UTC().Format("2006/01/02") + "/$repo/os/$arch"
}

// droppedOptions are [options] keys the generated config does not copy.
// DBPath is given on the command line. XferCommand would make root run a
// command named in a file in a user-owned directory.
var droppedOptions = []string{"DBPath", "XferCommand"}

// Report says what GenerateConf kept and left out, for the modal.
type Report struct {
	// Repos are the official repositories enabled in the generated config.
	Repos []string
	// Dropped are the other repositories of the real config (custom repos,
	// testing repos): not in the generated config.
	Dropped []string
	// Includes counts the Include lines left out, anywhere in the file.
	Includes int
	// Options are the [options] keys left out (DBPath, XferCommand).
	Options []string
}

// GenerateConf builds the pacman.conf for an archive day from the real one.
// It copies the real [options] (minus Include, DBPath and XferCommand),
// enables the official repos that are enabled in the real config, each with
// only the archive Server, and drops every other repository and every
// Include line. Comments are not copied. It is an error if no official
// repository is enabled.
func GenerateConf(realConf []byte, day time.Time) ([]byte, Report, error) {
	var rep Report
	var options []string
	enabled := map[string]bool{}
	section := ""
	sc := bufio.NewScanner(bytes.NewReader(realConf))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			switch {
			case section == "options":
			case slices.Contains(OfficialRepos, section):
				enabled[section] = true
			case !slices.Contains(rep.Dropped, section):
				rep.Dropped = append(rep.Dropped, section)
			}
			continue
		}
		key, _, _ := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		switch {
		case key == "Include":
			rep.Includes++
		case section == "options":
			if slices.Contains(droppedOptions, key) {
				if !slices.Contains(rep.Options, key) {
					rep.Options = append(rep.Options, key)
				}
				continue
			}
			options = append(options, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, rep, err
	}
	for _, r := range OfficialRepos {
		if enabled[r] {
			rep.Repos = append(rep.Repos, r)
		}
	}
	if len(rep.Repos) == 0 {
		return nil, rep, errors.New("no official repository (core, extra, multilib) is enabled in pacman.conf")
	}
	var b strings.Builder
	b.WriteString("# Written by cronen for one archive install. Do not edit: it is regenerated every time.\n[options]\n")
	for _, o := range options {
		b.WriteString(o + "\n")
	}
	for _, r := range rep.Repos {
		fmt.Fprintf(&b, "\n[%s]\nServer = %s\n", r, ServerURL(day))
	}
	return []byte(b.String()), rep, nil
}

// Days are the archive days to try for a last full upgrade at t: the day of
// the upgrade and the next, in UTC (the archive's days are UTC). The
// snapshot of the upgrade day may predate the upgrade.
func Days(t time.Time) []time.Time {
	d := time.Date(t.UTC().Year(), t.UTC().Month(), t.UTC().Day(), 0, 0, 0, 0, time.UTC)
	return []time.Time{d, d.AddDate(0, 0, 1)}
}
