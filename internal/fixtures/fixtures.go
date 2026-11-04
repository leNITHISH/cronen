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
	PacmanQq           = "pacman_Qq.txt"
	PacmanQiPrefix     = "pacman_Qi_" // + pkgname + ".txt"
	PacdiffO           = "pacdiff_o.txt"
	Vercmp             = "vercmp.txt"
	AURInfo            = "aur_info.json"
	ArchwebSearch      = "archweb_search.json"
	News               = "news.rss"
	PacmanLog          = "pacman.log"

	// The transaction preview fixtures are HAND-WRITTEN, pending capture
	// from a real Arch system. See HAND_WRITTEN.txt in testdata/.
	PacmanSup   = "pacman_Sup.txt"        // pacman -Sup --print-format '%n %v %r %s'
	PacmanSi    = "pacman_Si.txt"         // pacman -Si, for the printed packages
	PacmanQiOld = "pacman_Qi-updates.txt" // pacman -Qi, for the installed side
	PacmanQu    = "pacman_Qu.txt"         // pacman -Qu --dbpath, with an [ignored] line

	// The rebuild detection fixtures are HAND-WRITTEN too.
	PacmanQlForeign = "pacman_Ql-foreign.txt" // pacman -Ql, for the foreign packages
	// The M10 fixtures are HAND-WRITTEN too, pending capture from the real
	// services.
	PacmanQ          = "pacman_Q.txt"      // pacman -Q
	SecurityAll      = "security_all.json" // https://security.archlinux.org/all.json
	GitLabIssuesPref = "gitlab_issues_"    // + project + ".json"; other projects 404
)

// URLs and paths the fakes answer for.
const (
	NewsURL          = "https://archlinux.org/feeds/news/"
	AURInfoURL       = "https://aur.archlinux.org/rpc/v5/info"
	ArchwebSearchURL = "https://archlinux.org/packages/search/json/"
	SecurityURL      = "https://security.archlinux.org/all.json"
	// GitLabProjectsURL is the root of the GitLab project API; the project
	// path follows, URL-escaped, then /issues.
	GitLabProjectsURL = "https://gitlab.archlinux.org/api/v4/projects/"
	PacmanLogPath     = "/var/log/pacman.log"
	// SyncDBPath is where the fake checkupdates left its database.
	SyncDBPath        = "/tmp/checkup-db-1000"
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
// finds updates, pacman -Qm lists foreign packages (including makepkg debug
// splits and a VCS package), pacman -Qq lists every installed package,
// pacman -Qi answers for
// those with fixtures, pacdiff -o finds one .pacnew, and vercmp answers for
// every pair in vercmp.txt (in both orders).
func Runner() *system.FakeRunner {
	r := system.NewFakeRunner()
	r.Set("checkupdates", CheckupdatesResult())
	r.Set("pacman -Qm", system.FakeResult{Stdout: Bytes(PacmanQm)})
	r.Set("pacman -Q", system.FakeResult{Stdout: Bytes(PacmanQ)})
	r.Set("pacman -Qq", system.FakeResult{Stdout: Bytes(PacmanQq)})
	r.Set("pacdiff -o", system.FakeResult{Stdout: Bytes(PacdiffO)})
	r.Set("pacman -Sup --dbpath "+SyncDBPath+" --logfile /dev/null --noconfirm --print-format %n %v %r %s",
		system.FakeResult{Stdout: Bytes(PacmanSup)})
	r.Set("pacman -Qu --dbpath "+SyncDBPath+" --logfile /dev/null", system.FakeResult{Stdout: Bytes(PacmanQu)})
	r.Handler = infoHandler

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

// infoHandler answers `pacman -Si` (against the fake checkupdates database)
// and the batched `pacman -Qi -- names...` with the blocks of the fixtures
// for the packages named, like pacman does.
func infoHandler(line string) (system.FakeResult, bool) {
	var file string
	var names []string
	switch {
	case strings.HasPrefix(line, "pacman -Si --dbpath "+SyncDBPath+" --logfile /dev/null -- "):
		file = PacmanSi
		names = strings.Fields(strings.TrimPrefix(line, "pacman -Si --dbpath "+SyncDBPath+" --logfile /dev/null -- "))
	case strings.HasPrefix(line, "pacman -Qi -- "):
		file = PacmanQiOld
		names = strings.Fields(strings.TrimPrefix(line, "pacman -Qi -- "))
	case strings.HasPrefix(line, "pacman -Ql -- "):
		return qlHandler(strings.Fields(strings.TrimPrefix(line, "pacman -Ql -- ")))
	default:
		return system.FakeResult{}, false
	}
	blocks := infoBlocks(file)
	if file == PacmanQiOld {
		// The installed foreign packages have one -Qi fixture each.
		entries, err := files.ReadDir("testdata")
		if err != nil {
			panic(fmt.Sprintf("fixtures: %v", err))
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), PacmanQiPrefix) {
				for n, b := range infoBlocks(e.Name()) {
					blocks[n] = b
				}
			}
		}
	}
	var out []string
	for _, n := range names {
		b, ok := blocks[n]
		if !ok {
			return system.FakeResult{
				Stderr: []byte("error: package '" + n + "' was not found\n"), Code: 1,
			}, true
		}
		out = append(out, b)
	}
	return system.FakeResult{Stdout: []byte(strings.Join(out, "\n"))}, true
}

