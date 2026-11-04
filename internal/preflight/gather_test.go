package preflight

import (
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/system"
)

func TestMountPoints(t *testing.T) {
	data := `# comment
UUID=aaaa  /      ext4  defaults 0 1

/dev/nvme0n1p1 /boot/ vfat rw 0 2
/dev/sdb1 /mnt/with\040space ext4 rw 0 0
short
`
	got := mountPoints([]byte(data))
	want := []string{"/", "/boot", "/mnt/with space"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

type env struct {
	files map[string]string
	disk  map[string]system.DiskUsage
}

func (e env) gather(updated ...string) (Facts, error) {
	files := map[string][]byte{}
	for p, c := range e.files {
		files[p] = []byte(c)
	}
	return Gather(system.NewFakeFS(files), system.NewFakeDisk(e.disk), updated)
}

func baseEnv() env {
	return env{
		files: map[string]string{
			MountsPath:                           "/dev/sda2 / ext4 rw 0 0\n/dev/sda1 /boot vfat rw 0 0\n",
			FstabPath:                            "UUID=1 / ext4 defaults 0 1\nUUID=2 /boot vfat defaults 0 2\n",
			OSReleasePath:                        "6.17.4-arch1-1\n",
			ModulesDir + "/6.17.4-arch1-1/dep":   "x",
			"/boot/vmlinuz-linux":                strings.Repeat("k", 100),
			"/boot/initramfs-linux.img":          strings.Repeat("i", 50),
			"/boot/initramfs-linux-fallback.img": strings.Repeat("f", 20),
			"/boot/vmlinuz-linux-lts":            strings.Repeat("k", 7),
		},
		disk: map[string]system.DiskUsage{
			"/":     {FreeBytes: 1000, ID: "r"},
			"/boot": {FreeBytes: 300, ID: "b"},
		},
	}
}

func TestGather(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		f, err := baseEnv().gather("linux", "linux-headers", "vim")
		if err != nil {
			t.Fatal(err)
		}
		if !f.BootDeclared || !f.BootMounted || f.LockExists || f.RebootPending {
			t.Errorf("unexpected facts %+v", f)
		}
		if f.Root.Free != 1000 || f.Boot.Free != 300 || f.Boot.ID != "b" {
			t.Errorf("spaces: root %+v boot %+v", f.Root, f.Boot)
		}
		// No pacman.conf: the default cache dir, on the root filesystem.
		if len(f.Caches) != 1 || f.Caches[0].Path != "/var/cache/pacman/pkg" || f.Caches[0].ID != "r" {
			t.Errorf("caches %+v", f.Caches)
		}
		if got := f.BootBytes; len(got) != 1 || got["linux"] != 170 {
			t.Errorf("BootBytes = %v, want only linux: 170", got)
		}
		if f.RunningKernel != "6.17.4-arch1-1" {
			t.Errorf("RunningKernel = %q", f.RunningKernel)
		}
		if ps := Check(f); len(ps) != 0 {
			t.Errorf("problems: %+v", ps)
		}
	})

	t.Run("boot declared but not mounted", func(t *testing.T) {
		e := baseEnv()
		e.files[MountsPath] = "/dev/sda2 / ext4 rw 0 0\n"
		f, err := e.gather("linux")
		if err != nil {
			t.Fatal(err)
		}
		if !f.BootDeclared || f.BootMounted {
			t.Errorf("BootDeclared %v BootMounted %v", f.BootDeclared, f.BootMounted)
		}
		if ps := Check(f); len(ps) != 1 || ps[0].Check != CheckBootUnmounted {
			t.Errorf("problems: %+v", ps)
		}
	})

	t.Run("no separate boot", func(t *testing.T) {
		e := baseEnv()
		e.files[MountsPath] = "/dev/sda2 / ext4 rw 0 0\n"
		e.files[FstabPath] = "UUID=1 / ext4 defaults 0 1\n"
		delete(e.disk, "/boot")
		f, err := e.gather("linux")
		if err != nil {
			t.Fatal(err)
		}
		if f.BootDeclared || f.BootMounted || f.Boot.ID != "r" {
			t.Errorf("facts %+v", f)
		}
	})

	t.Run("lock and stale reboot", func(t *testing.T) {
		e := baseEnv()
		e.files[LockPath] = ""
		e.files[OSReleasePath] = "6.16.0-arch1-1\n"
		f, err := e.gather("vim")
		if err != nil {
			t.Fatal(err)
		}
		if !f.LockExists || !f.RebootPending || f.RunningKernel != "6.16.0-arch1-1" {
			t.Errorf("facts %+v", f)
		}
	})

	t.Run("custom cache dirs", func(t *testing.T) {
		e := baseEnv()
		e.files["/etc/pacman.conf"] = "[options]\nCacheDir = /mnt/big/pkg/ /var/cache/pacman/pkg/\n"
		e.disk["/mnt/big"] = system.DiskUsage{FreeBytes: 9, ID: "big"}
		f, err := e.gather()
		if err != nil {
			t.Fatal(err)
		}
		if len(f.Caches) != 2 || f.Caches[0].ID != "big" || f.Caches[1].ID != "r" {
			t.Errorf("caches %+v", f.Caches)
		}
	})

	t.Run("a hostile kernel release stays out of paths", func(t *testing.T) {
		e := baseEnv()
		e.files[OSReleasePath] = "../../etc\n"
		f, err := e.gather()
		if err != nil || f.RebootPending {
			t.Errorf("RebootPending %v, err %v", f.RebootPending, err)
		}
	})

	t.Run("unreadable facts are reported, the rest kept", func(t *testing.T) {
		boom := errors.New("boom")
		fsys := system.NewFakeFS(nil)
		fsys.ReadErr = boom
		f, err := Gather(fsys, system.NewFakeDisk(map[string]system.DiskUsage{"/": {FreeBytes: 5}}), []string{"linux"})
		if !errors.Is(err, boom) || !strings.HasPrefix(err.Error(), "preflight:") {
			t.Fatalf("err = %v", err)
		}
		if f.Root.Free != 5 {
			t.Errorf("root space lost: %+v", f.Root)
		}
		if f.BootMounted {
			t.Error("unreadable mounts must not count as mounted")
		}
	})

	t.Run("statfs failure", func(t *testing.T) {
		_, err := Gather(system.NewFakeFS(map[string][]byte{MountsPath: nil}), failingDisk{}, nil)
		if err == nil || !strings.Contains(err.Error(), "statfs x: boom") {
			t.Errorf("err = %v", err)
		}
	})
}

type failingDisk struct{}

func (failingDisk) Statfs(string) (system.DiskUsage, error) {
	return system.DiskUsage{}, &fs.PathError{Op: "statfs", Path: "x", Err: errors.New("boom")}
}
