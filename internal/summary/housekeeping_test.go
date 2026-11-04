package summary

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/system"
)

func TestLoadPacnewDiffs(t *testing.T) {
	r := fixtures.Runner()
	r.Set("diff -u /etc/sudoers /etc/sudoers.pacnew", system.FakeResult{Code: 2, Stderr: []byte("diff: /etc/sudoers: Permission denied\n")})
	r.Set("diff -u /etc/a /etc/a.pacsave", system.FakeResult{})
	r.Set("diff -u /etc/b /etc/b.pacnew", system.FakeResult{Code: 2, Stderr: []byte("diff: trouble")})
	got := LoadPacnewDiffs(context.Background(), r, []string{
		"/etc/pacman.d/mirrorlist.pacnew", "/etc/sudoers.pacnew", "/etc/a.pacsave", "/etc/b.pacnew", "-rf.pacnew", "/etc/x\ny.pacnew",
	})
	if len(got) != 6 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].Original != "/etc/pacman.d/mirrorlist" || !strings.Contains(got[0].Diff, "+## Generated on 2026-10-01") || got[0].Err != nil {
		t.Errorf("differing: %+v", got[0])
	}
	if !got[1].NeedsRoot || got[1].Err != nil || got[1].Diff != "" {
		t.Errorf("unreadable: %+v", got[1])
	}
	if got[2].Diff != "" || got[2].Err != nil || got[2].NeedsRoot {
		t.Errorf("identical: %+v", got[2])
	}
	if got[3].Err == nil {
		t.Errorf("real diff failure lost: %+v", got[3])
	}
	// Names that aren't absolute paths never reach diff.
	if got[4].Err == nil || got[5].Err == nil {
		t.Errorf("unsafe paths accepted: %+v %+v", got[4], got[5])
	}
	for _, c := range r.Calls() {
		if strings.Contains(c, "-rf") || strings.Contains(c, "\n") {
			t.Errorf("ran %q", c)
		}
	}
}

func TestLoadCache(t *testing.T) {
	fsys := fixtures.FS()
	info, err := LoadCache(fsys, nil, "")
	if err != nil || info.Files != 3 || info.Bytes != (150+75+300)<<20 || !reflect.DeepEqual(info.Dirs, []string{"/var/cache/pacman/pkg"}) {
		t.Errorf("%+v %v", info, err)
	}

	// Extra directories: globs, with ~ expanded; a missing one is skipped.
	for path, data := range map[string]string{
		"/home/u/.cache/paru/clone/paru/paru-2.1.0-1-x86_64.pkg.tar.zst": "12345",
		"/home/u/.cache/paru/clone/other/other-1-1-any.pkg.tar.xz":       "123",
	} {
		if err := fsys.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	info, err = LoadCache(fsys, []string{"~/.cache/paru/clone/*", "/nope/*"}, "/home/u")
	if err != nil || info.Files != 5 || info.Bytes != (150+75+300)<<20+8 || len(info.Dirs) != 3 {
		t.Errorf("%+v %v", info, err)
	}

	// No pacman.conf: pacman's default directory.
	empty := system.NewFakeFS(nil)
	info, err = LoadCache(empty, nil, "")
	if err != nil || info.Files != 0 || len(info.Dirs) != 0 {
		t.Errorf("%+v %v", info, err)
	}
}

func TestLoadFailedUnits(t *testing.T) {
	r := fixtures.Runner()
	u, err := LoadFailedUnits(context.Background(), r)
	if err != nil || !reflect.DeepEqual(u.System, []string{"bluetooth.service"}) || u.Count() != 1 {
		t.Errorf("%+v %v", u, err)
	}
	// No user manager is not an error.
	r.Set("systemctl --user --failed --plain --no-legend", system.FakeResult{Code: 1})
	if u, err = LoadFailedUnits(context.Background(), r); err != nil || u.Count() != 1 {
		t.Errorf("%+v %v", u, err)
	}
	r.Set("systemctl --failed --plain --no-legend", system.FakeResult{Err: errors.New("no systemd")})
	if _, err = LoadFailedUnits(context.Background(), r); err == nil {
		t.Error("system failure hidden")
	}
}
