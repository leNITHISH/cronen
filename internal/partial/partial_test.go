package partial

import (
	"slices"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/pacman"
)

func TestSyncsWithoutUpgrade(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		// The variants that sync without upgrading and install something.
		{"pacman -Sy discord", true},
		{"pacman -Syy discord", true},
		{"pacman -S -y discord", true},
		{"pacman -yS discord", true},
		{"pacman --sync --refresh discord", true},
		{"pacman -S --refresh discord", true},
		{"pacman --sync -y discord", true},
		{"pacman -Sy --needed --noconfirm discord vlc", true},
		{"pacman -Sy -- discord", true},
		{"pacman -Sy --config /etc/pacman.conf discord", true},
		{"pacman -S -y --config /etc/pacman.conf -- discord", true},
		{"pacman -Sy --dbpath=/var/lib/pacman discord", true},
		{"/usr/bin/pacman -Sy discord", true},
		{"sudo pacman -Sy discord", true},
		{"paru -Sy discord", true},
		{"yay -Sy discord", true},
		{"yay -S -y discord", true},
		{"paru --sync --refresh discord", true},
		{"paru -Syy discord", true},
		{"yay -yS discord", true},

		// Full upgrades never match.
		{"pacman -Syu", false},
		{"pacman -Syyu", false},
		{"pacman -Suy", false},
		{"pacman -Syu discord", false},
		{"pacman -S -y -u", false},
		{"pacman -S -u -y discord", false},
		{"pacman --sync --refresh --sysupgrade", false},
		{"pacman --sync --refresh --sysupgrade discord", false},
		{"pacman -Sy --sysupgrade discord", false},
		{"paru -Syu", false},
		{"yay -Syu", false},
		{"yay -Syyu discord", false},
		{"yay -Suy", false},

		// No refresh, no target, or not a sync at all.
		{"pacman -Sy", false},
		{"pacman -S discord", false},
		{"pacman -S --needed discord", false},
		{"pacman -U -- /home/user/.cache/yay/discord/discord-1-1-x86_64.pkg.tar.zst", false},
		{"pacman -D -q --asexplicit -- discord", false},
		{"pacman -R discord", false},
		{"pacman -Fy", false},
		{"pacman -Qy discord", false},
		{"pacman -Syw", false},

		// A value is not a target, and a target is not an option.
		{"pacman -Sy --config /etc/pacman.conf", false},
		{"pacman -Sb /var/lib/pacman discord", false},
		{"pacman -Sy -- -u", true},

		// Not pacman at all.
		{"", false},
		{"systemctl restart foo", false},
		{"echo pacman -Sy discord", false},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			c, ok := ParseCommand(tt.line)
			got := ok && c.SyncsWithoutUpgrade()
			if got != tt.want {
				t.Errorf("SyncsWithoutUpgrade(%q) = %v, want %v (parsed %+v, ok %v)", tt.line, got, tt.want, c, ok)
			}
		})
	}
}

func TestParseCommand(t *testing.T) {
	c, ok := ParseCommand("sudo -E pacman -Syy --config /etc/pacman.conf -- discord vlc")
	// sudo's own flag isn't pacman's: -E stops the prefix skipping, so
	// this is not a pacman line.
	if ok {
		t.Fatalf("parsed %+v, want not ok", c)
	}
	c, ok = ParseCommand("env LANG=C pacman -Syy --config /etc/pacman.conf -- discord vlc")
	if !ok {
		t.Fatal("env prefix not skipped")
	}
	if c.Program != "pacman" || !c.Sync || c.Refresh != 2 || c.Upgrade {
		t.Errorf("parsed %+v", c)
	}
	if want := []string{"discord", "vlc"}; !slices.Equal(c.Targets, want) {
		t.Errorf("Targets = %v, want %v", c.Targets, want)
	}
}

func tx(start string, cmd string, changes ...pacman.Change) pacman.LogTransaction {
	ts, err := time.Parse(time.RFC3339, start)
	if err != nil {
		panic(err)
	}
	return pacman.LogTransaction{Start: ts, Command: cmd, Changes: changes}
}

func TestFind(t *testing.T) {
	install := func(n string) pacman.Change {
		return pacman.Change{Action: pacman.ActionInstalled, Name: n, New: "1-1"}
	}
	upgrade := func(n string) pacman.Change {
		return pacman.Change{Action: pacman.ActionUpgraded, Name: n, Old: "1-1", New: "1-2"}
	}
	txs := []pacman.LogTransaction{
		tx("2026-10-20T10:00:00+05:30", "pacman -Syu", upgrade("glibc"), upgrade("zlib")),
		tx("2026-10-03T09:00:00+05:30", "pacman -Sy discord", install("discord"), upgrade("zlib"), install("discord")),
		tx("2026-10-09T09:00:00-07:00", "yay -S -y vlc", install("vlc")),
		tx("2026-10-10T09:00:00+00:00", "pacman -Sy" /* nothing to install */),
		tx("2026-10-11T09:00:00+00:00", "pacman -Sy htop", pacman.Change{Action: pacman.ActionRemoved, Name: "htop", Old: "1-1"}),
		tx("2026-10-12T09:00:00+00:00", "", install("x")),
	}
	got := Find(txs)
	if len(got) != 2 {
		t.Fatalf("Find found %d transactions, want 2: %+v", len(got), got)
	}
	if got[0].Command != "pacman -Sy discord" || got[1].Command != "yay -S -y vlc" {
		t.Errorf("order or commands wrong: %+v", got)
	}
	if want := []string{"discord", "zlib"}; !slices.Equal(got[0].Packages, want) {
		t.Errorf("Packages = %v, want %v", got[0].Packages, want)
	}

	last := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	since := Since(got, last)
	if len(since) != 1 || since[0].Command != "yay -S -y vlc" {
		t.Errorf("Since = %+v, want only the yay transaction", since)
	}
	if all := Since(got, time.Time{}); len(all) != 2 {
		t.Errorf("Since(zero) = %d transactions, want all 2", len(all))
	}
	pk := Packages(got)
	if !pk["discord"] || !pk["vlc"] || !pk["zlib"] || pk["glibc"] {
		t.Errorf("Packages = %v", pk)
	}
}
