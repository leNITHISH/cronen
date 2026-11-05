package snapshot

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/system"
)

func TestValidName(t *testing.T) {
	cases := []struct {
		tool Tool
		name string
		want bool
	}{
		{Timeshift, "2026-10-20_19-02-30", true},
		{Timeshift, "2026-10-20_19-02-3", false},
		{Timeshift, "2026-10-20 19-02-30", false},
		{Timeshift, "2026-10-20_19-02-30\n", false},
		{Timeshift, "2026-10-20_19-02-30 --yes", false},
		{Timeshift, "--yes", false},
		{Timeshift, "", false},
		{Timeshift, "12", false},
		{Snapper, "12", true},
		{Snapper, "1", true},
		{Snapper, "0", false}, // the current system
		{Snapper, "012", false},
		{Snapper, "1-5", false}, // a range
		{Snapper, "-1", false},
		{Snapper, "1;rm", false},
		{Snapper, "1\n", false},
		{Snapper, "$(id)", false},
		{Snapper, "", false},
		{Snapper, "1234567890", false},
		{Snapper, "2026-10-20_19-02-30", false},
		{"other", "1", false},
	}
	for _, c := range cases {
		if got := ValidName(c.tool, c.name); got != c.want {
			t.Errorf("ValidName(%s, %q) = %v, want %v", c.tool, c.name, got, c.want)
		}
	}
}

func TestValidConfigAndComment(t *testing.T) {
	for cfg, want := range map[string]bool{
		"root": true, "home_dir": true, "a.b-c": true,
		"": false, "-c": false, "--help": false, "a b": false, "a;b": false, "a\nb": false, "../x": false,
	} {
		if ValidConfig(cfg) != want {
			t.Errorf("ValidConfig(%q) != %v", cfg, want)
		}
	}
	for c, want := range map[string]bool{
		"cronen: before -Syu":                 true,
		"cronen: before -Syu (312 packages)":  true,
		"before -Syu":                         false,
		"cronen: x; rm -rf /":                 false,
		"cronen: $(id)":                       false,
		"cronen: `id`":                        false,
		"cronen: line\nbreak":                 false,
		"cronen: a|b":                         false,
		"cronen: \"quoted\"":                  false,
		"cronen: " + strings.Repeat("a", 200): false,
		"--comments":                          false,
		"":                                    false,
		"cronen: tab\there":                   false,
		"cronen: <redirect":                   false,
		"cronen: ünïcode":                     false,
	} {
		if ValidComment(c) != want {
			t.Errorf("ValidComment(%q) != %v", c, want)
		}
	}
	if c := Comment(312); c != "cronen: before -Syu (312 packages)" || !ValidComment(c) {
		t.Errorf("Comment(312) = %q", c)
	}
	if c := Comment(1); c != "cronen: before -Syu (1 package)" {
		t.Errorf("Comment(1) = %q", c)
	}
	if c := Comment(0); c != "cronen: before -Syu" || !ValidComment(c) {
		t.Errorf("Comment(0) = %q", c)
	}
}

func TestChoose(t *testing.T) {
	cases := []struct {
		name       string
		conf       string
		ts, sn     bool
		want       Tool
		found      bool
		wantMissed bool
	}{
		{"nothing installed", "", false, false, "", false, false},
		{"timeshift first", "", true, true, Timeshift, true, false},
		{"only snapper", "", false, true, Snapper, true, false},
		{"forced snapper", "snapper", true, true, Snapper, true, false},
		{"forced timeshift missing", "timeshift", false, true, "", false, true},
		{"forced snapper missing", "snapper", true, false, "", false, true},
		{"unknown value is auto", "btrfs", true, false, Timeshift, true, false},
	}
	for _, c := range cases {
		d := Choose(c.conf, c.ts, c.sn)
		if d.Found != c.found || d.Tool != c.want || (d.Missing != "") != c.wantMissed {
			t.Errorf("%s: %+v", c.name, d)
		}
	}
}

