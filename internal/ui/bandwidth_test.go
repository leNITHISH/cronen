package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/ops"
	"github.com/leNITHISH/cronen/internal/system"
)

// withSamples gives opts the demo's timing samples, so the header has an
// estimate.
func withSamples(opts Options) Options {
	for _, x := range fixtures.DemoPrefetchSamples() {
		opts.State.AddPrefetchSample(x)
	}
	for _, x := range fixtures.DemoUpgradeSamples() {
		opts.State.AddUpgradeSample(x)
	}
	return opts
}

func TestBandwidthHeader(t *testing.T) {
	m := loaded(t, withSamples(fixtureOptions()), 100, 30)
	out := plain(m)
	if !strings.Contains(out, "2 days behind · 18 packages · 886 MiB · ~") || !strings.Contains(out, "min (from your last 2 prefetches and 6 upgrades)") {
		t.Errorf("header lacks days behind and an estimate:\n%s", out)
	}
	golden(t, "bandwidth-header", out)
	checkSize(t, m, 100, 30)

	// Without samples: no guess.
	m = loaded(t, fixtureOptions(), 100, 30)
	if !strings.Contains(plain(m), "886 MiB · no estimate yet") {
		t.Errorf("no-sample header:\n%s", plain(m))
	}
	// A narrow terminal drops the least important parts instead of wrapping.
	m = loaded(t, withSamples(fixtureOptions()), 50, 24)
	if first := strings.Split(plain(m), "\n")[2]; !strings.HasPrefix(first, " 2 days behind · 18 packages") {
		t.Errorf("narrow header = %q", first)
	}
	checkSize(t, m, 50, 24)
}

func TestSortByDownloadSize(t *testing.T) {
	m := loaded(t, fixtureOptions(), 100, 30)
	m = send(t, m, press("s"))
	if !m.updates.bySize {
		t.Fatal("s did not sort by size")
	}
	var names []string
	var last int64 = 1 << 62
	for _, e := range m.updates.entries {
		if e.kind != entryUpdate || e.aur != nil || len(e.pkg.Flags) == 0 {
			continue
		}
		if d := m.entryDownload(e); d > last {
			t.Errorf("%s (%d) comes after a smaller download (%d) in NEEDS ATTENTION", e.pkg.Name, d, last)
		} else {
			last = d
		}
		names = append(names, e.pkg.Name)
	}
	if len(names) < 5 || names[0] != "linux-firmware" {
		t.Errorf("flagged by size = %v, want linux-firmware (420 MiB) first", names)
	}
	out := plain(m)
	if !strings.Contains(out, "sorted by download size") || !strings.Contains(out, "160 MiB →590.44") {
		t.Errorf("no size shown while sorted by size:\n%s", out)
	}
	golden(t, "sort-by-size", out)
	m = send(t, m, press("j")) // the flash goes; the status bar keeps saying so
	if !strings.Contains(plain(m), "· by size") {
		t.Errorf("status bar lacks the sort:\n%s", plain(m))
	}
	checkSize(t, m, 100, 30)

	m = send(t, m, press("s"))
	if m.updates.bySize || m.updates.entries[0].pkg.Name != "linux-firmware" {
		t.Errorf("s again did not go back; first = %s", m.updates.entries[0].pkg.Name)
	}

	// Without the preview there are no sizes to sort by.
	opts := fixtureOptions()
	r := fixtures.Runner()
	r.Set("pacman -Sup --dbpath "+fixtures.SyncDBPath+" --logfile /dev/null --noconfirm --print-format %n %v %r %s",
		system.FakeResult{Code: 1, Stderr: []byte("error: boom")})
	opts.Runner = r
	m = loaded(t, opts, 100, 30)
	m = send(t, m, press("s"))
	if !m.flashErr || !strings.Contains(m.flashMsg, "preview") {
		t.Errorf("flash = %q", m.flashMsg)
	}
}

