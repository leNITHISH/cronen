package aur_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/aur"
	"github.com/leNITHISH/cronen/internal/fixtures"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestParseAtom(t *testing.T) {
	commits, err := aur.ParseAtom(fixtures.Bytes("aur_atom_paru.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 3 {
		t.Fatalf("got %d commits: %+v", len(commits), commits)
	}
	c := commits[0]
	if c.Title != "upgpkg: paru 2.1.0-2" || c.Author != "Morganamilo" || !c.Date.Equal(at("2025-10-20T10:00:00Z")) {
		t.Errorf("newest = %+v", c)
	}
	if len(c.ID) != 40 {
		t.Errorf("id = %q", c.ID)
	}
	if !commits[0].Date.After(commits[1].Date) || !commits[1].Date.After(commits[2].Date) {
		t.Errorf("not newest first: %+v", commits)
	}
}

func TestParseAtomOddities(t *testing.T) {
	// Oldest first in the feed, an id only in the link, and entries
	// without an id or a date.
	feed := `<feed xmlns='http://www.w3.org/2005/Atom'>
<entry><title>old</title><published>2025-01-01T00:00:00Z</published><id>aaaaaaa1</id></entry>
<entry><title>linked</title><updated>2025-02-01T00:00:00Z</updated><id>tag:nope</id>
  <link rel='alternate' href='https://aur.archlinux.org/cgit/aur.git/commit/?h=x&amp;id=bbbbbbb2'/></entry>
<entry><title>no id</title><published>2025-03-01T00:00:00Z</published></entry>
<entry><title>no date</title><id>ccccccc3</id></entry>
</feed>`
	commits, err := aur.ParseAtom([]byte(feed))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range commits {
		ids = append(ids, c.ID)
	}
	if !slices.Equal(ids, []string{"bbbbbbb2", "aaaaaaa1"}) {
		t.Fatalf("ids = %v", ids)
	}
	if _, err := aur.ParseAtom([]byte("<feed")); err == nil {
		t.Error("garbage parsed")
	}
}

func TestBaseCommit(t *testing.T) {
	commits := []aur.Commit{
		{ID: "c3", Date: at("2025-10-20T10:00:00Z")},
		{ID: "c2", Date: at("2025-07-01T08:00:00Z")},
		{ID: "c1", Date: at("2025-03-02T08:00:00Z")},
	}
	tests := []struct {
		name  string
		built time.Time
		want  string
	}{
		{"between", at("2025-07-05T09:00:00Z"), "c2"},
		{"exactly at a commit", at("2025-07-01T08:00:00Z"), "c2"},
		{"after all", at("2026-01-01T00:00:00Z"), "c3"},
		{"before all", at("2025-01-01T00:00:00Z"), ""},
		{"unknown build date", time.Time{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := aur.BaseCommit(commits, tt.built)
			if (tt.want != "") != ok || got.ID != tt.want {
				t.Errorf("got %q, %v; want %q", got.ID, ok, tt.want)
			}
		})
	}
}

func TestReview(t *testing.T) {
	f := fixtures.Fetcher()
	c := aur.NewClient(f, fixtures.Runner())
	ctx := context.Background()

	// paru was built 2025-07-05: the base is the 2025-07-01 commit.
	built := at("2025-07-05T09:00:00Z")
	r, err := c.Review(ctx, "paru", built, false)
	if err != nil {
		t.Fatal(err)
	}
	if !r.HasBase || r.Base.Title != "upgpkg: paru 2.1.0-1" {
		t.Fatalf("base = %+v", r.Base)
	}
	if !strings.Contains(r.Diff, "+pkgrel=2") || len(r.Findings) != 0 {
		t.Errorf("diff = %q, findings = %v", r.Diff, r.Findings)
	}
	wantURL := "https://aur.archlinux.org/cgit/aur.git/rawdiff/?h=paru&id=" + r.Commits[0].ID + "&id2=" + r.Base.ID
	if calls := f.Calls(); calls[len(calls)-1].URL != wantURL {
		t.Errorf("diff URL = %q, want %q", calls[len(calls)-1].URL, wantURL)
	}

	// Built before any commit: the log, no diff, and a reason.
	r, err = c.Review(ctx, "paru", at("2024-01-01T00:00:00Z"), false)
	if err != nil || r.Diff != "" || r.HasBase || len(r.Commits) != 3 || !strings.Contains(r.Note, "newer than the installed build") {
		t.Errorf("early build: %+v, %v", r, err)
	}
	// Already at the newest commit.
	r, err = c.Review(ctx, "paru", at("2026-01-01T00:00:00Z"), false)
	if err != nil || r.Diff != "" || !strings.Contains(r.Note, "already has the newest") {
		t.Errorf("up to date: %+v, %v", r, err)
	}
	// Unknown build date.
	r, err = c.Review(ctx, "paru", time.Time{}, false)
	if err != nil || r.Diff != "" || !strings.Contains(r.Note, "build date is unknown") {
		t.Errorf("no build date: %+v, %v", r, err)
	}
	// A suspicious update is analyzed.
	r, err = c.Review(ctx, "visual-studio-code-bin", at("2025-09-01T10:00:00Z"), false)
	if err != nil || len(r.Findings) != 1 || r.Findings[0].Kind != aur.FindSourceHost {
		t.Errorf("vscode: %+v, %v", r.Findings, err)
	}
	// An unknown base is an error, not an empty log.
	if _, err := c.Review(ctx, "no-such-pkg", built, false); err == nil {
		t.Error("unknown base: no error")
	}
	if _, err := c.Log(ctx, "", false); err == nil {
		t.Error("empty base accepted")
	}
}

func TestDiffURLEscapes(t *testing.T) {
	got := aur.DiffURL("a&b", "abc1234", "def5678")
	if !strings.Contains(got, "h=a%26b") {
		t.Errorf("DiffURL = %q", got)
	}
	if got := aur.LogURL("x y"); !strings.HasSuffix(got, "?h=x+y") {
		t.Errorf("LogURL = %q", got)
	}
}

func TestAnalyzeDiff(t *testing.T) {
	tests := []struct {
		file string
		want []aur.FindingKind
		line string // text the first finding is on
	}{
		{"aur_diff_clean.diff", nil, ""},
		{"aur_diff_skip_vcs.diff", nil, ""},
		{"aur_rawdiff_paru.diff", nil, ""},
		{"aur_diff_source_host.diff", []aur.FindingKind{aur.FindSourceHost}, "files.example-cdn.xyz"},
		{"aur_diff_install.diff", []aur.FindingKind{aur.FindInstall}, "install=foo.install"},
		{"aur_diff_install_script.diff", []aur.FindingKind{aur.FindInstall}, "systemctl enable"},
		{"aur_diff_pipe_shell.diff", []aur.FindingKind{aur.FindPipeToShell}, "curl -fsSL"},
		{"aur_diff_pipe_shell_wget.diff", []aur.FindingKind{aur.FindPipeToShell}, "wget -O-"},
		{"aur_diff_decode.diff", []aur.FindingKind{aur.FindDecodeRun, aur.FindDecodeRun}, "base64 -d"},
		{"aur_diff_skip.diff", []aur.FindingKind{aur.FindSkipChecksum}, "SKIP"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			got := aur.AnalyzeDiff(string(fixtures.Bytes(tt.file)))
			var kinds []aur.FindingKind
			for _, f := range got {
				kinds = append(kinds, f.Kind)
				if f.Reason == "" || f.Text == "" {
					t.Errorf("empty finding %+v", f)
				}
			}
			if !slices.Equal(kinds, tt.want) {
				t.Fatalf("kinds = %v, want %v (%+v)", kinds, tt.want, got)
			}
			if len(got) > 0 && !strings.Contains(got[0].Text, tt.line) {
				t.Errorf("first finding on %q, want a line with %q", got[0].Text, tt.line)
			}
			lines := strings.Split(string(fixtures.Bytes(tt.file)), "\n")
			for _, f := range got {
				if lines[f.Line][1:] != f.Text || lines[f.Line][0] != '+' {
					t.Errorf("Line %d is %q, not the added %q", f.Line, lines[f.Line], f.Text)
				}
			}
		})
	}
}