func TestParseTimeshiftListFixture(t *testing.T) {
	snaps, err := ParseTimeshiftList(fixtures.Bytes(fixtures.TimeshiftList), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 3 {
		t.Fatalf("snaps = %+v", snaps)
	}
	s := snaps[1]
	if s.Name != "2026-10-20_19-02-30" || s.Tool != Timeshift || s.Type != "O" ||
		s.Description != "cronen: before -Syu (41 packages)" ||
		!s.Date.Equal(time.Date(2026, 10, 20, 19, 2, 30, 0, time.UTC)) {
		t.Errorf("snap 1 = %+v", s)
	}
	if snaps[2].Description != "" || snaps[2].Type != "D" {
		t.Errorf("snap 2 = %+v", snaps[2])
	}
	if d, ok := Newest(snaps); !ok || d.Day() != 22 {
		t.Errorf("Newest = %v %v", d, ok)
	}
}

func TestParseTimeshiftListEmpty(t *testing.T) {
	snaps, err := ParseTimeshiftList(fixtures.Bytes(fixtures.TimeshiftListEmpty), time.UTC)
	if err != nil || len(snaps) != 0 {
		t.Fatalf("snaps %v, err %v", snaps, err)
	}
}

func TestParseTimeshiftListTolerant(t *testing.T) {
	table := "Num     Name                 Tags  Description\n" +
		"------------------------------------------------------------------------------\n"
	row := "0    >  2026-10-20_19-02-30  O     a b c\n"
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"no header, no rule", row, 1},
		{"ansi colours", "\x1b[1m" + table + "\x1b[0m\x1b[32m" + row + "\x1b[0m", 1},
		{"crlf", strings.ReplaceAll(table+row, "\n", "\r\n"), 1},
		{"blank lines between rows", table + "\n" + row + "\n\n" + strings.Replace(row, "30", "31", 1), 2},
		{"extra column", table + "0    >  2026-10-20_19-02-30  O  extra  a b c\n", 1},
		{"no marker", table + "0     2026-10-20_19-02-30  O  hi\n", 1},
		{"localised header", "Nr.  Name  Beschreibung\n----------------------------------------------------------\n" + row, 1},
		{"localised count", "1 Schnappschüsse, 10 GB frei\n" + table + row, 1},
		{"no tags", table + "0    >  2026-10-20_19-02-30\n", 1},
		{"rule only (empty table)", table, 0},
	}
	for _, c := range cases {
		snaps, err := ParseTimeshiftList([]byte(c.in), time.UTC)
		if err != nil || len(snaps) != c.want {
			t.Errorf("%s: %d snaps, err %v", c.name, len(snaps), err)
		}
	}
}

func TestParseTimeshiftListRefusesToGuess(t *testing.T) {
	good := string(fixtures.Bytes(fixtures.TimeshiftList))
	cases := map[string]string{
		"empty":                  "",
		"whitespace":             " \n\n",
		"prose":                  "Timeshift needs a configured backup device\n",
		"no table":               "Device : /dev/sda1\nMode : RSYNC\n",
		"truncated last row":     strings.Join(strings.Split(strings.TrimSpace(good), "\n")[:11], "\n") + "\n2  >  2026-11-01_08-0",
		"count disagrees":        strings.Replace(good, "3 snapshots", "5 snapshots", 1),
		"rows missing":           strings.Join(strings.Split(good, "\n")[:11], "\n") + "\n",
		"garbled name":           "Num Name Tags Description\n------------------------------------------------------------\n0 > 2026-10-20_19-02 O hi\n",
		"permission text only":   "E: You need to run this command as root\n",
		"name with wrong format": "Num Name\n----------------------------------------------------------\n0 > 2026-10-20 19:02:30 hi\n",
	}
	for name, in := range cases {
		snaps, err := ParseTimeshiftList([]byte(in), time.UTC)
		if !errors.Is(err, ErrUnreadable) {
			t.Errorf("%s: err %v, snaps %+v (want ErrUnreadable)", name, err, snaps)
		}
		if snaps != nil {
			t.Errorf("%s: returned snapshots with the error: %+v", name, snaps)
		}
	}
}

