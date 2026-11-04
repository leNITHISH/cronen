package ops

import (
	"slices"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/pacman"
)

var testCacheDirs = []string{"/var/cache/pacman/pkg", "/home/u/.cache/paru/clone/foo"}

const (
	linuxFile  = "/var/cache/pacman/pkg/linux-6.17.3.arch1-1-x86_64.pkg.tar.zst"
	firefoxFil = "/var/cache/pacman/pkg/firefox-143.0-1-x86_64.pkg.tar.zst"
)

func TestDowngradeArgv(t *testing.T) {
	dry, err := DowngradeDryArgv([]string{linuxFile}, testCacheDirs)
	want := []string{"pacman", "-Up", "--print-format", "%n %v", "--", linuxFile}
	if err != nil || !slices.Equal(dry, want) {
		t.Errorf("dry = %q, %v", dry, err)
	}
	cmd, err := DowngradeArgv([]string{"doas"}, []string{linuxFile, firefoxFil}, testCacheDirs)
	want = []string{"doas", "pacman", "-U", "--", linuxFile, firefoxFil}
	if err != nil || !slices.Equal(cmd, want) {
		t.Errorf("cmd = %q, %v", cmd, err)
	}
	if _, err := DowngradeArgv(nil, []string{linuxFile}, testCacheDirs); err == nil {
		t.Error("no privilege command accepted")
	}
}

// Paths that must be refused before any argv exists.
func TestDowngradeRefusesBadPaths(t *testing.T) {
	for name, p := range map[string]string{
		"outside the cache": "/tmp/linux-6.17.3.arch1-1-x86_64.pkg.tar.zst",
		"dotdot":            "/var/cache/pacman/pkg/../../../tmp/linux-6.17.3.arch1-1-x86_64.pkg.tar.zst",
		"relative":          "linux-6.17.3.arch1-1-x86_64.pkg.tar.zst",
		"an option":         "-Sy",
		"a signature":       linuxFile + ".sig",
		"a newline":         linuxFile + "\n",
		"a subdirectory":    "/var/cache/pacman/pkg/x/linux-6.17.3.arch1-1-x86_64.pkg.tar.zst",
		"empty":             "",
	} {
		if argv, err := DowngradeDryArgv([]string{linuxFile, p}, testCacheDirs); err == nil || argv != nil {
			t.Errorf("%s: dry run argv built for %q: %q", name, p, argv)
		}
		if argv, err := DowngradeArgv([]string{"sudo"}, []string{p}, testCacheDirs); err == nil || argv != nil {
			t.Errorf("%s: hand-off argv built for %q: %q", name, p, argv)
		}
	}
	if _, err := DowngradeDryArgv(nil, testCacheDirs); err == nil {
		t.Error("no files accepted")
	}
	if _, err := DowngradeDryArgv([]string{linuxFile}, nil); err == nil {
		t.Error("no cache directories accepted")
	}
	if _, err := BuildDowngradePlan(DowngradeInput{Targets: []DowngradeTarget{{Name: "linux", Path: "/tmp/x-1-1-any.pkg.tar.zst"}}, Priv: []string{"sudo"}, CacheDirs: testCacheDirs}); err == nil {
		t.Error("a plan was built for a path outside the cache")
	}
	if _, err := BuildDowngradePlan(DowngradeInput{Targets: []DowngradeTarget{{Name: "-evil", Path: linuxFile}}, Priv: []string{"sudo"}, CacheDirs: testCacheDirs}); err == nil {
		t.Error("a plan was built for a bad name")
	}
}

func testDB() *pacman.LocalDB {
	return pacman.NewLocalDB([]pacman.LocalPackage{
		{Name: "linux", Version: "6.17.4.arch1-1", RequiredBy: []string{"linux-headers", "nvidia"}},
		{Name: "firefox", Version: "144.0-1"},
	}, nil, nil)
}

func downgradeInput(targets ...DowngradeTarget) DowngradeInput {
	return DowngradeInput{
		Targets: targets, Priv: []string{"sudo"}, CacheDirs: testCacheDirs,
		Protected: []string{"linux*"}, DB: testDB(),
	}
}