func TestLastUsedInTheDetailPane(t *testing.T) {
	opts, _, _ := realOptions()
	m := loaded(t, opts, 100, 30)

	m = selectPackage(t, m, "vim")
	if out := plain(m); !strings.Contains(out, "last used ~4 months ago") {
		t.Errorf("vim:\n%s", out)
	}
	m = selectPackage(t, m, "firefox")
	if out := plain(m); !strings.Contains(out, "last used 2 days ago") {
		t.Errorf("firefox:\n%s", out)
	}
	// No access times known: nothing shown, no placeholder.
	m = selectPackage(t, m, "ripgrep")
	if out := plain(m); strings.Contains(out, "last used") {
		t.Errorf("ripgrep shows a last used date:\n%s", out)
	}
	// Packages the upgrade adds aren't installed: nothing to look up.
	m = selectPackage(t, m, "iptables-nft")
	if out := plain(m); strings.Contains(out, "last used") {
		t.Errorf("new package shows last used:\n%s", out)
	}
}

func TestLastUsedHiddenUnderNoatime(t *testing.T) {
	opts, _, fsys := realOptions()
	_ = fsys.WriteFile("/proc/self/mounts", []byte("/dev/nvme0n1p2 / ext4 rw,noatime 0 0\n"), 0o644)
	m := loaded(t, opts, 100, 30)
	m = selectPackage(t, m, "vim")
	if out := plain(m); strings.Contains(out, "last used") {
		t.Errorf("noatime must hide last used:\n%s", out)
	}
}

func TestMarkAndRemoveFromUpdates(t *testing.T) {
	opts, _, _ := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	m := loaded(t, opts, 100, 30)

	m = selectPackage(t, m, "ripgrep")
	m = send(t, m, press("space"))
	if !m.pkgs.marked["ripgrep"] {
		t.Fatal("space did not mark ripgrep")
	}
	if out := plain(m); !strings.Contains(out, "marked for removal") || !strings.Contains(out, "1 marked") {
		t.Errorf("no mark shown:\n%s", out)
	}
	// New packages aren't installed yet and can't be marked.
	m = selectPackage(t, m, "iptables-nft")
	m = send(t, m, press("space"))
	if m.pkgs.marked["iptables-nft"] || len(m.pkgs.marked) != 1 {
		t.Errorf("marked = %v", m.pkgs.marked)
	}

	before := m.tx.DownloadBytes
	m = send(t, m, press("X"))
	if m.screen != screenConfirm || m.act == nil || m.act.plan.Blocked() {
		t.Fatalf("screen %v act %+v", m.screen, m.act)
	}
	if got := strings.Join(m.act.plan.Command, " "); !strings.HasPrefix(got, "sudo pacman -R") || !strings.HasSuffix(got, " -- ripgrep") {
		t.Errorf("hand-off = %q", got)
	}
	// The preview afterwards is of a system without ripgrep: a smaller download.
	r := m.opts.Runner.(*system.FakeRunner)
	r.Set("checkupdates", system.FakeResult{Stdout: []byte("vim 9.1.1800-1 -> 9.1.1850-1\n")})
	r.Set("pacman -Sup --dbpath "+fixtures.SyncDBPath+" --logfile /dev/null --noconfirm --print-format %n %v %r %s", system.FakeResult{Stdout: []byte("vim 9.1.1850-1 extra 1048576\n")})
	m = send(t, m, press("y"))
	if len(ex.argv) != 1 {
		t.Fatalf("ran %q", ex.argv)
	}
	if m.tx.DownloadBytes >= before || !strings.Contains(m.actionNotice, "saved") {
		t.Errorf("download %d (was %d), notice %q: want what the removal saved", m.tx.DownloadBytes, before, m.actionNotice)
	}
	if len(m.pkgs.marked) != 0 {
		t.Errorf("marks left: %v", m.pkgs.marked)
	}

	// X with nothing marked does nothing but say so.
	m = send(t, m, press("X"))
	if m.screen == screenConfirm || !strings.Contains(m.flashMsg, "mark packages") {
		t.Errorf("X with nothing marked: screen %v flash %q", m.screen, m.flashMsg)
	}
}