func TestParseSnapperListFixture(t *testing.T) {
	snaps, err := ParseSnapperList(fixtures.Bytes(fixtures.SnapperList), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 4 {
		t.Fatalf("snaps = %+v", snaps) // 0 is the running system
	}
	want := []struct {
		name, typ, desc string
		date            time.Time
	}{
		{"1", "single", "before the September upgrade", time.Date(2026, 9, 18, 19, 2, 11, 0, time.UTC)},
		{"2", "pre", "pacman -Syu", time.Date(2026, 10, 20, 19, 2, 30, 0, time.UTC)},
		{"3", "post", "pacman -Syu", time.Date(2026, 10, 20, 19, 9, 41, 0, time.UTC)},
		{"4", "single", "cronen: before -Syu (41 packages)", time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)},
	}
	for i, w := range want {
		s := snaps[i]
		if s.Name != w.name || s.Type != w.typ || s.Description != w.desc || !s.Date.Equal(w.date) || s.Tool != Snapper {
			t.Errorf("snap %d = %+v", i, s)
		}
	}
}

func TestParseSnapperListTolerant(t *testing.T) {
	head := " # | Type   | Pre # | Date | User | Cleanup | Description | Userdata\n---+--------+-------+------+------+---------+-------------+---------\n"
	cases := []struct {
		name  string
		in    string
		want  int
		dated bool
	}{
		{"only current", head + "0  | single |       |      | root |         | current     |\n", 0, false},
		{"no header", "0  | single | | | root | | current |\n1  | single | | 2026-10-20 19:02:30 | root | number | x |\n", 1, true},
		{"active markers", head + "1+ | single | | 2026-10-20 19:02:30 | root | | a |\n2- | single | | 2026-10-21 19:02:30 | root | | b |\n3* | single | | | root | | c |\n", 3, true},
		{"unknown date is kept, undated", head + "1 | single | | Sa 18 Sep 2026 | root | | x |\n", 1, false},
		{"extra column", " # | Type | Pre # | Date | User | Used Space | Cleanup | Description | Userdata\n--+---+--+--+--+--+--+--+--\n1 | single | | 2026-10-20 19:02:30 | root | 1.00 MiB | number | desc | \n", 1, true},
		{"reordered columns", " # | Date | Type | Description\n--+--+--+--\n5 | 2026-10-20 19:02:30 | pre | d\n", 1, true},
		{"ansi", "\x1b[1m" + head + "\x1b[0m1 | single | | 2026-10-20 19:02:30 | root | | x |\n", 1, true},
		{"blank lines", head + "\n1 | single | | 2026-10-20 19:02:30 | root | | x |\n\n", 1, true},
		{"unknown type", head + "1 | zweifach | | 2026-10-20 19:02:30 | root | | x |\n", 1, true},
		{"crlf", strings.ReplaceAll(head+"1 | single | | 2026-10-20 19:02:30 | root | | x |\n", "\n", "\r\n"), 1, true},
	}
	for _, c := range cases {
		snaps, err := ParseSnapperList([]byte(c.in), time.UTC)
		if err != nil || len(snaps) != c.want {
			t.Errorf("%s: %+v, err %v", c.name, snaps, err)
			continue
		}
		if c.want > 0 && snaps[len(snaps)-1].Date.IsZero() == c.dated && c.name != "active markers" {
			t.Errorf("%s: date %v", c.name, snaps[len(snaps)-1].Date)
		}
	}
}

func TestParseSnapperListRefusesToGuess(t *testing.T) {
	head := " # | Type   | Pre # | Date | User | Cleanup | Description | Userdata\n---+--------+-------+------+------+---------+-------------+---------\n"
	cases := map[string]string{
		"empty":            "",
		"prose":            "No permissions.\n",
		"prose with pipe":  "something | else\n",
		"row not a number": head + "x | single | | | root | | y |\n",
		"cut-off row":      head + "1\n",
		"cut off at type":  head + "1 |\n",
		"stray line":       head + "1 | single | | 2026-10-20 19:02:30 | root | | x |\nFailure (Unknown)\n",
		"huge number":      head + "99999999999 | single | | | root | | y |\n",
	}
	for name, in := range cases {
		snaps, err := ParseSnapperList([]byte(in), time.UTC)
		if !errors.Is(err, ErrUnreadable) {
			t.Errorf("%s: err %v snaps %+v (want ErrUnreadable)", name, err, snaps)
		}
		if snaps != nil {
			t.Errorf("%s: snapshots returned with the error", name)
		}
	}
}

