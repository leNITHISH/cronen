package ops

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/pacman"
)

func TestValidName(t *testing.T) {
	good := []string{"vim", "libx11", "python-pywal", "gtk+", "a.b_c@d", "7zip", "_x", "libsigc++"}
	bad := []string{"", "-rf", "--noconfirm", "Vim", "a b", "a;b", "a$(x)", "a\nb", "../etc", "/etc/passwd", "a/b", "é", "a`b`", "-"}
	for _, n := range good {
		if !ValidName(n) {
			t.Errorf("ValidName(%q) = false", n)
		}
	}
	for _, n := range bad {
		if ValidName(n) {
			t.Errorf("ValidName(%q) = true", n)
		}
	}
}

func TestDryArgvNeverNoSave(t *testing.T) {
	for _, rec := range []bool{false, true} {
		for _, ns := range []bool{false, true} {
			o := RemoveOptions{Recursive: rec, NoSave: ns}
			dry, err := RemoveDryArgv(o, []string{"a"})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(dry[1], "n") {
				t.Errorf("%+v: dry run %v has n", o, dry)
			}
			h, _ := RemoveArgv([]string{"sudo"}, o, []string{"a"})
			if strings.Contains(h[2], "n") != ns {
				t.Errorf("%+v: hand-off %v, n should be %v", o, h, ns)
			}
		}
	}
}

func TestArgv(t *testing.T) {
	tests := []struct {
		opts    RemoveOptions
		names   []string
		dry     []string
		handoff []string
	}{
		{DefaultRemoveOptions(), []string{"vim"},
			[]string{"pacman", "-Rsp", "--print-format", "%n %v", "--", "vim"},
			[]string{"sudo", "pacman", "-Rns", "--", "vim"}},
		{RemoveOptions{}, []string{"a", "b"},
			[]string{"pacman", "-Rp", "--print-format", "%n %v", "--", "a", "b"},
			[]string{"sudo", "pacman", "-R", "--", "a", "b"}},
		{RemoveOptions{Recursive: true}, []string{"a"},
			[]string{"pacman", "-Rsp", "--print-format", "%n %v", "--", "a"},
			[]string{"sudo", "pacman", "-Rs", "--", "a"}},
		{RemoveOptions{NoSave: true}, []string{"a"},
			[]string{"pacman", "-Rp", "--print-format", "%n %v", "--", "a"},
			[]string{"sudo", "pacman", "-Rn", "--", "a"}},
	}
	for _, tt := range tests {
		dry, err := RemoveDryArgv(tt.opts, tt.names)
		if err != nil || !reflect.DeepEqual(dry, tt.dry) {
			t.Errorf("dry = %v, %v; want %v", dry, err, tt.dry)
		}
		h, err := RemoveArgv([]string{"sudo"}, tt.opts, tt.names)
		if err != nil || !reflect.DeepEqual(h, tt.handoff) {
			t.Errorf("handoff = %v, %v; want %v", h, err, tt.handoff)
		}
		for _, a := range append(dry, h...) {
			if a == "-Rc" || strings.Contains(a, "c") && strings.HasPrefix(a, "-R") {
				t.Errorf("cascade in %v", a)
			}
		}
	}
	h, _ := RemoveArgv([]string{"doas"}, DefaultRemoveOptions(), []string{"x"})
	if h[0] != "doas" {
		t.Errorf("privilege ignored: %v", h)
	}
}

func TestRefusesBadNames(t *testing.T) {
	for _, bad := range []string{"-rf", "--noconfirm", "a b", "", "Foo", "a;rm", "/tmp/x.pkg.tar.zst", "x\n"} {
		names := []string{"vim", bad}
		if _, err := RemoveDryArgv(DefaultRemoveOptions(), names); err == nil {
			t.Errorf("dry run accepted %q", bad)
		}
		if _, err := RemoveArgv([]string{"sudo"}, DefaultRemoveOptions(), names); err == nil {
			t.Errorf("hand-off accepted %q", bad)
		}
		if _, err := BuildRemovePlan(RemoveInput{Options: DefaultRemoveOptions(), Names: names, Priv: []string{"sudo"}}); err == nil {
			t.Errorf("plan accepted %q", bad)
		}
	}
	if _, err := RemoveDryArgv(DefaultRemoveOptions(), nil); err == nil {
		t.Error("empty selection accepted")
	}
	if _, err := RemoveArgv(nil, DefaultRemoveOptions(), []string{"vim"}); err == nil {
		t.Error("no privilege command accepted")
	}
}

func TestIsProtected(t *testing.T) {
	g := []string{"base", "linux*", "glibc", "systemd-boot*"}
	for n, want := range map[string]bool{"base": true, "linux": true, "linux-lts": true, "glibc": true,
		"systemd-boot-update": true, "vim": false, "base-devel": false} {
		if IsProtected(n, g) != want {
			t.Errorf("IsProtected(%q) = %v", n, !want)
		}
	}
	if IsProtected("x", []string{"[bad"}) {
		t.Error("bad glob matched")
	}
}