func TestPrefetchModalAndHandoff(t *testing.T) {
	opts, _, fsys := realOptions()
	ex := &fakeExec{}
	clock := fixtures.Now
	opts.Now = func() time.Time { return clock }
	opts.Exec = func(argv []string, done tea.ExecCallback) tea.Cmd {
		clock = clock.Add(5 * time.Minute) // the download takes 5 minutes
		return ex.exec(argv, done)
	}
	m := loaded(t, opts, 100, 30)

	m = send(t, m, press("F"))
	if m.screen != screenConfirm || m.act == nil || m.act.kind != ops.KindPrefetch {
		t.Fatalf("screen %v act %+v", m.screen, m.act)
	}
	out := plain(m)
	if !strings.Contains(out, "This runs: sudo checkupdates -d") || !strings.Contains(out, "installs nothing") {
		t.Errorf("modal:\n%s", out)
	}
	golden(t, "prefetch-modal", out)
	checkSize(t, m, 100, 30)
	// Only y proceeds.
	for _, k := range []string{"j", "n", "enter", "X"} {
		m = send(t, m, press(k))
	}
	if len(ex.argv) != 0 || m.screen != screenConfirm {
		t.Fatalf("ran %q on a stray key", ex.argv)
	}
	m = send(t, m, press("y"))
	if len(ex.argv) != 1 || strings.Join(ex.argv[0], " ") != "sudo checkupdates -d" {
		t.Fatalf("ran %q, want sudo checkupdates -d", ex.argv)
	}
	if !strings.Contains(m.actionNotice, "prefetched 886 MiB") {
		t.Errorf("notice = %q", m.actionNotice)
	}
	st := savedState(t, fsys)
	p := st.PrefetchSamples()
	if len(p) != 1 || p[0].Seconds != 300 || p[0].Bytes != fixtureDownload || p[0].Packages != 18 {
		t.Errorf("saved prefetch samples = %+v", p)
	}
	if len(st.UpgradeSamples()) != 0 {
		t.Errorf("a prefetch recorded an upgrade sample: %+v", st.UpgradeSamples())
	}
}

// fixtureDownload is the download the fixture preview reports.
const fixtureDownload = 929069795

func TestPrefetchFailureRecordsNothing(t *testing.T) {
	opts, _, fsys := realOptions()
	ex := &fakeExec{err: errors.New("exit status 1")}
	opts.Exec = ex.exec
	m := loaded(t, opts, 100, 30)
	m = send(t, m, press("F"))
	m = send(t, m, press("y"))
	if !m.actionErr || !strings.Contains(m.actionNotice, "prefetch failed") {
		t.Errorf("notice = %q err %v", m.actionNotice, m.actionErr)
	}
	if got := savedState(t, fsys).PrefetchSamples(); len(got) != 0 {
		t.Errorf("a failed prefetch was recorded: %+v", got)
	}
}

func TestPrefetchRefusedWithoutPreview(t *testing.T) {
	opts, r, _ := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	r.Set("pacman -Sup --dbpath "+fixtures.SyncDBPath+" --logfile /dev/null --noconfirm --print-format %n %v %r %s",
		system.FakeResult{Code: 1, Stderr: []byte("error: boom")})
	m := loaded(t, opts, 100, 30)
	m = send(t, m, press("F"))
	if m.screen == screenConfirm || !m.flashErr || !strings.Contains(m.flashMsg, "preview unavailable") {
		t.Errorf("screen %v flash %q", m.screen, m.flashMsg)
	}
	if len(ex.argv) != 0 {
		t.Errorf("ran %q", ex.argv)
	}
}