func TestBuildDowngradePlan(t *testing.T) {
	t.Run("signed, with reverse dependencies and protected", func(t *testing.T) {
		in := downgradeInput(DowngradeTarget{Name: "linux", From: "6.17.4.arch1-1", Version: "6.17.3.arch1-1", Path: linuxFile, HasSig: true})
		in.Out = []byte("linux 6.17.3.arch1-1\n")
		p, err := BuildDowngradePlan(in)
		if err != nil || p.Blocked() {
			t.Fatalf("%v %+v", err, p)
		}
		if got := strings.Join(p.Command, " "); got != "sudo pacman -U -- "+linuxFile {
			t.Errorf("command = %q", got)
		}
		if len(p.Items) != 1 || p.Items[0].From != "6.17.4.arch1-1" || p.Items[0].Version != "6.17.3.arch1-1" || !p.Items[0].Selected {
			t.Errorf("items = %+v", p.Items)
		}
		warnings := strings.Join(p.Warnings, "\n")
		for _, want := range []string{"required by linux-headers, nvidia", "IgnorePkg", "protected: linux"} {
			if !strings.Contains(warnings, want) {
				t.Errorf("warnings lack %q:\n%s", want, warnings)
			}
		}
		if strings.Contains(warnings, ".sig") || len(p.Unsigned) != 0 {
			t.Errorf("a signed file was reported unsigned:\n%s", warnings)
		}
		if p.Token() != "linux" {
			t.Errorf("token = %q: a protected package needs its name typed", p.Token())
		}
	})
	t.Run("missing .sig", func(t *testing.T) {
		in := downgradeInput(DowngradeTarget{Name: "firefox", From: "144.0-1", Version: "143.0-1", Path: firefoxFil})
		in.Out = []byte("firefox 143.0-1\n")
		p, err := BuildDowngradePlan(in)
		if err != nil || p.Blocked() {
			t.Fatalf("%v %+v", err, p)
		}
		if !slices.Equal(p.Unsigned, []string{"firefox-143.0-1-x86_64.pkg.tar.zst"}) {
			t.Errorf("unsigned = %v", p.Unsigned)
		}
		if w := strings.Join(p.Warnings, "\n"); !strings.Contains(w, "no .sig file beside firefox-143.0-1") {
			t.Errorf("no missing-sig warning:\n%s", w)
		}
		if p.Token() != "" {
			t.Errorf("token = %q: firefox is not protected", p.Token())
		}
		if w := strings.Join(p.Warnings, "\n"); strings.Contains(w, "required by") {
			t.Errorf("firefox has no reverse dependencies:\n%s", w)
		}
	})
	t.Run("pulled in and not listed", func(t *testing.T) {
		in := downgradeInput(DowngradeTarget{Name: "firefox", From: "144.0-1", Version: "143.0-1", Path: firefoxFil, HasSig: true})
		in.Out = []byte("firefox 143.0-1\nlibfoo 1.0-1\n")
		p, _ := BuildDowngradePlan(in)
		if len(p.Items) != 2 || p.Items[1].Selected || !strings.Contains(strings.Join(p.Warnings, "\n"), "1 package is pulled in") {
			t.Errorf("%+v", p)
		}
		in.Out = []byte("libfoo 1.0-1\n")
		p, _ = BuildDowngradePlan(in)
		if !strings.Contains(strings.Join(p.Warnings, "\n"), "firefox is not in pacman's list") {
			t.Errorf("%v", p.Warnings)
		}
	})
	t.Run("dry run failed", func(t *testing.T) {
		in := downgradeInput(DowngradeTarget{Name: "linux", From: "6.17.4.arch1-1", Version: "6.17.2.arch1-1", Path: strings.Replace(linuxFile, "6.17.3", "6.17.2", 1), HasSig: true})
		in.Failed, in.RunErr = true, errTest
		in.Stderr = []byte("loading packages...\nerror: failed to prepare transaction (could not satisfy dependencies)\n:: installing linux (6.17.2.arch1-1) breaks dependency 'linux=6.17.4.arch1-1' required by nvidia\n")
		p, err := BuildDowngradePlan(in)
		if err != nil || !p.Blocked() {
			t.Fatalf("%v %+v", err, p)
		}
		if !strings.Contains(p.Failure, "could not satisfy dependencies") || len(p.Problems) != 1 ||
			p.Problems[0].RequiredBy != "nvidia" || p.Problems[0].Dependency != "linux=6.17.4.arch1-1" {
			t.Errorf("failure = %q, problems = %+v", p.Failure, p.Problems)
		}
		if p.Token() != "" {
			t.Error("a blocked plan offers a token")
		}
	})
	t.Run("dry run printed nothing or junk", func(t *testing.T) {
		in := downgradeInput(DowngradeTarget{Name: "firefox", Path: firefoxFil, HasSig: true})
		if p, _ := BuildDowngradePlan(in); !p.Blocked() {
			t.Error("nothing printed, not blocked")
		}
		in.Out = []byte("what is this\n")
		if p, _ := BuildDowngradePlan(in); !p.Blocked() {
			t.Error("junk printed, not blocked")
		}
	})
}

type testErr string

func (e testErr) Error() string { return string(e) }

const errTest = testErr("exit status 1")
