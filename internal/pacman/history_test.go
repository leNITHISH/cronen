package pacman

import (
	"reflect"
	"testing"
	"time"
)

const historyLog = `[2026-10-20T19:02:05+0530] [PACMAN] Running 'pacman -Syu'
[2026-10-20T19:02:05+0530] [PACMAN] synchronizing package lists
[2026-10-20T19:02:11+0530] [PACMAN] starting full system upgrade
[2026-10-20T19:02:30+0530] [ALPM] transaction started
[2026-10-20T19:02:31+0530] [ALPM] upgraded firefox (143.0.4-1 -> 144.0-1)
[2026-10-20T19:02:33+0530] [ALPM] installed egl-wayland2 (1.0.0-1)
[2026-10-20T19:02:34+0530] [ALPM] removed iptables (1:1.8.11-1)
[2026-10-20T19:02:40+0530] [ALPM-SCRIPTLET] ==> Starting build: '6.17.4-arch1-1'
[2026-10-20T19:02:58+0530] [ALPM] transaction completed
[2026-11-04T10:15:00+0000] [PACMAN] Running 'pacman -U /var/cache/pacman/pkg/linux-6.17.3.arch1-1-x86_64.pkg.tar.zst'
[2026-11-04T10:15:00+0000] [ALPM] transaction started
[2026-11-04T10:15:01+0000] [ALPM] downgraded linux (6.17.4.arch1-1 -> 6.17.3.arch1-1)
[2026-11-04T10:15:01+0000] [ALPM] reinstalled ripgrep (14.1.1-1)
[2026-11-04T10:15:09+0000] [ALPM] transaction completed
`

func TestParseHistory(t *testing.T) {
	loc := time.FixedZone("T", 0)
	txs := ParseHistory([]byte(historyLog), loc)
	if len(txs) != 2 {
		t.Fatalf("%d transactions, want 2: %+v", len(txs), txs)
	}
	a, b := txs[0], txs[1]
	if a.Command != "pacman -Syu" || a.Interrupted || a.Implicit {
		t.Errorf("first = %+v", a)
	}
	if !a.Start.Equal(time.Date(2026, 10, 20, 13, 32, 30, 0, time.UTC)) || !a.End.Equal(time.Date(2026, 10, 20, 13, 32, 58, 0, time.UTC)) {
		t.Errorf("first times = %v .. %v", a.Start, a.End)
	}
	want := []Change{
		{ActionUpgraded, "firefox", "143.0.4-1", "144.0-1"},
		{ActionInstalled, "egl-wayland2", "", "1.0.0-1"},
		{ActionRemoved, "iptables", "1:1.8.11-1", ""},
	}
	if !reflect.DeepEqual(a.Changes, want) {
		t.Errorf("changes = %+v, want %+v", a.Changes, want)
	}
	if a.Count(ActionUpgraded) != 1 || a.Count(ActionRemoved) != 1 || !a.Touches("iptables") || a.Touches("linux") {
		t.Errorf("Count/Touches wrong: %+v", a)
	}
	if b.Command != "pacman -U /var/cache/pacman/pkg/linux-6.17.3.arch1-1-x86_64.pkg.tar.zst" {
		t.Errorf("command = %q", b.Command)
	}
	if got := b.Changes[0]; got != (Change{ActionDowngraded, "linux", "6.17.4.arch1-1", "6.17.3.arch1-1"}) {
		t.Errorf("downgrade = %+v", got)
	}
	if got := b.Changes[1]; got != (Change{ActionReinstalled, "ripgrep", "14.1.1-1", "14.1.1-1"}) {
		t.Errorf("reinstall = %+v", got)
	}
}

func TestParseHistoryTimestampFormats(t *testing.T) {
	loc := time.FixedZone("T", 2*3600)
	log := `[2019-01-01 12:00] [PACMAN] Running 'pacman -S foo'
[2019-01-01 12:00] [ALPM] transaction started
[2019-01-01 12:00] [ALPM] installed foo (1.0-1)
[2019-01-01 12:01] [ALPM] transaction completed
[2019-03-01 08:30:15] [ALPM] transaction started
[2019-03-01 08:30:16] [ALPM] installed bar (2.0-1)
[2019-03-01 08:30:17] [ALPM] transaction completed
[2026-11-04T10:15:00+0000] [ALPM] transaction started
[2026-11-04T10:15:01+0000] [ALPM] installed baz (3.0-1)
[2026-11-04T10:15:02+0000] [ALPM] transaction completed
[2026-11-05T10:15:00Z] [ALPM] transaction started
[2026-11-05T10:15:01Z] [ALPM] installed qux (4.0-1)
[2026-11-05T10:15:02Z] [ALPM] transaction completed
`
	txs := ParseHistory([]byte(log), loc)
	if len(txs) != 4 {
		t.Fatalf("%d transactions, want 4: %+v", len(txs), txs)
	}
	for i, want := range []time.Time{
		time.Date(2019, 1, 1, 12, 0, 0, 0, loc),
		time.Date(2019, 3, 1, 8, 30, 15, 0, loc),
		time.Date(2026, 11, 4, 10, 15, 0, 0, time.UTC),
		time.Date(2026, 11, 5, 10, 15, 0, 0, time.UTC),
	} {
		if !txs[i].Start.Equal(want) {
			t.Errorf("transaction %d starts %v, want %v", i, txs[i].Start, want)
		}
		if txs[i].Interrupted {
			t.Errorf("transaction %d is interrupted", i)
		}
	}
}

