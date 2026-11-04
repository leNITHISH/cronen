package summary

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/system"
)

func TestGatherPreview(t *testing.T) {
	s := Gather(context.Background(), fixtureSources())
	if !s.Previewed || len(s.Errors) > 0 {
		t.Fatalf("previewed %v, errors %v", s.Previewed, s.Errors)
	}
	tx := s.Transaction
	if len(tx.Updates) != s.Repo {
		t.Errorf("%d previewed updates, %d pending", len(tx.Updates), s.Repo)
	}
	if len(tx.New) != 2 || len(tx.Removed) != 1 || len(s.Held) != 1 || s.Held[0].Name != "bash" {
		t.Errorf("new %d, removed %d, held %v", len(tx.New), len(tx.Removed), s.Held)
	}
	// Held packages are neither updates nor flagged.
	for _, p := range s.Flagged {
		if p.Name == "bash" {
			t.Error("a held package counts as flagged")
		}
	}
	if s.Updates() != 18 {
		t.Errorf("updates = %d, want 18", s.Updates())
	}
}

func TestTextPreview(t *testing.T) {
	text := Gather(context.Background(), fixtureSources()).Text()
	for _, want := range []string{
		"transaction: 886 MiB download, +26 MiB on disk, 2 new, 1 replaced\n",
		"ℹ bash: Held by IgnorePkg: 5.3.3-1 → 5.3.3-2. Holding packages is a partial upgrade.\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
}

func TestPreviewDisabled(t *testing.T) {
	src := fixtureSources()
	src.Config.Preview = false
	r := src.Runner.(*system.FakeRunner)
	s := Gather(context.Background(), src)
	if s.Previewed || len(s.Errors) > 0 || len(s.Held) != 0 {
		t.Errorf("previewed %v, errors %v, held %v", s.Previewed, s.Errors, s.Held)
	}
	for _, c := range r.Calls() {
		if strings.Contains(c, "-Sup") || strings.Contains(c, "-Si") {
			t.Errorf("ran %q with the preview off", c)
		}
	}
	if strings.Contains(s.Text(), "transaction:") {
		t.Errorf("text mentions the transaction:\n%s", s.Text())
	}
}

// A failed preview is a failed source: --check exits 1, as for the others,
// but the rest of the summary still stands.
func TestPreviewFailure(t *testing.T) {
	for name, setup := range map[string]func(*Sources){
		"no database": func(src *Sources) { src.FS = system.NewFakeFS(nil) },
		"no path":     func(src *Sources) { src.SyncDB = "" },
		"pacman fails": func(src *Sources) {
			r := src.Runner.(*system.FakeRunner)
			r.Set("pacman -Sup --dbpath /tmp/checkup-db-1000 --logfile /dev/null --noconfirm --print-format %n %v %r %s",
				system.FakeResult{Code: 1, Stderr: []byte("error: failed to prepare transaction")})
		},
	} {
		t.Run(name, func(t *testing.T) {
			src := fixtureSources()
			setup(&src)
			s := Gather(context.Background(), src)
			if len(s.Errors) != 1 || s.Previewed {
				t.Fatalf("errors %v, previewed %v", s.Errors, s.Previewed)
			}
			if name != "pacman fails" && !errors.Is(s.Errors[0], pacman.ErrPreviewUnavailable) {
				t.Errorf("error = %v, want ErrPreviewUnavailable", s.Errors[0])
			}
			if s.ExitCode() != ExitError {
				t.Errorf("exit code %d, want %d", s.ExitCode(), ExitError)
			}
			if s.Repo != 16 || len(s.Held) != 0 || s.Transaction.DownloadBytes != 0 {
				t.Errorf("repo %d, held %v, download %d", s.Repo, s.Held, s.Transaction.DownloadBytes)
			}
		})
	}
}

// A failed checkupdates already says why, and gives the preview nothing to
// read.
func TestPreviewSkippedWhenCheckupdatesFails(t *testing.T) {
	src := fixtureSources()
	r := src.Runner.(*system.FakeRunner)
	r.Set("checkupdates", system.FakeResult{Code: 1, Stderr: []byte("boom")})
	s := Gather(context.Background(), src)
	if len(s.Errors) != 1 {
		t.Errorf("errors = %v, want only checkupdates", s.Errors)
	}
	for _, c := range r.Calls() {
		if strings.Contains(c, "-Sup") {
			t.Errorf("ran %q", c)
		}
	}
}

func TestFreshBuildFlagged(t *testing.T) {
	flagged := func(hours int) map[string]bool {
		src := fixtureSources()
		src.Config.FlagFreshHours = hours
		src.Config.AUR = false
		src.State.MarkRead(firmwareGUID, nvidiaGUID)
		got := map[string]bool{}
		for _, p := range Gather(context.Background(), src).Flagged {
			for _, f := range p.Flags {
				if f.Reason != "" && strings.HasPrefix(f.Reason, "Fresh build") {
					got[p.Name] = true
				}
			}
		}
		return got
	}
	if got := flagged(0); len(got) != 0 {
		t.Errorf("flag_fresh_hours = 0 flagged %v", got)
	}
	// linux and linux-headers were built 18 hours before the fixture's now.
	got := flagged(24)
	if len(got) != 2 || !got["linux"] || !got["linux-headers"] {
		t.Errorf("flag_fresh_hours = 24 flagged %v, want linux and linux-headers", got)
	}
}

func TestJSONWithoutPreview(t *testing.T) {
	got := string(Summary{Repo: 2}.JSON())
	const want = `"download_bytes":0,"size_delta_bytes":0,"new":0,"replaced":0,"held":0`
	if !strings.Contains(got, want) {
		t.Errorf("json = %s, want %s", got, want)
	}
}
