package ops

import (
	"slices"
	"strings"
	"testing"
)

const (
	testReal    = "/var/lib/pacman"
	testBase    = "/home/u/.cache/cronen/archive"
	testChkDB   = "/tmp/checkup-db-1000"
	testArchive = testBase + "/20260918T120000Z/2026-09-18"
)

var testGuard = Guard{RealDB: testReal, ArchiveBase: testBase}

func line(argv []string) string { return strings.Join(argv, " ") }

func TestUpgradeDryArgv(t *testing.T) {
	got, err := UpgradeDryArgv(testChkDB, []string{"htop", "foo-bar"})
	if err != nil {
		t.Fatal(err)
	}
	want := "pacman -Sup --dbpath /tmp/checkup-db-1000 --logfile /dev/null --noconfirm --print-format %n %v %r %s -- htop foo-bar"
	if line(got) != want {
		t.Fatalf("got  %s\nwant %s", line(got), want)
	}
	none, err := UpgradeDryArgv(testChkDB, nil)
	if err != nil || slices.Contains(none, "--") {
		t.Fatalf("no names: %v, %v", none, err)
	}
}

func TestUpgradeInstallArgv(t *testing.T) {
	for _, c := range []struct {
		priv   []string
		helper string
		want   string
	}{
		{[]string{"sudo"}, "", "sudo pacman -Syu -- htop"},
		{[]string{"doas"}, "", "doas pacman -Syu -- htop"},
		{[]string{"run0"}, "", "run0 pacman -Syu -- htop"},
		{[]string{"sudo"}, "paru", "paru -Syu -- htop"},
		{[]string{"sudo"}, "yay", "yay -Syu -- htop"},
	} {
		got, err := UpgradeInstallArgv(c.priv, c.helper, []string{"htop"})
		if err != nil || line(got) != c.want {
			t.Errorf("%v %q: got %q, %v; want %q", c.priv, c.helper, line(got), err, c.want)
		}
	}
}

func TestArchiveArgv(t *testing.T) {
	sync, err := ArchiveSyncArgv(testArchive)
	if err != nil {
		t.Fatal(err)
	}
	if want := "fakeroot -- pacman -Sy --dbpath " + testArchive + " --config " + testArchive + "/pacman.conf --logfile /dev/null"; line(sync) != want {
		t.Errorf("sync: got %s", line(sync))
	}
	drift, _ := ArchiveDriftArgv(testArchive)
	if want := "pacman -Qu --dbpath " + testArchive + " --config " + testArchive + "/pacman.conf --logfile /dev/null"; line(drift) != want {
		t.Errorf("drift: got %s", line(drift))
	}
	dry, _ := ArchiveDryArgv(testArchive, []string{"htop"})
	if want := "pacman -Sp --dbpath " + testArchive + " --config " + testArchive + "/pacman.conf --logfile /dev/null --noconfirm --print-format %n %v %r %s -- htop"; line(dry) != want {
		t.Errorf("dry: got %s", line(dry))
	}
	inst, _ := ArchiveInstallArgv([]string{"sudo"}, testArchive, []string{"htop"})
	if want := "sudo pacman -S --dbpath " + testArchive + " --config " + testArchive + "/pacman.conf -- htop"; line(inst) != want {
		t.Errorf("install: got %s", line(inst))
	}
}

// Every command any builder can produce, for every option combination,
// passes the guard, and none of them is a partial upgrade.
func TestBuildersNeverMakePartialUpgrade(t *testing.T) {
	nameSets := [][]string{{"htop"}, {"htop", "foo-bar"}, {"a", "b+", "c@d._"}}
	var all [][]string
	add := func(argv []string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("builder failed: %v", err)
		}
		all = append(all, argv)
	}
	for _, names := range nameSets {
		for _, priv := range [][]string{{"sudo"}, {"doas"}, {"run0"}} {
			for _, helper := range []string{"", "paru", "yay"} {
				argv, err := UpgradeInstallArgv(priv, helper, names)
				add(argv, err)
			}
			argv, err := ArchiveInstallArgv(priv, testArchive, names)
			add(argv, err)
		}
		argv, err := UpgradeDryArgv(testChkDB, names)
		add(argv, err)
		argv, err = ArchiveDryArgv(testArchive, names)
		add(argv, err)
	}
	argv, err := UpgradeDryArgv(testChkDB, nil)
	add(argv, err)
	argv, err = ArchiveSyncArgv(testArchive)
	add(argv, err)
	argv, err = ArchiveDriftArgv(testArchive)
	add(argv, err)

	for _, argv := range all {
		if err := testGuard.Verify(argv); err != nil {
			t.Errorf("guard refuses a builder's own command: %v", err)
		}
		l := " " + line(argv) + " "
		pacman := slices.Contains(argv, "pacman")
		// Read the option clusters before "--".
		var y, u, p bool
		var dbpath string
		start := slices.Index(argv, "pacman") + 1
		for i := start; i < len(argv) && start > 0; i++ {
			a := argv[i]
			if a == "--" {
				break
			}
			if a == "--dbpath" {
				dbpath = argv[i+1]
			}
			if strings.HasPrefix(a, "-S") {
				y, u, p = strings.Contains(a, "y"), strings.Contains(a, "u"), strings.Contains(a, "p")
			}
		}
		if !pacman && (argv[0] == "paru" || argv[0] == "yay") {
			if argv[1] != "-Syu" {
				t.Errorf("helper without -Syu: %s", l)
			}
			continue
		}
		if strings.Contains(l, " -Sy ") && (argv[0] != "fakeroot" || !strings.HasPrefix(dbpath, testBase+"/")) {
			t.Errorf("-Sy without -u outside an archive directory under fakeroot: %s", l)
		}
		if strings.Contains(l, " -S ") && !strings.HasPrefix(dbpath, testBase+"/") {
			t.Errorf("-S against a database that isn't an archive directory: %s", l)
		}
		if y && !u && argv[0] != "fakeroot" {
			t.Errorf("-y without -u and without fakeroot: %s", l)
		}
		if u && dbpath != "" && strings.HasPrefix(dbpath, testBase+"/") {
			t.Errorf("-u against an archive directory (would downgrade everything): %s", l)
		}
		if p && y {
			t.Errorf("dry run with -y: %s", l)
		}
	}
}