// infoBlocks splits a pacman -Si/-Qi fixture into its blocks by name.
func infoBlocks(file string) map[string]string {
	blocks := map[string]string{}
	for _, block := range strings.Split(string(Bytes(file)), "\n\n") {
		for l := range strings.Lines(block) {
			if name, ok := strings.CutPrefix(l, "Name            : "); ok {
				blocks[strings.TrimSpace(name)] = strings.TrimRight(block, "\n") + "\n"
			}
		}
	}
	return blocks
}

// qlHandler answers pacman -Ql -- names with the lines of the fixture for
// the packages named, in the order asked.
func qlHandler(names []string) (system.FakeResult, bool) {
	var out strings.Builder
	for _, n := range names {
		found := false
		for l := range strings.Lines(string(Bytes(PacmanQlForeign))) {
			if strings.HasPrefix(l, n+" ") {
				out.WriteString(l)
				found = true
			}
		}
		if !found {
			return system.FakeResult{Stderr: []byte("error: package '" + n + "' was not found\n"), Code: 1}, true
		}
	}
	return system.FakeResult{Stdout: []byte(out.String())}, true
}

// Fetcher returns a FakeFetcher serving the news feed, the AUR RPC v5 info
// endpoint and the archweb package search. AUR and archweb requests are
// answered with the subset of fixture packages named in the query, like
// the real services.
func Fetcher() *system.FakeFetcher {
	f := system.NewFakeFetcher()
	f.Set(NewsURL, system.FakeResponse{Body: Bytes(News)})
	f.Set(SecurityURL, system.FakeResponse{Body: Bytes(SecurityAll)})
	f.Handler = handle
	return f
}

// handleGitLab answers the issues of the projects that have a fixture and
// 404s for the others, like GitLab does for a project that doesn't exist.
func handleGitLab(rawURL string) (system.FakeResponse, bool) {
	rest, ok := strings.CutPrefix(rawURL, GitLabProjectsURL)
	if !ok {
		return system.FakeResponse{}, false
	}
	escaped, _, ok := strings.Cut(rest, "/issues")
	if !ok {
		return system.FakeResponse{}, false
	}
	full, err := url.PathUnescape(escaped)
	if err != nil {
		return system.FakeResponse{}, false
	}
	name := GitLabIssuesPref + path.Base(full) + ".json"
	if _, err := files.ReadFile(path.Join("testdata", name)); err != nil {
		return system.FakeResponse{Status: 404}, true
	}
	return system.FakeResponse{Body: Bytes(name)}, true
}

func handle(rawURL string) (system.FakeResponse, bool) {
	if r, ok := preflightResponse(rawURL); ok {
		return r, true
	}
	if resp, ok := handleGitLab(rawURL); ok {
		return resp, true
	}
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

// FS returns a FakeFS holding the fixture pacman.log at PacmanLogPath, the
// sync database checkupdates leaves behind at SyncDBPath, and the files
// preflight reads (see preflight.go).
func FS() *system.FakeFS {
	files := preflightFiles()
	files[PacmanLogPath] = Bytes(PacmanLog)
	files[SyncDBPath+"/sync/core.db"] = nil
	files[SyncDBPath+"/sync/extra.db"] = nil
	return system.NewFakeFS(files)
}

// Opener returns a FakeOpener serving the ELF files of the fixture foreign
// packages (see pacman_Ql-foreign.txt), built with ELF:
//
//   - /opt/visual-studio-code/code links libzstd.so.1, which the fixture
//     zstd update drops (the ELF signal);
//   - /usr/bin/nvim (neovim-git) links no library the updates change;
//   - /opt/spotify/spotify links libgbm.so.1, which mesa still provides;
//   - /usr/lib/debug/usr/bin/paru.debug links libzstd.so.1, but is a debug
//     split's file.
func Opener() *system.FakeOpener {
	return system.NewFakeOpener(map[string][]byte{
		"/opt/visual-studio-code/code":      ELF(true, "libc.so.6", "libzstd.so.1"),
		"/usr/bin/nvim":                     ELF(true, "libc.so.6", "libuv.so.1", "libluajit-5.1.so.2"),
		"/usr/bin/paru":                     ELF(true, "libc.so.6", "libalpm.so.15"),
		"/opt/spotify/spotify":              ELF(true, "libc.so.6", "libgbm.so.1"),
		"/usr/lib/debug/usr/bin/paru.debug": ELF(true, "libzstd.so.1"),
		"/usr/bin/wal":                      []byte("#!/usr/bin/python\n"),
	})
}
