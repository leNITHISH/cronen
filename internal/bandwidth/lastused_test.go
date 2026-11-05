package bandwidth

import (
	"testing"
	"time"
)

const mountsRelatime = `proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0
/dev/nvme0n1p2 / ext4 rw,relatime 0 0
/dev/nvme0n1p3 /home ext4 rw,noatime 0 0
/dev/sda1 /mnt/with\040space ext4 rw,noatime 0 0
tmpfs /tmp tmpfs rw,nosuid,nodev 0 0
garbage
`

func TestParseMounts(t *testing.T) {
	ms := ParseMounts([]byte(mountsRelatime))
	if len(ms) != 5 {
		t.Fatalf("parsed %d mounts, want 5: %+v", len(ms), ms)
	}
	if ms[3].Point != "/mnt/with space" {
		t.Errorf("escaped mount point = %q", ms[3].Point)
	}
}

func TestMountForAndTrust(t *testing.T) {
	ms := ParseMounts([]byte(mountsRelatime))
	tests := []struct {
		path      string
		wantPoint string
		trusted   bool
	}{
		{"/usr/bin/vim", "/", true},
		{"/home/me/bin/tool", "/home", false},
		{"/home", "/home", false},
		{"/homework/x", "/", true},
		{"/tmp/x", "/tmp", true}, // neither relatime nor noatime listed: strictatime
		{"/mnt/with space/bin/x", "/mnt/with space", false},
	}
	for _, tt := range tests {
		m, ok := MountFor(ms, tt.path)
		if !ok || m.Point != tt.wantPoint {
			t.Errorf("MountFor(%q) = %q, %v; want %q", tt.path, m.Point, ok, tt.wantPoint)
			continue
		}
		if got := m.AtimeTrusted(); got != tt.trusted {
			t.Errorf("AtimeTrusted(%q) = %v, want %v", tt.path, got, tt.trusted)
		}
	}
	if _, ok := MountFor(nil, "/usr"); ok {
		t.Error("MountFor with no mounts found one")
	}
	if (Mount{}).AtimeTrusted() {
		t.Error("a mount with no options is trusted")
	}
}

func TestUsageFiles(t *testing.T) {
	got := UsageFiles([]string{
		"/usr/", "/usr/bin/", "/usr/bin/firefox", "/usr/lib/firefox/firefox-bin",
		"/usr/share/applications/firefox.desktop", "/usr/share/applications/mimeinfo.cache",
		"/usr/share/icons/firefox.desktop", "/usr/local/sbin/x", "/opt/app/bin/run", "",
	})
	want := []string{
		"/usr/bin/firefox", "/usr/share/applications/firefox.desktop", "/usr/local/sbin/x", "/opt/app/bin/run",
	}
	if len(got) != len(want) {
		t.Fatalf("UsageFiles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("UsageFiles[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLastUsed(t *testing.T) {
	ms := ParseMounts([]byte(mountsRelatime))
	old := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	newer := old.AddDate(0, 1, 0)
	newest := old.AddDate(0, 3, 0)
	tests := []struct {
		name string
		in   []Access
		want time.Time
		ok   bool
	}{
		{"none", nil, time.Time{}, false},
		{"newest of several", []Access{{"/usr/bin/a", old}, {"/usr/bin/b", newer}}, newer, true},
		{"noatime mount is dropped", []Access{{"/usr/bin/a", old}, {"/home/me/bin/b", newest}}, old, true},
		{"only noatime: hidden", []Access{{"/home/me/bin/b", newest}}, time.Time{}, false},
		{"zero time is dropped", []Access{{"/usr/bin/a", time.Time{}}}, time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := LastUsed(tt.in, ms)
			if ok != tt.ok || !got.Equal(tt.want) {
				t.Errorf("LastUsed = %v, %v; want %v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
	if _, ok := LastUsed([]Access{{"/usr/bin/a", old}}, nil); ok {
		t.Error("LastUsed with unknown mounts must be hidden")
	}
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	for d, want := range map[int]string{0: "today", 1: "1 day ago", 12: "12 days ago", 59: "59 days ago", 120: "~4 months ago", 400: "~13 months ago", 800: "~2 years ago"} {
		if got := Ago(now.AddDate(0, 0, -d), now); got != want {
			t.Errorf("Ago(%d days) = %q, want %q", d, got, want)
		}
	}
}