func TestPrefetchNothingToDownload(t *testing.T) {
	opts, r, _ := realOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	r.Set("checkupdates", fixtures.CheckupdatesNoneResult())
	r.Set("pacman -Sup --dbpath "+fixtures.SyncDBPath+" --logfile /dev/null --noconfirm --print-format %n %v %r %s", system.FakeResult{})
	m := loaded(t, opts, 100, 30)
	m = send(t, m, press("F"))
	if m.screen == screenConfirm || !strings.Contains(m.flashMsg, "nothing to download") {
		t.Errorf("screen %v flash %q", m.screen, m.flashMsg)
	}
}

func TestPrefetchWithoutRootWhenCacheIsWritable(t *testing.T) {
	opts, _, fsys := realOptions()
	fsys.SetWritable(fixtures.CacheDir)
	m := loaded(t, opts, 100, 30)
	m = send(t, m, press("F"))
	if got := strings.Join(m.act.plan.Command, " "); got != "checkupdates -d" {
		t.Errorf("command = %q, want no privilege command", got)
	}
	// prefetch_command replaces it entirely.
	cfg := m.cfg
	cfg.PrefetchCommand = "doas checkupdates -d"
	opts.Config = &cfg
	m = loaded(t, opts, 100, 30)
	m = send(t, m, press("F"))
	if got := strings.Join(m.act.plan.Command, " "); got != "doas checkupdates -d" {
		t.Errorf("command = %q", got)
	}
}

func TestPrefetchInDemoRunsNothing(t *testing.T) {
	opts := fixtureOptions()
	ex := &fakeExec{}
	opts.Exec = ex.exec
	m := loaded(t, opts, 100, 30)
	m = send(t, m, press("F"))
	m = send(t, m, press("y"))
	if len(ex.argv) != 0 || !strings.Contains(m.flashMsg, "demo: nothing is run") {
		t.Errorf("ran %q, flash %q", ex.argv, m.flashMsg)
	}
}

func TestPrefetchShrinksTheDownload(t *testing.T) {
	opts, r, _ := realOptions()
	opts.Exec = (&fakeExec{}).exec
	m := loaded(t, opts, 100, 30)
	if m.bw.Prefetched != 0 {
		t.Fatalf("prefetched = %d before", m.bw.Prefetched)
	}
	// After the prefetch everything is in the cache: the print lists size 0.
	var out strings.Builder
	for l := range strings.Lines(string(fixtures.Bytes(fixtures.PacmanSup))) {
		f := strings.Fields(l)
		f[len(f)-1] = "0"
		out.WriteString(strings.Join(f, " ") + "\n")
	}
	r.Set("pacman -Sup --dbpath "+fixtures.SyncDBPath+" --logfile /dev/null --noconfirm --print-format %n %v %r %s", system.FakeResult{Stdout: []byte(out.String())})
	m = send(t, m, press("F"))
	m = send(t, m, press("y"))
	if m.tx.DownloadBytes != 0 || m.bw.Prefetched == 0 {
		t.Fatalf("download %d, prefetched %d", m.tx.DownloadBytes, m.bw.Prefetched)
	}
	if line := strings.Split(plain(m), "\n")[2]; !strings.Contains(line, "already downloaded") {
		t.Errorf("header = %q, want the already-downloaded part", line)
	}
}

func TestMeteredConnection(t *testing.T) {
	opts, r, _ := realOptions()
	opts.Disk = fixtures.Disk()
	r.Set(fixtures.NmcliMeteredCommand, system.FakeResult{Stdout: fixtures.Bytes(fixtures.NmcliMeteredGuessed)})
	opts.Exec = (&fakeExec{}).exec
	m := loaded(t, opts, 100, 30)
	if !m.metered() {
		t.Fatalf("no metered problem: %+v", m.pf.problems)
	}
	// The upgrade and prefetch modals repeat it.
	m = send(t, m, press("U"))
	if !strings.Contains(plain(m), "is metered and the upgrade downloads 886 MiB") {
		t.Errorf("upgrade modal:\n%s", plain(m))
	}
	m = send(t, m, press("esc"))
	m = send(t, m, press("F"))
	out := plain(m)
	if !strings.Contains(out, "is metered") {
		t.Errorf("prefetch modal:\n%s", out)
	}
	golden(t, "prefetch-metered", out)

	// Not metered: no problem.
	r.Set(fixtures.NmcliMeteredCommand, system.FakeResult{Stdout: fixtures.Bytes(fixtures.NmcliMeteredNo)})
	m = loaded(t, opts, 100, 30)
	if m.metered() {
		t.Error("metered problem on an unmetered connection")
	}
}

