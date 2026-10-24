package fixtures

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/system"
)

type aurResult struct {
	Name       string  `json:"Name"`
	Version    string  `json:"Version"`
	Maintainer *string `json:"Maintainer"`
	OutOfDate  *int64  `json:"OutOfDate"`
}

type aurResponse struct {
	ResultCount int         `json:"resultcount"`
	Results     []aurResult `json:"results"`
	Type        string      `json:"type"`
	Version     int         `json:"version"`
}

func qm(t *testing.T) map[string]string {
	t.Helper()
	pkgs := map[string]string{}
	for line := range strings.Lines(string(Bytes(PacmanQm))) {
		f := strings.Fields(line)
		if len(f) != 2 {
			t.Fatalf("bad pacman -Qm line %q", line)
		}
		pkgs[f[0]] = f[1]
	}
	return pkgs
}

func TestCheckupdatesFormat(t *testing.T) {
	re := regexp.MustCompile(`^\S+ \S+ -> \S+$`)
	n := 0
	for line := range strings.Lines(string(Bytes(Checkupdates))) {
		if !re.MatchString(strings.TrimSuffix(line, "\n")) {
			t.Errorf("bad checkupdates line %q", line)
		}
		n++
	}
	if n == 0 {
		t.Fatal("no updates in fixture")
	}
	if len(Bytes(CheckupdatesNone)) != 0 {
		t.Error("checkupdates_none should be empty")
	}
}

func TestAURFixtureCoversEdgeCases(t *testing.T) {
	var resp aurResponse
	if err := json.Unmarshal(Bytes(AURInfo), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Version != 5 || resp.ResultCount != len(resp.Results) {
		t.Fatalf("bad envelope: %+v", resp)
	}

	installed := qm(t)
	inAUR := map[string]bool{}
	var orphaned, outOfDate bool
	for _, r := range resp.Results {
		inAUR[r.Name] = true
		if _, ok := installed[r.Name]; !ok {
			t.Errorf("AUR result %s is not in pacman -Qm", r.Name)
		}
		orphaned = orphaned || r.Maintainer == nil
		outOfDate = outOfDate || r.OutOfDate != nil
	}
	if !orphaned || !outOfDate {
		t.Errorf("want an orphaned and an out-of-date package, got orphaned=%v outOfDate=%v", orphaned, outOfDate)
	}
	missing := 0
	for name := range installed {
		if !inAUR[name] {
			missing++
		}
	}
	if missing == 0 {
		t.Error("want a pacman -Qm package missing from the AUR")
	}

	// Debug splits and VCS packages need their own handling.
	all := map[string]bool{}
	for line := range strings.Lines(string(Bytes(PacmanQq))) {
		all[strings.TrimSpace(line)] = true
	}
	var debugForeignBase, debugRepoBase, vcs bool
	for name := range installed {
		if !all[name] {
			t.Errorf("%s is in pacman -Qm but not pacman -Qq", name)
		}
		if base, ok := strings.CutSuffix(name, "-debug"); ok && all[base] {
			_, foreign := installed[base]
			debugForeignBase = debugForeignBase || foreign
			debugRepoBase = debugRepoBase || !foreign
		}
		vcs = vcs || strings.HasSuffix(name, "-git")
	}
	if !debugForeignBase || !debugRepoBase || !vcs {
		t.Errorf("want debug splits of a foreign and a repo package and a VCS package; got %v %v %v",
			debugForeignBase, debugRepoBase, vcs)
	}
}

func TestRunner(t *testing.T) {
	ctx := context.Background()
	r := Runner()

	if out, code, err := r.Run(ctx, "checkupdates"); err != nil || code != 0 || len(out) == 0 {
		t.Fatalf("checkupdates: %d, %v", code, err)
	}
	for name := range qm(t) {
		out, _, err := r.Run(ctx, "pacman", "-Qi", name)
		if err != nil || !strings.Contains(string(out), "Depends On") {
			t.Errorf("pacman -Qi %s: %v", name, err)
		}
	}
	if out, _, err := r.Run(ctx, "pacdiff", "-o"); err != nil || !strings.Contains(string(out), ".pacnew") {
		t.Fatalf("pacdiff: %q, %v", out, err)
	}

	// Every installed AUR package must be comparable to its AUR version.
	var resp aurResponse
	if err := json.Unmarshal(Bytes(AURInfo), &resp); err != nil {
		t.Fatal(err)
	}
	installed := qm(t)
	for _, res := range resp.Results {
		for _, args := range [][]string{{installed[res.Name], res.Version}, {res.Version, installed[res.Name]}} {
			out, _, err := r.Run(ctx, "vercmp", args...)
			if err != nil {
				t.Errorf("vercmp %v: %v", args, err)
				continue
			}
			if s := strings.TrimSpace(string(out)); s != "-1" && s != "0" && s != "1" {
				t.Errorf("vercmp %v = %q", args, s)
			}
		}
	}

	if _, _, err := r.Run(ctx, "vercmp", "1:1.0-1", "2.0-1"); err != nil {
		t.Fatal(err)
	}
	if out, _, _ := r.Run(ctx, "vercmp", "2.0-1", "1:1.0-1"); strings.TrimSpace(string(out)) != "-1" {
		t.Errorf("reversed vercmp = %q, want -1", out)
	}

	for _, res := range []system.FakeResult{CheckupdatesNoneResult(), CheckupdatesErrorResult()} {
		r.Set("checkupdates", res)
		_, code, err := r.Run(ctx, "checkupdates")
		var exitErr *system.ExitError
		if code != res.Code || !errors.As(err, &exitErr) {
			t.Errorf("checkupdates variant: %d, %v", code, err)
		}
	}
}

func TestFetcher(t *testing.T) {
	ctx := context.Background()
	f := Fetcher()

	body, err := f.Get(ctx, NewsURL, false)
	if err != nil {
		t.Fatal(err)
	}
	feed := string(body)
	for _, want := range []string{"requires manual intervention", "nvidia-utils", "linux-firmware"} {
		if !strings.Contains(feed, want) {
			t.Errorf("news feed lacks %q", want)
		}
	}

	body, err = f.Get(ctx, AURInfoURL+"?arg[]=paru&arg[]=ttf-ms-fonts&arg[]=gnome-shell-extension-legacy", false)
	if err != nil {
		t.Fatal(err)
	}
	var resp aurResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ResultCount != 2 || len(resp.Results) != 2 {
		t.Fatalf("filtered AUR response = %+v", resp)
	}

	body, err = f.Get(ctx, ArchwebSearchURL+"?name=linux-headers", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"pkgbase":"linux"`) {
		t.Fatalf("archweb response = %s", body)
	}

	if _, err := f.Get(ctx, "https://example.com/", false); !errors.Is(err, system.ErrUnexpectedURL) {
		t.Fatalf("unknown URL: %v", err)
	}
}

func TestPacmanLog(t *testing.T) {
	data, err := FS().ReadFile(PacmanLogPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	old := regexp.MustCompile(`(?m)^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}\] \[PACMAN\] starting full system upgrade$`)
	iso := regexp.MustCompile(`(?m)^\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{4}\] \[PACMAN\] starting full system upgrade$`)
	if !old.MatchString(log) || len(iso.FindAllString(log, -1)) < 2 {
		t.Fatal("pacman.log should have old-format and several ISO-format full upgrades")
	}
}