func TestParseHistoryInterrupted(t *testing.T) {
	tests := []struct {
		name string
		log  string
		want []bool // Interrupted, per transaction
	}{
		{"cut off at the end", `[2026-11-04T10:00:00+0000] [ALPM] transaction started
[2026-11-04T10:00:01+0000] [ALPM] upgraded foo (1-1 -> 2-1)
`, []bool{true}},
		{"followed by another started", `[2026-11-04T10:00:00+0000] [ALPM] transaction started
[2026-11-04T10:00:01+0000] [ALPM] upgraded foo (1-1 -> 2-1)
[2026-11-04T10:05:00+0000] [ALPM] transaction started
[2026-11-04T10:05:01+0000] [ALPM] upgraded foo (1-1 -> 2-1)
[2026-11-04T10:05:09+0000] [ALPM] transaction completed
`, []bool{true, false}},
		{"followed by another command", `[2026-11-04T10:00:00+0000] [PACMAN] Running 'pacman -Syu'
[2026-11-04T10:00:01+0000] [ALPM] transaction started
[2026-11-04T10:00:02+0000] [ALPM] upgraded foo (1-1 -> 2-1)
[2026-11-04T10:05:00+0000] [PACMAN] Running 'pacman -Syu'
[2026-11-04T10:05:01+0000] [ALPM] transaction started
[2026-11-04T10:05:09+0000] [ALPM] transaction completed
`, []bool{true, false}},
		{"started and nothing else", `[2026-11-04T10:00:00+0000] [ALPM] transaction started
`, []bool{true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txs := ParseHistory([]byte(tt.log), time.UTC)
			var got []bool
			for _, tx := range txs {
				got = append(got, tx.Interrupted)
				if tx.Interrupted && !tx.End.IsZero() {
					t.Errorf("an interrupted transaction has an end: %v", tx.End)
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("interrupted = %v, want %v", got, tt.want)
			}
		})
	}
	// The second transaction of "another command" keeps its own command.
	txs := ParseHistory([]byte(tests[2].log), time.UTC)
	if txs[0].Command != "pacman -Syu" || txs[1].Command != "pacman -Syu" {
		t.Errorf("commands = %q, %q", txs[0].Command, txs[1].Command)
	}
}

func TestParseHistoryOldLogWithoutStartedLines(t *testing.T) {
	log := `[2019-01-01 12:00] [PACMAN] Running 'pacman -Syu'
[2019-01-01 12:01] [ALPM] upgraded linux (4.20.arch1-1 -> 4.20.1.arch1-1)
[2019-01-01 12:01] [ALPM] transaction completed
`
	txs := ParseHistory([]byte(log), time.UTC)
	if len(txs) != 1 || !txs[0].Implicit || txs[0].Interrupted || txs[0].Command != "pacman -Syu" || len(txs[0].Changes) != 1 {
		t.Errorf("txs = %+v", txs)
	}
}

func TestParseHistoryIgnoresGarbage(t *testing.T) {
	log := `garbage
[not a time] [ALPM] transaction started
[2026-11-04T10:00:00+0000] [ALPM] transaction started
[2026-11-04T10:00:01+0000] [ALPM] upgraded Foo Bar (1-1 -> 2-1)
[2026-11-04T10:00:01+0000] [ALPM] upgraded ../evil (1-1 -> 2-1)
[2026-11-04T10:00:01+0000] [ALPM] upgraded foo (1-1)
[2026-11-04T10:00:01+0000] [ALPM] warning: /etc/foo installed as /etc/foo.pacnew
[2026-11-04T10:00:01+0000] [ALPM] upgraded ok (1-1 -> 2-1)
[2026-11-04T10:00:02+0000] [ALPM] transaction completed
[2026-11-04T10:00:03+0000] [ALPM] transaction completed
`
	txs := ParseHistory([]byte(log), time.UTC)
	if len(txs) != 1 || len(txs[0].Changes) != 1 || txs[0].Changes[0].Name != "ok" {
		t.Errorf("txs = %+v", txs)
	}
	if got := ParseHistory(nil, time.UTC); len(got) != 0 {
		t.Errorf("empty log: %+v", got)
	}
}

func TestReverseTransactions(t *testing.T) {
	in := []LogTransaction{{Command: "a"}, {Command: "b"}}
	out := ReverseTransactions(in)
	if out[0].Command != "b" || in[0].Command != "a" {
		t.Errorf("out = %+v, in = %+v", out, in)
	}
}
