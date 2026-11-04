package system

import (
	"errors"
	"io/fs"
	"runtime"
	"testing"
)

func TestFakeDisk(t *testing.T) {
	d := NewFakeDisk(map[string]DiskUsage{
		"/":     {FreeBytes: 100, ID: "root"},
		"/boot": {FreeBytes: 5, ID: "esp"},
	})
	tests := []struct {
		path    string
		want    string
		wantErr bool
	}{
		{"/", "root", false},
		{"/boot", "esp", false},
		{"/boot/", "esp", false},
		{"/var/cache/pacman/pkg", "root", false},
		{"/boot/EFI", "esp", false},
	}
	for _, tt := range tests {
		u, err := d.Statfs(tt.path)
		if err != nil || u.ID != tt.want {
			t.Errorf("Statfs(%q) = %+v, %v; want ID %q", tt.path, u, err, tt.want)
		}
	}
	if got := len(d.Calls()); got != len(tests) {
		t.Errorf("recorded %d calls, want %d", got, len(tests))
	}

	empty := NewFakeDisk(nil)
	if _, err := empty.Statfs("/x"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("unknown path: err = %v, want ErrNotExist", err)
	}
}

func TestOSDisk(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("statfs is only implemented on Linux")
	}
	u, err := OSDisk{}.Statfs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if u.TotalBytes <= 0 || u.FreeBytes < 0 || u.FreeBytes > u.TotalBytes {
		t.Errorf("implausible usage %+v", u)
	}
	if _, err := (OSDisk{}).Statfs(t.TempDir() + "/missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing path: err = %v, want ErrNotExist", err)
	}
}
