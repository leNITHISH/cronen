package system

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestOSOpenerRefusesNonRegularFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "l")
	if err := os.Symlink(file, link); err != nil {
		t.Skip("symlinks unsupported:", err)
	}

	f, err := OSOpener{}.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2)
	if _, err := f.ReadAt(buf, 1); err != nil || string(buf) != "el" {
		t.Errorf("ReadAt = %q, %v", buf, err)
	}
	_ = f.Close()

	for _, p := range []string{link, dir} {
		if _, err := (OSOpener{}).Open(p); !errors.Is(err, ErrNotRegular) {
			t.Errorf("Open(%s) = %v, want ErrNotRegular", p, err)
		}
	}
	if _, err := (OSOpener{}).Open(filepath.Join(dir, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open(missing) = %v, want ErrNotExist", err)
	}
}

func TestFakeOpener(t *testing.T) {
	o := NewFakeOpener(map[string][]byte{"/a/b": []byte("xyz")})
	f, err := o.Open("/a/b")
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 3)
	if n, err := f.ReadAt(buf, 0); n != 3 || (err != nil && !errors.Is(err, io.EOF)) {
		t.Errorf("ReadAt = %d, %v", n, err)
	}
	if _, err := o.Open("/nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open(/nope) = %v", err)
	}
	if got := o.Opened(); len(got) != 2 {
		t.Errorf("Opened = %v", got)
	}
}