func TestGuardRefuses(t *testing.T) {
	conf := testArchive + "/pacman.conf"
	arch := []string{"--dbpath", testArchive, "--config", conf}
	cat := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	bad := map[string][]string{
		"sy against the real db":          {"sudo", "pacman", "-Sy", "--", "htop"},
		"sy, no sudo":                     {"pacman", "-Sy", "--", "htop"},
		"syy":                             {"sudo", "pacman", "-Syy", "--", "htop"},
		"yS":                              {"sudo", "pacman", "-yS", "--", "htop"},
		"s then y":                        {"sudo", "pacman", "-S", "-y", "--", "htop"},
		"long refresh":                    {"sudo", "pacman", "--sync", "--refresh", "--", "htop"},
		"long sysupgrade":                 {"sudo", "pacman", "--sync", "--refresh", "--sysupgrade", "--", "htop"},
		"s X against the real db":         {"sudo", "pacman", "-S", "--", "htop"},
		"s X, doas":                       {"doas", "pacman", "-S", "--", "htop"},
		"s X with the real dbpath":        {"sudo", "pacman", "-S", "--dbpath", testReal, "--", "htop"},
		"s X real dbpath trailing slash":  {"sudo", "pacman", "-S", "--dbpath", testReal + "/", "--config", testReal + "/pacman.conf", "--", "htop"},
		"s X, config but no dbpath":       {"sudo", "pacman", "-S", "--config", conf, "--", "htop"},
		"s X, dbpath but no config":       {"sudo", "pacman", "-S", "--dbpath", testArchive, "--", "htop"},
		"s X, dir outside the archive":    {"sudo", "pacman", "-S", "--dbpath", "/tmp/evil", "--config", "/tmp/evil/pacman.conf", "--", "htop"},
		"s X, conf elsewhere":             {"sudo", "pacman", "-S", "--dbpath", testArchive, "--config", "/etc/pacman.conf", "--", "htop"},
		"s X, dir with ..":                {"sudo", "pacman", "-S", "--dbpath", testBase + "/../x", "--config", testBase + "/../x/pacman.conf", "--", "htop"},
		"s X, relative dir":               {"sudo", "pacman", "-S", "--dbpath", "archive/x", "--config", "archive/x/pacman.conf", "--", "htop"},
		"su without y":                    {"sudo", "pacman", "-Su", "--", "htop"},
		"syu against the archive":         cat([]string{"sudo", "pacman", "-Syu"}, arch, []string{"--", "htop"}),
		"syu with a dbpath":               {"sudo", "pacman", "-Syu", "--dbpath", testChkDB, "--", "htop"},
		"syu without sudo":                {"pacman", "-Syu", "--", "htop"},
		"syu with fakeroot":               {"fakeroot", "--", "pacman", "-Syu", "--", "htop"},
		"syu no target":                   {"sudo", "pacman", "-Syu"},
		"sync of the real db, fakeroot":   {"fakeroot", "--", "pacman", "-Sy"},
		"sync of the checkupdates db":     {"fakeroot", "--", "pacman", "-Sy", "--dbpath", testChkDB},
		"sync of the real dbpath":         {"fakeroot", "--", "pacman", "-Sy", "--dbpath", testReal, "--config", testReal + "/pacman.conf"},
		"sync with a target":              cat([]string{"fakeroot", "--", "pacman", "-Sy"}, arch, []string{"--", "htop"}),
		"sync with sudo":                  cat([]string{"sudo", "fakeroot", "--", "pacman", "-Sy"}, arch),
		"sync without fakeroot":           cat([]string{"pacman", "-Sy"}, arch),
		"sync with -p":                    cat([]string{"fakeroot", "--", "pacman", "-Syp"}, arch, []string{"--", "htop"}),
		"dry run with y":                  {"pacman", "-Syup", "--dbpath", testChkDB, "--", "htop"},
		"dry sp against the real db":      {"pacman", "-Sp", "--", "htop"},
		"dry sp against the checkupdates": {"pacman", "-Sp", "--dbpath", testChkDB, "--", "htop"},
		"dry sup against the real dbpath": {"pacman", "-Sup", "--dbpath", testReal, "--", "htop"},
		"dry sup with no dbpath":          {"pacman", "-Sup", "--", "htop"},
		"download only":                   {"pacman", "-Sw", "--dbpath", testChkDB, "--", "htop"},
		"download only long":              cat([]string{"sudo", "pacman", "--downloadonly", "-S"}, arch, []string{"--", "htop"}),
		"target before --":                cat([]string{"sudo", "pacman", "-S"}, arch, []string{"htop"}),
		"invalid name":                    cat([]string{"sudo", "pacman", "-S"}, arch, []string{"--", "-Syu"}),
		"upper-case name":                 cat([]string{"sudo", "pacman", "-S"}, arch, []string{"--", "Htop"}),
		"no names":                        cat([]string{"sudo", "pacman", "-S"}, arch, []string{"--"}),
		"two operations":                  cat([]string{"sudo", "pacman", "-SQ"}, arch, []string{"--", "htop"}),
		"remove":                          {"sudo", "pacman", "-R", "--", "htop"},
		"upgrade from file":               {"sudo", "pacman", "-U", "--", "/x.pkg.tar.zst"},
		"logfile elsewhere":               cat([]string{"pacman", "-Qu"}, arch, []string{"--logfile", "/var/log/pacman.log"}),
		"qu against the real db":          {"pacman", "-Qu"},
		"qu with sudo":                    cat([]string{"sudo", "pacman", "-Qu"}, arch),
		"paru with s":                     {"paru", "-S", "--", "htop"},
		"paru sy":                         {"paru", "-Sy", "--", "htop"},
		"paru syu no names":               {"paru", "-Syu"},
		"paru with sudo":                  {"sudo", "paru", "-Syu", "--", "htop"},
		"paru bad name":                   {"paru", "-Syu", "--", "ht op;x"},
		"a shell":                         {"sh", "-c", "pacman -Sy htop"},
		"empty":                           {},
		"control char":                    {"sudo", "pacman", "-Syu", "--", "ht\nop"},
		"empty arg":                       {"sudo", "pacman", "-Syu", "--", ""},
	}
	for name, argv := range bad {
		if err := testGuard.Verify(argv); err == nil {
			t.Errorf("%s: guard allowed %q", name, line(argv))
		}
	}
}