func TestMeteredCheckOff(t *testing.T) {
	opts, r, _ := realOptions()
	c := config.Default()
	c.MeteredWarnMiB = 0
	opts.Config = &c
	loaded(t, opts, 100, 30)
	for _, call := range r.Calls() {
		if strings.HasPrefix(call, "nmcli") {
			t.Errorf("metered_warn_mib = 0 still ran %q", call)
		}
	}
}

func TestHousekeepingParallelDownloadsHint(t *testing.T) {
	opts, _, fsys := realOptions()
	m := loaded(t, opts, 100, 30)
	m = send(t, m, press("5"))
	if !strings.Contains(plain(m), "set `ParallelDownloads = 5`") {
		t.Errorf("housekeeping lacks the hint:\n%s", plain(m))
	}
	_ = fsys.WriteFile("/etc/pacman.conf", []byte("[options]\nParallelDownloads = 5\n"), 0o644)
	m = loaded(t, opts, 100, 30)
	m = send(t, m, press("5"))
	if strings.Contains(plain(m), "ParallelDownloads") {
		t.Errorf("hint shown although ParallelDownloads = 5:\n%s", plain(m))
	}
}

func TestUpgradeTimingIsRecorded(t *testing.T) {
	opts, _, fsys := realOptions()
	ex := &fakeExec{}
	clock := fixtures.Now
	opts.Now = func() time.Time { return clock }
	opts.Exec = func(argv []string, done tea.ExecCallback) tea.Cmd {
		clock = clock.Add(12 * time.Minute)
		return ex.exec(argv, done)
	}
	m := loaded(t, opts, 100, 30)
	m = send(t, m, press("U"))
	send(t, m, press("y"))
	u := savedState(t, fsys).UpgradeSamples()
	if len(u) != 1 || u[0].Seconds != 720 || u[0].Packages != 18 || u[0].Bytes != fixtureDownload {
		t.Errorf("saved upgrade samples = %+v", u)
	}

	// A failed upgrade says nothing about how long a good one takes.
	opts2, _, fsys2 := realOptions()
	ex2 := &fakeExec{err: errors.New("exit status 1")}
	clock = fixtures.Now
	opts2.Now = opts.Now
	opts2.Exec = func(argv []string, done tea.ExecCallback) tea.Cmd {
		clock = clock.Add(12 * time.Minute)
		return ex2.exec(argv, done)
	}
	m = loaded(t, opts2, 100, 30)
	m = send(t, m, press("U"))
	send(t, m, press("y"))
	if got := savedState(t, fsys2).UpgradeSamples(); len(got) != 0 {
		t.Errorf("a failed upgrade was recorded: %+v", got)
	}
}

func TestPendingSizeIsRecordedOnceADay(t *testing.T) {
	opts, _, fsys := realOptions()
	m := loaded(t, opts, 100, 30)
	got := savedState(t, fsys).PendingSizes()
	if len(got) != 1 || got[0].Day != "2026-10-23" || got[0].Bytes != fixtureDownload {
		t.Fatalf("pending sizes = %+v", got)
	}
	// A refresh the same day changes nothing.
	m = send(t, m, press("r"))
	if got := m.opts.State.PendingSizes(); len(got) != 1 {
		t.Errorf("same-day refresh: %+v", got)
	}
}
