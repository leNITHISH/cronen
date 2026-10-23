package pacman

import (
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/system"
)

func TestLastFullUpgrade(t *testing.T) {
	// A fixed zone for zoneless timestamps, so results don't depend on TZ.
	loc := time.FixedZone("test", -5*60*60)
	ist := time.FixedZone("+0530", 5*60*60+30*60)

	tests := []struct {
		name   string
		log    string
		want   time.Time
		wantOK bool
	}{
		{
			name:   "fixture: last ISO upgrade, later -S ignored",
			log:    string(fixtures.Bytes(fixtures.PacmanLog)),
			want:   time.Date(2026, 10, 20, 19, 2, 11, 0, ist),
			wantOK: true,
		},
		{
			name: "old format only, in loc",
			log: "[2019-01-01 12:00] [PACMAN] starting full system upgrade\n" +
				"[2019-03-14 08:30] [PACMAN] starting full system upgrade\n" +
				"[2019-03-14 08:31] [ALPM] transaction completed\n",
			want:   time.Date(2019, 3, 14, 8, 30, 0, 0, loc),
			wantOK: true,
		},
		{
			name: "ISO after old format wins",
			log: "[2019-01-01 12:00] [PACMAN] starting full system upgrade\n" +
				"[2026-01-02T03:04:05+0000] [PACMAN] starting full system upgrade\n",
			want:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			wantOK: true,
		},
		{
			name:   "RFC 3339 zone with colon",
			log:    "[2026-01-02T03:04:05+05:30] [PACMAN] starting full system upgrade\n",
			want:   time.Date(2026, 1, 2, 3, 4, 5, 0, ist),
			wantOK: true,
		},
		{
			name: "CRLF line endings",
			log: "[2026-01-02T03:04:05+0000] [PACMAN] starting full system upgrade\r\n" +
				"[2026-01-02T03:05:00+0000] [ALPM] transaction completed\r\n",
			want:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			wantOK: true,
		},
		{
			name:   "no trailing newline",
			log:    "[2026-01-02T03:04:05+0000] [PACMAN] starting full system upgrade",
			want:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			wantOK: true,
		},
		{
			name: "garbled last timestamp falls back to previous",
			log: "[2026-01-02T03:04:05+0000] [PACMAN] starting full system upgrade\n" +
				"[2026-13-45Tgarbage] [PACMAN] starting full system upgrade\n",
			want:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			wantOK: true,
		},
		{
			name: "marker inside scriptlet output is ignored",
			log: "[2026-01-02T03:04:05+0000] [PACMAN] starting full system upgrade\n" +
				"[2026-02-01T00:00:00+0000] [ALPM-SCRIPTLET] [PACMAN] starting full system upgrade\n" +
				"[2026-02-01T00:00:00+0000] [ALPM-SCRIPTLET] echo [PACMAN] starting full system upgrade\n",
			want:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			wantOK: true,
		},
		{
			name:   "no full upgrade",
			log:    "[2026-10-21T09:12:00+0530] [PACMAN] Running 'pacman -S ripgrep'\n",
			wantOK: false,
		},
		{name: "empty", log: "", wantOK: false},
		{name: "just newlines", log: "\n\n\n", wantOK: false},
		{name: "unterminated bracket", log: "[2026-01-02T03:04:05+0000 [PACMAN] starting full system upgrade\n", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := LastFullUpgrade([]byte(tt.log), loc)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && !got.Equal(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLastFullUpgradeLongLine(t *testing.T) {
	// Scriptlet output can be far longer than bufio.Scanner's default limit.
	long := make([]byte, 1<<20)
	for i := range long {
		long[i] = 'x'
	}
	log := "[2026-01-02T03:04:05+0000] [PACMAN] starting full system upgrade\n" +
		"[2026-01-02T03:05:00+0000] [ALPM-SCRIPTLET] " + string(long) + "\n"
	got, ok := LastFullUpgrade([]byte(log), time.UTC)
	if !ok || !got.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("got %v, %v", got, ok)
	}
}

func TestReadLastFullUpgrade(t *testing.T) {
	got, err := ReadLastFullUpgrade(fixtures.FS(), fixtures.PacmanLogPath, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 20, 13, 32, 11, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	if _, err := ReadLastFullUpgrade(system.NewFakeFS(nil), LogPath, time.UTC); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing log: got %v", err)
	}

	unreadable := fixtures.FS()
	unreadable.ReadErr = fs.ErrPermission
	if _, err := ReadLastFullUpgrade(unreadable, LogPath, time.UTC); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("unreadable log: got %v", err)
	}

	empty := system.NewFakeFS(map[string][]byte{LogPath: []byte("[2026-10-21T09:12:00+0530] [ALPM] transaction completed\n")})
	if _, err := ReadLastFullUpgrade(empty, LogPath, time.UTC); !errors.Is(err, ErrNoFullUpgrade) {
		t.Fatalf("no upgrade: got %v", err)
	}
}