func TestBuildersRefuseBadInput(t *testing.T) {
	badNames := [][]string{nil, {}, {""}, {"-Syu"}, {"Htop"}, {"a b"}, {"a;b"}, {"a\nb"}, {"../x"}, {"a", "a"}, {"$(id)"}, {"--noconfirm"}}
	for _, names := range badNames {
		if _, err := UpgradeInstallArgv([]string{"sudo"}, "", names); err == nil {
			t.Errorf("UpgradeInstallArgv accepted %q", names)
		}
		if _, err := ArchiveDryArgv(testArchive, names); err == nil {
			t.Errorf("ArchiveDryArgv accepted %q", names)
		}
		if _, err := ArchiveInstallArgv([]string{"sudo"}, testArchive, names); err == nil {
			t.Errorf("ArchiveInstallArgv accepted %q", names)
		}
		if len(names) > 0 {
			if _, err := UpgradeDryArgv(testChkDB, names); err == nil {
				t.Errorf("UpgradeDryArgv accepted %q", names)
			}
		}
	}
	var many []string
	for i := range MaxInstallNames + 1 {
		many = append(many, "p"+strings.Repeat("x", i))
	}
	if _, err := UpgradeInstallArgv([]string{"sudo"}, "", many); err == nil {
		t.Error("accepted too many names")
	}
	for _, priv := range [][]string{nil, {}, {"su"}, {"sh", "-c"}, {"sudo", ""}} {
		if _, err := UpgradeInstallArgv(priv, "", []string{"htop"}); err == nil {
			t.Errorf("accepted privilege command %q", priv)
		}
		if _, err := ArchiveInstallArgv(priv, testArchive, []string{"htop"}); err == nil {
			t.Errorf("archive install accepted privilege command %q", priv)
		}
	}
	if _, err := UpgradeInstallArgv([]string{"sudo"}, "pikaur", []string{"htop"}); err == nil {
		t.Error("accepted an unknown AUR helper")
	}
	for _, dir := range []string{"", "relative/dir", "/a/../b", "/a/b/", "/a\nb", "/a//b"} {
		if _, err := ArchiveSyncArgv(dir); err == nil && dir != "/a/b/" {
			t.Errorf("ArchiveSyncArgv accepted %q", dir)
		}
		if _, err := ArchiveDriftArgv(dir); err == nil && dir != "/a/b/" {
			t.Errorf("ArchiveDriftArgv accepted %q", dir)
		}
	}
	if _, err := UpgradeDryArgv("", nil); err == nil {
		t.Error("UpgradeDryArgv accepted an empty database path")
	}
}