func TestAnalyzeDiffEdgeCases(t *testing.T) {
	hunk := func(lines ...string) string { return "@@ -1,3 +1,3 @@\n" + strings.Join(lines, "\n") + "\n" }
	tests := []struct {
		name string
		diff string
		want int
	}{
		{"same host, new path", hunk(
			`-source=("https://github.com/a/b/archive/v1.tar.gz")`,
			`+source=("https://github.com/a/b/releases/download/v2/b.tar.gz")`), 0},
		{"a new source with nothing removed is not a host change", hunk(
			` source=("https://github.com/a/b.tar.gz"`,
			`+        "https://example.org/extra.patch")`), 0},
		{"host change in a continuation line", hunk(
			` source=("https://github.com/a/b.tar.gz"`,
			`-        "https://github.com/a/extra.patch")`,
			`+        "https://evil.example/extra.patch")`), 1},
		{"url outside source is not a source host", hunk(
			`-url="https://old.example/b"`,
			`+url="https://new.example/b"`), 0},
		{"comment with curl pipe", hunk(`+# curl https://x | sh`), 0},
		{"curl without a pipe to a shell", hunk(`+  curl -o out.json https://x.example/a.json | jq .`), 0},
		{"removed eval is not added", hunk(`-  eval "$x"`), 0},
		{"words containing eval", hunk(`+  evaluate_things; retrieval`), 0},
		{"skip for a local file next to a remote one", hunk(
			` source=("https://github.com/a/b.tar.gz" "local.patch")`,
			`+sha256sums=('aaaa' 'SKIP')`), 1},
		{"skip with no source in view", hunk(`+sha256sums=('SKIP')`), 1},
		{"b2sums skip", hunk(
			` source=("https://x.example/a.tar.gz")`,
			`+b2sums=('SKIP')`), 1},
		{"unchanged install line is context", hunk(` install=foo.install`), 0},
		{"xxd", hunk(`+  xxd -r -p blob.hex > bin`), 1},
		{"base64 encode is fine", hunk(`+  base64 key.bin > key.b64`), 0},
		{"empty", "", 0},
		{"headers only", "diff --" + "git a/x b/x\n--- a/x\n+++ b/x\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := aur.AnalyzeDiff(tt.diff); len(got) != tt.want {
				t.Errorf("got %d findings, want %d: %+v", len(got), tt.want, got)
			}
		})
	}
}