func TestParsePrint(t *testing.T) {
	got, err := ParsePrint(fixtures.Bytes(fixtures.PacmanRp))
	want := []Printed{{"spotify", "1.2.65.1-1"}, {"alsa-lib", "1.2.14-1"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, %v", got, err)
	}
	for _, bad := range []string{"vim", "vim 1 2", "warning: x y z", "Vim 1-1"} {
		if _, err := ParsePrint([]byte(bad)); err == nil {
			t.Errorf("ParsePrint(%q) accepted", bad)
		}
	}
}

func TestParseErrors(t *testing.T) {
	e := ParseErrors(fixtures.Bytes(fixtures.PacmanRpBreaks))
	if !e.Unsatisfied || len(e.Problems) != 2 || e.Problems[0] != (Problem{Removing: "libx11", Dependency: "libx11", RequiredBy: "gtk3", Text: ":: removing libx11 breaks dependency 'libx11' required by gtk3"}) {
		t.Errorf("%+v", e)
	}
	e = ParseErrors([]byte(":: gtk3: removing libx11 breaks dependency 'libx11'\nerror: target not found: nope\n"))
	if len(e.Problems) != 1 || e.Problems[0].RequiredBy != "gtk3" || e.Problems[0].Removing != "libx11" || !reflect.DeepEqual(e.NotFound, []string{"nope"}) {
		t.Errorf("%+v", e)
	}
}

func localDB(t *testing.T) *pacman.LocalDB {
	t.Helper()
	pkgs, err := pacman.ParseLocalDB(fixtures.Bytes(fixtures.PacmanQiAll), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return pacman.NewLocalDB(pkgs, nil, nil)
}

func TestBuildRemovePlan(t *testing.T) {
	db := localDB(t)
	prot := []string{"sudo", "glibc"}
	base := RemoveInput{Options: DefaultRemoveOptions(), Names: []string{"spotify"}, Priv: []string{"sudo"}, Protected: prot, DB: db,
		Out: fixtures.Bytes(fixtures.PacmanRp)}

	p, err := BuildRemovePlan(base)
	if err != nil || p.Blocked() {
		t.Fatalf("%v %+v", err, p)
	}
	if len(p.Items) != 2 || !p.Items[0].Selected || p.Items[1].Selected || p.Items[1].Name != "alsa-lib" {
		t.Errorf("items = %+v", p.Items)
	}
	if p.Freed <= 0 || p.Token() != "" {
		t.Errorf("freed %d token %q", p.Freed, p.Token())
	}
	if !reflect.DeepEqual(p.Command, []string{"sudo", "pacman", "-Rns", "--", "spotify"}) {
		t.Errorf("command = %v", p.Command)
	}
	if len(p.Pacsave) != 0 {
		t.Errorf("pacsave with -n: %v", p.Pacsave)
	}

	// Without -n, backup files may become .pacsave.
	in := base
	in.Options.NoSave = false
	in.Names, in.Out = []string{"sudo"}, []byte("sudo 1.9.17-1\n")
	bk, err := pacman.ParseBackupFiles(fixtures.Bytes(fixtures.PacmanQii))
	if err != nil {
		t.Fatal(err)
	}
	in.Backups = bk
	p, _ = BuildRemovePlan(in)
	// Only the [modified] file becomes .pacsave.
	if !reflect.DeepEqual(p.Pacsave, []string{"/etc/sudoers"}) || p.Token() != "sudo" {
		t.Errorf("pacsave %v token %q", p.Pacsave, p.Token())
	}

	// A failed -Qii read warns and does not block.
	in.Backups, in.BackupsErr = nil, errors.New("boom")
	p, _ = BuildRemovePlan(in)
	if p.Blocked() || len(p.Pacsave) != 0 || !strings.Contains(strings.Join(p.Warnings, "|"), "backup files") {
		t.Errorf("blocked %v pacsave %v warnings %v", p.Blocked(), p.Pacsave, p.Warnings)
	}

	// A protected package pulled in counts too.
	in = base
	in.Out = []byte("spotify 1-1\nglibc 2.42-1\n")
	p, _ = BuildRemovePlan(in)
	if !reflect.DeepEqual(p.Protected, []string{"glibc"}) || p.Token() != "glibc" {
		t.Errorf("protected %v", p.Protected)
	}

	// The dry run failed: no way to proceed, and who needs what.
	in = base
	in.Names, in.Out, in.Stderr, in.Failed = []string{"libx11"}, nil, fixtures.Bytes(fixtures.PacmanRpBreaks), true
	in.RunErr = errors.New("exit status 1")
	p, _ = BuildRemovePlan(in)
	if !p.Blocked() || len(p.Problems) != 2 || len(p.Warnings) != 2 || p.Token() != "" {
		t.Errorf("%+v", p)
	}
	if !strings.Contains(p.Failure, "could not satisfy dependencies") {
		t.Errorf("failure = %q", p.Failure)
	}

	// Garbage output is a failure, never an empty plan.
	in = base
	in.Out = []byte("what\n")
	if p, _ = BuildRemovePlan(in); !p.Blocked() {
		t.Error("garbage was accepted")
	}
	in.Out = nil
	if p, _ = BuildRemovePlan(in); !p.Blocked() {
		t.Error("empty output was accepted")
	}
}

func TestPacdiffPlan(t *testing.T) {
	p, err := PacdiffPlan([]string{"sudo", "pacdiff"}, []string{"/etc/x.pacnew"})
	if err != nil || p.Kind != KindPacdiff || len(p.Files) != 1 {
		t.Errorf("%+v %v", p, err)
	}
	if _, err := PacdiffPlan(nil, []string{"x"}); err == nil {
		t.Error("no command accepted")
	}
	if _, err := PacdiffPlan([]string{"sudo", "pacdiff"}, nil); err == nil {
		t.Error("no files accepted")
	}
	if _, err := PacdiffPlan([]string{"sudo\n", "pacdiff"}, []string{"x"}); err == nil {
		t.Error("newline accepted")
	}
}