func TestParseTimeshiftConfig(t *testing.T) {
	c, err := ParseTimeshiftConfig(fixtures.Bytes(fixtures.TimeshiftJSON))
	if err != nil || c.Mode != "rsync" || c.BackupUUID != "5c7d1c3e-1111-4a5b-9d2e-0123456789ab" {
		t.Fatalf("%+v %v", c, err)
	}
	for in, want := range map[string]string{
		`{"btrfs_mode":"true"}`:        "btrfs",
		`{"btrfs_mode":true}`:          "btrfs",
		`{"btrfs_mode":"True "}`:       "btrfs",
		`{"btrfs_mode":"false"}`:       "rsync",
		`{}`:                           "rsync",
		`{"btrfs_mode":3,"x":[1,2]}`:   "rsync",
		`{"backup_device_uuid":"../"}`: "rsync",
	} {
		c, err := ParseTimeshiftConfig([]byte(in))
		if err != nil || c.Mode != want {
			t.Errorf("%s: %+v %v", in, c, err)
		}
	}
	if c, _ := ParseTimeshiftConfig([]byte(`{"backup_device_uuid":"../"}`)); c.BackupUUID != "" {
		t.Errorf("accepted a bad uuid: %q", c.BackupUUID)
	}
	for _, bad := range []string{"", "null", "[]", "{", "not json"} {
		if _, err := ParseTimeshiftConfig([]byte(bad)); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestBackupMount(t *testing.T) {
	mounts := "/dev/nvme0n1p2 / ext4 rw 0 0\n/dev/sdb1 /mnt/backup\\040disk ext4 rw 0 0\n"
	if _, ok := BackupMount([]byte(mounts), "5c7d1c3e-1111-4a5b-9d2e-0123456789ab"); ok {
		t.Error("found a mount that is not there")
	}
	if p, ok := BackupMount([]byte(mounts+"/dev/sdb1 /run/timeshift/4711/backup ext4 rw 0 0\n"), "x"); !ok || p != "/run/timeshift/4711/backup" {
		t.Errorf("%q %v", p, ok)
	}
	if p, ok := BackupMount([]byte("/dev/disk/by-uuid/abcd-1234 /mnt/b\\040d ext4 rw 0 0\n"), "abcd-1234"); !ok || p != "/mnt/b d" {
		t.Errorf("%q %v", p, ok)
	}
	if _, ok := BackupMount([]byte("/dev/x /run/timeshift/backup/../evil ext4 rw 0 0\n"), ""); ok {
		t.Error("accepted a path below the mount point")
	}
}

func TestClassifyError(t *testing.T) {
	base := errors.New("exit status 1")
	for stderr, perm := range map[string]bool{
		"No permissions.\n":                       true,
		"sudo: a password is required\n":          true,
		"E: You need to run this command as root": true,
		"Permission denied":                       true,
		"Failure (listing configs failed).\n":     false,
		"":                                        false,
	} {
		err := ClassifyError(base, stderr)
		if errors.Is(err, ErrPermission) != perm {
			t.Errorf("%q: %v", stderr, err)
		}
	}
	if ClassifyError(nil, "x") != nil {
		t.Error("nil became an error")
	}
}

func TestStripANSI(t *testing.T) {
	if got := StripANSI("\x1b[1;32mhi\x1b[0m \x1b]8;;http://x\x07link\x1b]8;;\x07"); got != "hi link" {
		t.Errorf("%q", got)
	}
}

var priv = []string{"sudo"}

func TestArgvBuilders(t *testing.T) {
	join := func(a []string, err error) string {
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(a, "\x00")
	}
	cm := "cronen: before -Syu (312 packages)"
	if got, want := join(CreateArgv(Timeshift, priv, "root", cm)), strings.Join([]string{"sudo", "timeshift", "--create", "--comments", cm}, "\x00"); got != want {
		t.Errorf("timeshift create %q", got)
	}
	if got, want := join(CreateArgv(Snapper, []string{"doas"}, "root", cm)), strings.Join([]string{"doas", "snapper", "-c", "root", "create", "--description", cm, "--cleanup-algorithm", "number"}, "\x00"); got != want {
		t.Errorf("snapper create %q", got)
	}
	if got, want := join(DeleteArgv(Timeshift, priv, "root", "2026-10-20_19-02-30")), "sudo\x00timeshift\x00--delete\x00--snapshot\x002026-10-20_19-02-30"; got != want {
		t.Errorf("timeshift delete %q", got)
	}
	if got, want := join(DeleteArgv(Snapper, priv, "root", "7")), "sudo\x00snapper\x00-c\x00root\x00delete\x007"; got != want {
		t.Errorf("snapper delete %q", got)
	}
	if got, want := join(RestoreArgv(Timeshift, priv, "2026-10-20_19-02-30")), "sudo\x00timeshift\x00--restore\x00--snapshot\x002026-10-20_19-02-30"; got != want {
		t.Errorf("restore %q", got)
	}
	l, ok, err := ListArgv(Snapper, "root", false)
	if err != nil || !ok || strings.Join(l, " ") != "snapper -c root list" {
		t.Errorf("snapper list %v %v %v", l, ok, err)
	}
	l, ok, err = ListArgv(Timeshift, "root", true)
	if err != nil || !ok || strings.Join(l, " ") != "sudo -n timeshift --list" {
		t.Errorf("timeshift list %v %v %v", l, ok, err)
	}
	// Without snapshot_list_sudo, Timeshift has no listing at all.
	if l, ok, err = ListArgv(Timeshift, "root", false); ok || l != nil || err != nil {
		t.Errorf("timeshift list without the setting: %v %v %v", l, ok, err)
	}
	if s, err := RollbackDisplay(priv, "root", "7"); err != nil || s != "sudo snapper -c root rollback 7" {
		t.Errorf("rollback %q %v", s, err)
	}
}

func TestBuildersRefuse(t *testing.T) {
	cm := Comment(3)
	if _, err := RestoreArgv(Snapper, priv, "7"); err == nil {
		t.Error("a snapper restore was built")
	}
	bad := []struct {
		what string
		err  error
	}{
		{"priv none", second(CreateArgv(Timeshift, nil, "root", cm))},
		{"priv shell", second(CreateArgv(Timeshift, []string{"sh", "-c"}, "root", cm))},
		{"priv two", second(CreateArgv(Timeshift, []string{"sudo", "-n"}, "root", cm))},
		{"priv path", second(CreateArgv(Timeshift, []string{"/tmp/sudo"}, "root", cm))},
		{"comment", second(CreateArgv(Timeshift, priv, "root", "x; reboot"))},
		{"comment flag", second(CreateArgv(Timeshift, priv, "root", "--yes"))},
		{"config", second(CreateArgv(Snapper, priv, "--help", cm))},
		{"config space", second(CreateArgv(Snapper, priv, "a b", cm))},
		{"tool", second(CreateArgv("btrfs", priv, "root", cm))},
		{"ts name", second(DeleteArgv(Timeshift, priv, "root", "latest"))},
		{"ts name flag", second(DeleteArgv(Timeshift, priv, "root", "--yes"))},
		{"ts name nl", second(DeleteArgv(Timeshift, priv, "root", "2026-10-20_19-02-30\n"))},
		{"sn name range", second(DeleteArgv(Snapper, priv, "root", "1-5"))},
		{"sn name zero", second(DeleteArgv(Snapper, priv, "root", "0"))},
		{"sn name flag", second(DeleteArgv(Snapper, priv, "root", "-s"))},
		{"sn config", second(DeleteArgv(Snapper, priv, "", "3"))},
		{"restore name", second(RestoreArgv(Timeshift, priv, "2026-10-20"))},
		{"restore priv", second(RestoreArgv(Timeshift, nil, "2026-10-20_19-02-30"))},
		{"list config", func() error { _, _, e := ListArgv(Snapper, "-x", false); return e }()},
		{"list tool", func() error { _, _, e := ListArgv("x", "root", true); return e }()},
		{"rollback", func() error { _, e := RollbackDisplay(priv, "root", "x"); return e }()},
		{"rollback priv", func() error { _, e := RollbackDisplay([]string{"x"}, "root", "7"); return e }()},
	}
	for _, b := range bad {
		if b.err == nil {
			t.Errorf("%s: built", b.what)
		}
	}
}

func second(_ []string, err error) error { return err }

// Every argv a builder makes is accepted by the Guard, and by the model of
// the real tools in internal/system (which refuses invalid combinations).
func TestBuildersPassGuardAndTheToolModel(t *testing.T) {
	g := Guard{Config: "root"}
	cm := Comment(12)
	var all [][]string
	add := func(a []string, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, a)
	}
	for _, p := range []string{"sudo", "doas", "run0"} {
		pv := []string{p}
		add(CreateArgv(Timeshift, pv, "root", cm))
		add(CreateArgv(Snapper, pv, "root", cm))
		add(DeleteArgv(Timeshift, pv, "root", "2026-10-20_19-02-30"))
		add(DeleteArgv(Snapper, pv, "root", "12"))
		add(RestoreArgv(Timeshift, pv, "2026-10-20_19-02-30"))
	}
	l, _, _ := ListArgv(Snapper, "root", false)
	all = append(all, l)
	l, _, _ = ListArgv(Timeshift, "root", true)
	all = append(all, l)
	for _, a := range all {
		if err := g.Verify(a); err != nil {
			t.Errorf("guard refused %q: %v", a, err)
		}
		if msg := system.InvalidCall(a[0], a[1:]); msg != "" {
			t.Errorf("tool model refused %q: %s", a, msg)
		}
	}
}

func TestGuardRefuses(t *testing.T) {
	g := Guard{Config: "root"}
	ts := "2026-10-20_19-02-30"
	cm := Comment(3)
	cases := map[string][]string{
		"empty":                   nil,
		"empty element":           {"sudo", "timeshift", "--create", "--comments", ""},
		"not a snapshot command":  {"sudo", "pacman", "-Syu"},
		"rm":                      {"sudo", "rm", "-rf", "/"},
		"shell":                   {"sh", "-c", "timeshift --create"},
		"path program":            {"sudo", "/tmp/timeshift", "--create", "--comments", cm},
		"path privilege":          {"/tmp/sudo", "timeshift", "--create", "--comments", cm},
		"timeshift no priv":       {"timeshift", "--create", "--comments", cm},
		"timeshift --list priv":   {"sudo", "timeshift", "--list"},
		"timeshift --list doas":   {"doas", "-n", "timeshift", "--list"},
		"sudo -n create":          {"sudo", "-n", "timeshift", "--create", "--comments", cm},
		"list + extra":            {"sudo", "-n", "timeshift", "--list", "--scripted"},
		"list before -n":          {"sudo", "timeshift", "-n", "--list"},
		"extra flag --yes":        {"sudo", "timeshift", "--create", "--comments", cm, "--yes"},
		"leading --yes":           {"sudo", "timeshift", "--yes", "--create", "--comments", cm},
		"--scripted":              {"sudo", "timeshift", "--scripted", "--create", "--comments", cm},
		"two operations":          {"sudo", "timeshift", "--create", "--delete", "--snapshot", ts},
		"delete-all":              {"sudo", "timeshift", "--delete-all"},
		"delete without name":     {"sudo", "timeshift", "--delete"},
		"delete name flag":        {"sudo", "timeshift", "--delete", "--snapshot", "--yes"},
		"delete name latest":      {"sudo", "timeshift", "--delete", "--snapshot", "latest"},
		"delete two names":        {"sudo", "timeshift", "--delete", "--snapshot", ts, ts},
		"delete name+flag glued":  {"sudo", "timeshift", "--delete", "--snapshot", ts + " --yes"},
		"delete name newline":     {"sudo", "timeshift", "--delete", "--snapshot", ts + "\n--yes"},
		"delete name semicolon":   {"sudo", "timeshift", "--delete", "--snapshot", ts + ";id"},
		"restore name option":     {"sudo", "timeshift", "--restore", "--snapshot", "-x"},
		"restore glob":            {"sudo", "timeshift", "--restore", "--snapshot", "2026-*"},
		"restore = form":          {"sudo", "timeshift", "--restore", "--snapshot=" + ts},
		"comment shell":           {"sudo", "timeshift", "--create", "--comments", "cronen: $(reboot)"},
		"comment newline":         {"sudo", "timeshift", "--create", "--comments", "cronen: a\nb"},
		"comment option":          {"sudo", "timeshift", "--create", "--comments", "--restore"},
		"comment semicolon":       {"sudo", "timeshift", "--create", "--comments", "cronen: a; b"},
		"comment backtick":        {"sudo", "timeshift", "--create", "--comments", "cronen: `id`"},
		"tags flag":               {"sudo", "timeshift", "--create", "--comments", cm, "--tags", "D"},
		"create no comment":       {"sudo", "timeshift", "--create"},
		"snapper no -c":           {"sudo", "snapper", "create", "--description", cm, "--cleanup-algorithm", "number"},
		"snapper other config":    {"sudo", "snapper", "-c", "home", "create", "--description", cm, "--cleanup-algorithm", "number"},
		"snapper bad config":      {"sudo", "snapper", "-c", "--help", "list"},
		"snapper config space":    {"sudo", "snapper", "-c", "a b", "create", "--description", cm, "--cleanup-algorithm", "number"},
		"snapper long config":     {"sudo", "snapper", "--config", "root", "create", "--description", cm, "--cleanup-algorithm", "number"},
		"snapper rollback":        {"sudo", "snapper", "-c", "root", "rollback", "3"},
		"snapper rollback bare":   {"snapper", "-c", "root", "rollback", "3"},
		"snapper list priv":       {"sudo", "snapper", "-c", "root", "list"},
		"snapper list extra":      {"snapper", "-c", "root", "list", "--columns", "number"},
		"snapper list -a":         {"snapper", "-c", "root", "list", "-a"},
		"snapper create no priv":  {"snapper", "-c", "root", "create", "--description", cm, "--cleanup-algorithm", "number"},
		"snapper algorithm":       {"sudo", "snapper", "-c", "root", "create", "--description", cm, "--cleanup-algorithm", "timeline"},
		"snapper create type":     {"sudo", "snapper", "-c", "root", "create", "-t", "pre", "--description", cm, "--cleanup-algorithm", "number"},
		"snapper create cmd":      {"sudo", "snapper", "-c", "root", "create", "--command", "id", "--description", cm, "--cleanup-algorithm", "number"},
		"snapper create bad desc": {"sudo", "snapper", "-c", "root", "create", "--description", "x;y", "--cleanup-algorithm", "number"},
		"snapper delete range":    {"sudo", "snapper", "-c", "root", "delete", "1-5"},
		"snapper delete two":      {"sudo", "snapper", "-c", "root", "delete", "1", "2"},
		"snapper delete zero":     {"sudo", "snapper", "-c", "root", "delete", "0"},
		"snapper delete flag":     {"sudo", "snapper", "-c", "root", "delete", "--sync", "3"},
		"snapper delete none":     {"sudo", "snapper", "-c", "root", "delete"},
		"snapper delete shell":    {"sudo", "snapper", "-c", "root", "delete", "3;id"},
		"snapper modify":          {"sudo", "snapper", "-c", "root", "modify", "3"},
		"snapper cleanup":         {"sudo", "snapper", "-c", "root", "cleanup", "number"},
		"snapper -c only":         {"snapper", "-c", "root"},
		"snapper option first":    {"snapper", "--no-dbus", "-c", "root", "list"},
		"nul in argument":         {"sudo", "timeshift", "--delete", "--snapshot", ts + "\x00"},
		"control char":            {"sudo", "timeshift", "--delete", "--snapshot", ts + "\x1b"},
		"two privileges":          {"sudo", "sudo", "timeshift", "--create", "--comments", cm},
	}
	for name, argv := range cases {
		if err := g.Verify(argv); err == nil {
			t.Errorf("%s: %q was accepted", name, argv)
		}
	}
	// With no configured snapper config, any valid name is fine; with one,
	// only that.
	if err := (Guard{}).Verify([]string{"snapper", "-c", "home", "list"}); err != nil {
		t.Errorf("any config: %v", err)
	}
}
