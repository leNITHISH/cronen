// Package fixtures holds embedded sample data for tests and --demo, and
// builds fake system implementations that serve it.
//
// The data is cross-consistent: the pending updates, AUR packages, news
// items and pacman.log describe one plausible system, so every package can
// test against it and --demo can show all of it together.
package fixtures

import (
	"bufio"
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/leNITHISH/cronen/internal/system"
)

//go:embed testdata
var files embed.FS

// Fixture file names, relative to testdata/.
const (
	Checkupdates       = "checkupdates.txt"
	CheckupdatesNone   = "checkupdates_none.txt"
	CheckupdatesStderr = "checkupdates_error.stderr"
	PacmanQm           = "pacman_Qm.txt"
	PacmanQiPrefix     = "pacman_Qi_" // + pkgname + ".txt"
	PacdiffO           = "pacdiff_o.txt"
	Vercmp             = "vercmp.txt"
	AURInfo            = "aur_info.json"
	ArchwebSearch      = "archweb_search.json"
	News               = "news.rss"
	PacmanLog          = "pacman.log"
)

// URLs and paths the fakes answer for.
const (
	NewsURL           = "https://archlinux.org/feeds/news/"
	AURInfoURL        = "https://aur.archlinux.org/rpc/v5/info"
	ArchwebSearchURL  = "https://archlinux.org/packages/search/json/"
	PacmanLogPath     = "/var/log/pacman.log"
	vercmpCommandName = "vercmp"
)

// Now is the time the fixture data describes: a few days after the last
// full upgrade in pacman.log, with two unread news items. Tests and --demo
// use it as the current time so relative dates stay stable.
var Now = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

// Bytes returns the contents of a fixture file. It panics if the file does
// not exist, since that is a programming error.
func Bytes(name string) []byte {
	b, err := files.ReadFile(path.Join("testdata", name))
	if err != nil {
		panic(fmt.Sprintf("fixtures: %v", err))
	}
	return b
}

// CheckupdatesResult returns the fake result of checkupdates finding the
// pending updates in checkupdates.txt (exit 0).
func CheckupdatesResult() system.FakeResult {
	return system.FakeResult{Stdout: Bytes(Checkupdates)}
}

// CheckupdatesNoneResult returns the fake result of checkupdates finding
// nothing to update (exit 2, no output).
func CheckupdatesNoneResult() system.FakeResult {
	return system.FakeResult{Stdout: Bytes(CheckupdatesNone), Code: 2}
}

// CheckupdatesErrorResult returns the fake result of checkupdates failing
// (exit 1, message on stderr).
func CheckupdatesErrorResult() system.FakeResult {
	return system.FakeResult{Stderr: Bytes(CheckupdatesStderr), Code: 1}
}

// VercmpCase is one known `vercmp A B` result.
type VercmpCase struct {
	A, B   string
	Result int
}

// VercmpTable returns the known vercmp comparisons from vercmp.txt.
func VercmpTable() []VercmpCase {
	var cases []VercmpCase
	sc := bufio.NewScanner(bytes.NewReader(Bytes(Vercmp)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var c VercmpCase
		if _, err := fmt.Sscan(line, &c.A, &c.B, &c.Result); err != nil {
			panic(fmt.Sprintf("fixtures: bad vercmp line %q: %v", line, err))
		}
		cases = append(cases, c)
	}
	return cases
}

// Runner returns a FakeRunner describing the fixture system: checkupdates
// finds updates, pacman -Qm lists foreign packages, pacman -Qi answers for
// those with fixtures, pacdiff -o finds one .pacnew, and vercmp answers for
// every pair in vercmp.txt (in both orders).
func Runner() *system.FakeRunner {
	r := system.NewFakeRunner()
	r.Set("checkupdates", CheckupdatesResult())
	r.Set("pacman -Qm", system.FakeResult{Stdout: Bytes(PacmanQm)})
	r.Set("pacdiff -o", system.FakeResult{Stdout: Bytes(PacdiffO)})

	entries, err := files.ReadDir("testdata")
	if err != nil {
		panic(fmt.Sprintf("fixtures: %v", err))
	}
	for _, e := range entries {
		name := e.Name()
		if pkg, ok := strings.CutPrefix(name, PacmanQiPrefix); ok {
			pkg = strings.TrimSuffix(pkg, ".txt")
			r.Set("pacman -Qi "+pkg, system.FakeResult{Stdout: Bytes(name)})
		}
	}

	for _, c := range VercmpTable() {
		r.Set(vercmpCommandName+" "+c.A+" "+c.B, system.FakeResult{Stdout: fmt.Appendf(nil, "%d\n", c.Result)})
		r.Set(vercmpCommandName+" "+c.B+" "+c.A, system.FakeResult{Stdout: fmt.Appendf(nil, "%d\n", -c.Result)})
	}
	return r
}

// Fetcher returns a FakeFetcher serving the news feed, the AUR RPC v5 info
// endpoint and the archweb package search. AUR and archweb requests are
// answered with the subset of fixture packages named in the query, like
// the real services.
func Fetcher() *system.FakeFetcher {
	f := system.NewFakeFetcher()
	f.Set(NewsURL, system.FakeResponse{Body: Bytes(News)})
	f.Handler = handle
	return f
}

func handle(rawURL string) (system.FakeResponse, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return system.FakeResponse{}, false
	}
	base := u.Scheme + "://" + u.Host + u.Path
	switch base {
	case AURInfoURL:
		return filterResults(Bytes(AURInfo), "Name", u.Query()["arg[]"], "resultcount"), true
	case ArchwebSearchURL:
		return filterResults(Bytes(ArchwebSearch), "pkgname", u.Query()["name"], ""), true
	}
	return system.FakeResponse{}, false
}

// filterResults keeps the entries of doc["results"] whose nameKey is in
// names, and updates countKey (if non-empty) to match.
func filterResults(doc []byte, nameKey string, names []string, countKey string) system.FakeResponse {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(doc, &top); err != nil {
		panic(fmt.Sprintf("fixtures: %v", err))
	}
	var results []map[string]json.RawMessage
	if err := json.Unmarshal(top["results"], &results); err != nil {
		panic(fmt.Sprintf("fixtures: %v", err))
	}

	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	kept := []map[string]json.RawMessage{}
	for _, r := range results {
		var name string
		if err := json.Unmarshal(r[nameKey], &name); err == nil && want[name] {
			kept = append(kept, r)
		}
	}

	top["results"] = mustMarshal(kept)
	if countKey != "" {
		top[countKey] = mustMarshal(len(kept))
	}
	return system.FakeResponse{Body: mustMarshal(top)}
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("fixtures: %v", err))
	}
	return b
}

// FS returns a FakeFS holding the fixture pacman.log at PacmanLogPath.
func FS() *system.FakeFS {
	return system.NewFakeFS(map[string][]byte{PacmanLogPath: Bytes(PacmanLog)})
}
