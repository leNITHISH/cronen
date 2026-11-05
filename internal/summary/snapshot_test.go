package summary

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/preflight"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/snapshot"
	"github.com/leNITHISH/cronen/internal/system"
)

func snapSources(kit fixtures.SnapshotKit, mut func(*config.Config, *config.State)) (SnapshotSources, *system.FakeRunner) {
	r := fixtures.WithSnapshots(fixtures.Runner(), kit)
	fsys := fixtures.WithTimeshiftConfig(fixtures.FS())
	cfg := config.Default()
	var st config.State
	if mut != nil {
		mut(&cfg, &st)
	}
	return SnapshotSources{Runner: r, FS: fsys, Disk: fixtures.Disk(), Config: cfg, State: st, Location: time.UTC}, r
}

func noSudo(t *testing.T, r *system.FakeRunner) {
	t.Helper()
	for _, c := range r.Calls() {
		if strings.HasPrefix(c, "sudo") || strings.HasPrefix(c, "doas") || strings.HasPrefix(c, "run0") {
			t.Errorf("ran %q", c)
		}
	}
}

func TestNoToolDetectedIsAQuietFirstClassState(t *testing.T) {
	// The default fixtures have neither tool installed, like the
	// maintainer's machine.
	src, r := snapSources(fixtures.SnapshotKit{}, nil)
	info, err := LoadSnapshots(context.Background(), src)
	if err != nil || info.Found() || info.Note != "" || info.ListErr != nil || info.Detect.Missing != "" {
		t.Fatalf("info %+v err %v", info, err)
	}
	if info.Offered(config.SnapshotAsk) || info.DefaultTake(config.SnapshotAlways) || info.Deletable() {
		t.Error("offered something with no tool")
	}
	if _, ok := SnapshotProblem(info, src.Config, fixtures.Now); ok {
		t.Error("a problem without a tool")
	}
	if ps := AddSnapshot(nil, info, src.Config, fixtures.Now); len(ps) != 0 {
		t.Errorf("problems %v", ps)
	}
	// Only the two version probes were run.
	if got := strings.Join(r.Calls(), "|"); got != "timeshift --version|snapper --version" {
		t.Errorf("calls %q", got)
	}

	// Through Gather: no error, no problem, no JSON field, nothing for the
	// exit code to notice.
	full := fixtureSources()
	full.State.MarkRead(firmwareGUID, nvidiaGUID)
	s := Gather(context.Background(), full)
	if len(s.Errors) != 0 || s.Snapshot.Found() {
		t.Fatalf("errors %v snapshot %+v", s.Errors, s.Snapshot)
	}
	for _, p := range s.Preflight {
		if p.Check == preflight.CheckSnapshot {
			t.Errorf("snapshot problem: %+v", p)
		}
	}
	if strings.Contains(string(s.JSON()), "last_snapshot") {
		t.Errorf("json: %s", s.JSON())
	}
}

func TestSnapshotsSwitchOffRunsNothing(t *testing.T) {
	src, r := snapSources(fixtures.SnapshotKit{Timeshift: true}, func(c *config.Config, _ *config.State) { c.Snapshots = false })
	info, err := LoadSnapshots(context.Background(), src)
	if err != nil || info.Found() || len(r.Calls()) != 0 {
		t.Errorf("info %+v err %v calls %v", info, err, r.Calls())
	}
}

func TestTimeshiftWithoutListSudoNeverRunsSudo(t *testing.T) {
	rec := config.SnapshotRecord{At: fixtures.Now.Add(-48 * time.Hour), Tool: "timeshift", Comment: "cronen: before -Syu (3 packages)"}
	src, r := snapSources(fixtures.SnapshotKit{Timeshift: true, SudoList: true}, func(_ *config.Config, st *config.State) {
		st.AddSnapshot(rec)
		st.AddSnapshot(config.SnapshotRecord{At: fixtures.Now, Tool: "snapper"}) // another tool's: not ours
	})
	info, err := LoadSnapshots(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	noSudo(t, r)
	if !info.Found() || info.Tool() != snapshot.Timeshift || info.Listed || len(info.Snapshots) != 0 {
		t.Fatalf("info %+v", info)
	}
	if !strings.Contains(info.Note, "snapshot_list_sudo") {
		t.Errorf("note %q", info.Note)
	}
	if len(info.Records) != 1 || !info.Last.Equal(rec.At) || !info.LastFromRecords || info.Deletable() {
		t.Errorf("records %v last %v", info.Records, info.Last)
	}
	if info.Mode != "rsync" || !info.Backup.Mounted == false {
		// the fixture mounts have no Timeshift backup device
		t.Logf("mode %q backup %+v", info.Mode, info.Backup)
	}
	for _, c := range r.Calls() {
		if strings.Contains(c, "--list") {
			t.Errorf("listed: %q", c)
		}
	}
}

func TestTimeshiftWithListSudo(t *testing.T) {
	src, r := snapSources(fixtures.SnapshotKit{Timeshift: true, SudoList: true}, func(c *config.Config, _ *config.State) { c.SnapshotListSudo = true })
	info, err := LoadSnapshots(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Listed || len(info.Snapshots) != 3 || !info.Deletable() || info.Note != "" {
		t.Fatalf("info %+v", info)
	}
	if want := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC); !info.Last.Equal(want) || info.LastFromRecords {
		t.Errorf("last %v fromRecords %v", info.Last, info.LastFromRecords)
	}
	var sudo []string
	for _, c := range r.Calls() {
		if strings.HasPrefix(c, "sudo") {
			sudo = append(sudo, c)
		}
	}
	if len(sudo) != 1 || sudo[0] != "sudo -n timeshift --list" {
		t.Errorf("sudo calls %q", sudo)
	}
}

func TestTimeshiftListSudoDeniedIsANoteNotAnError(t *testing.T) {
	src, _ := snapSources(fixtures.SnapshotKit{Timeshift: true, SudoDenied: true}, func(c *config.Config, _ *config.State) { c.SnapshotListSudo = true })
	info, err := LoadSnapshots(context.Background(), src)
	if err != nil || info.ListErr != nil || info.Listed {
		t.Fatalf("info %+v err %v", info, err)
	}
	if !strings.Contains(info.Note, "passwordless") || !strings.Contains(info.Note, "a password is required") {
		t.Errorf("note %q", info.Note)
	}
}

func TestSnapperListing(t *testing.T) {
	src, r := snapSources(fixtures.SnapshotKit{Snapper: true}, nil)
	info, err := LoadSnapshots(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	noSudo(t, r)
	if info.Tool() != snapshot.Snapper || !info.Listed || len(info.Snapshots) != 4 || info.Mode != "" {
		t.Fatalf("info %+v", info)
	}
	if want := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC); !info.Last.Equal(want) {
		t.Errorf("last %v", info.Last)
	}
	// Timeshift is probed first, and absent.
	if r.Calls()[0] != "timeshift --version" {
		t.Errorf("calls %q", r.Calls())
	}
}

func TestSnapperPermissionDeniedIsAClearNoteNotAnEmptyList(t *testing.T) {
	src, r := snapSources(fixtures.SnapshotKit{Snapper: true, SnapperDenied: true}, nil)
	info, err := LoadSnapshots(context.Background(), src)
	if err != nil || info.ListErr != nil {
		t.Fatalf("err %v listErr %v", err, info.ListErr)
	}
	noSudo(t, r)
	if info.Listed || len(info.Snapshots) != 0 {
		t.Errorf("listed %v %v", info.Listed, info.Snapshots)
	}
	for _, w := range []string{"refused to list", "ALLOW_USERS", "No permissions"} {
		if !strings.Contains(info.Note, w) {
			t.Errorf("note %q lacks %q", info.Note, w)
		}
	}
}

func TestUnreadableListIsAFailedSourceNotAnEmptyList(t *testing.T) {
	for name, out := range map[string]string{
		"garbled": "Failure (something unexpected).\n",
		"cut off": " # | Type | Pre # | Date | User | Cleanup | Description | Userdata\n---+--+--+--+--+--+--+--\n1\n",
		"empty":   "",
	} {
		src, r := snapSources(fixtures.SnapshotKit{Snapper: true}, nil)
		r.Set("snapper -c root list", system.FakeResult{Stdout: []byte(out)})
		info, err := LoadSnapshots(context.Background(), src)
		if !errors.Is(err, snapshot.ErrUnreadable) || !errors.Is(info.ListErr, snapshot.ErrUnreadable) {
			t.Errorf("%s: err %v", name, err)
		}
		if info.Listed || len(info.Snapshots) != 0 {
			t.Errorf("%s: invented or hid snapshots: %+v", name, info.Snapshots)
		}
		if _, ok := SnapshotProblem(info, src.Config, fixtures.Now); ok {
			t.Errorf("%s: a problem about age from an unreadable list", name)
		}
	}
	// Through Gather it is a failed source: --check exits 1.
	full := fixtureSources()
	full.Runner = fixtures.WithSnapshots(fixtures.Runner(), fixtures.SnapshotKit{Snapper: true})
	full.Runner.(*system.FakeRunner).Set("snapper -c root list", system.FakeResult{Stdout: []byte("garbage\n")})
	s := Gather(context.Background(), full)
	if s.ExitCode() != ExitError {
		t.Errorf("exit %d, errors %v", s.ExitCode(), s.Errors)
	}
}

func TestSnapperOtherFailureIsAnError(t *testing.T) {
	src, r := snapSources(fixtures.SnapshotKit{Snapper: true}, nil)
	r.Set("snapper -c root list", system.FakeResult{Code: 1, Stderr: []byte("Failure (listing configs failed).\n")})
	info, err := LoadSnapshots(context.Background(), src)
	if err == nil || info.ListErr == nil || info.Note != "" || errors.Is(err, snapshot.ErrPermission) {
		t.Errorf("err %v info %+v", err, info)
	}
}

func TestHookDetection(t *testing.T) {
	for _, hook := range []string{"timeshift-autosnap", "snap-pac"} {
		src, _ := snapSources(fixtures.SnapshotKit{Timeshift: true, Hook: hook}, nil)
		info, err := LoadSnapshots(context.Background(), src)
		if err != nil || info.Hook() != hook {
			t.Fatalf("%s: %+v %v", hook, info.Hooks, err)
		}
		// "ask" skips the extra snapshot; "always" and "never" do what they say.
		if info.DefaultTake(config.SnapshotAsk) || !info.DefaultTake(config.SnapshotAlways) || info.DefaultTake(config.SnapshotNever) {
			t.Errorf("%s: DefaultTake wrong", hook)
		}
	}
	src, _ := snapSources(fixtures.SnapshotKit{Timeshift: true}, nil)
	info, _ := LoadSnapshots(context.Background(), src)
	if info.Hook() != "" || !info.DefaultTake(config.SnapshotAsk) || info.Offered(config.SnapshotNever) || !info.Offered(config.SnapshotAsk) {
		t.Errorf("no hook: %+v", info)
	}
}

func TestConfiguredToolMissing(t *testing.T) {
	src, _ := snapSources(fixtures.SnapshotKit{Timeshift: true}, func(c *config.Config, _ *config.State) { c.SnapshotTool = "snapper" })
	info, err := LoadSnapshots(context.Background(), src)
	if err != nil || info.Found() || !strings.Contains(info.Detect.Missing, "snapper isn't installed") {
		t.Errorf("%+v %v", info.Detect, err)
	}
	// Forcing a tool never probes the other.
	src, r := snapSources(fixtures.SnapshotKit{Timeshift: true, Snapper: true}, func(c *config.Config, _ *config.State) { c.SnapshotTool = "snapper" })
	_, _ = LoadSnapshots(context.Background(), src)
	if strings.Contains(strings.Join(r.Calls(), "|"), "timeshift --version") {
		t.Errorf("calls %q", r.Calls())
	}
}

func TestTimeshiftConfigAndBackupSpace(t *testing.T) {
	src, _ := snapSources(fixtures.SnapshotKit{Timeshift: true}, nil)
	mounts := "/dev/nvme0n1p2 /run/timeshift/4711/backup ext4 rw 0 0\n"
	if err := src.FS.WriteFile(preflight.MountsPath, []byte(mounts), 0o644); err != nil {
		t.Fatal(err)
	}
	src.Disk = system.NewFakeDisk(map[string]system.DiskUsage{"/run/timeshift/4711/backup": {FreeBytes: 212 << 30}})
	info, _ := LoadSnapshots(context.Background(), src)
	if info.Mode != "rsync" || !info.Backup.Mounted || info.Backup.Path != "/run/timeshift/4711/backup" || info.Backup.Free != 212<<30 {
		t.Errorf("mode %q backup %+v", info.Mode, info.Backup)
	}

	// Not mounted: no free space is invented.
	src, _ = snapSources(fixtures.SnapshotKit{Timeshift: true}, nil)
	info, _ = LoadSnapshots(context.Background(), src)
	if info.Backup.Mounted {
		t.Errorf("backup %+v", info.Backup)
	}

	// A config that can't be understood is reported, not fatal.
	src, _ = snapSources(fixtures.SnapshotKit{Timeshift: true}, nil)
	_ = src.FS.WriteFile(TimeshiftConfigPath, []byte("{ nope"), 0o644)
	info, err := LoadSnapshots(context.Background(), src)
	if err != nil || info.ConfigErr == nil || info.Mode != "" || !info.Found() {
		t.Errorf("err %v info %+v", err, info)
	}

	// And a missing one is no problem at all.
	src, _ = snapSources(fixtures.SnapshotKit{Timeshift: true}, nil)
	src.FS = fixtures.FS()
	info, err = LoadSnapshots(context.Background(), src)
	if err != nil || info.ConfigErr != nil || info.Mode != "" {
		t.Errorf("err %v info %+v", err, info)
	}
}

func TestSnapshotPreflightThroughGather(t *testing.T) {
	gather := func(kit fixtures.SnapshotKit, mut func(*Sources)) Summary {
		src := fixtureSources()
		src.State.MarkRead(firmwareGUID, nvidiaGUID)
		src.Runner = fixtures.WithSnapshots(fixtures.Runner(), kit)
		src.Config.AUR = false
		src.Config.SnapshotListSudo = true
		if mut != nil {
			mut(&src)
		}
		return Gather(context.Background(), src)
	}
	find := func(s Summary) *preflight.Problem {
		for i := range s.Preflight {
			if s.Preflight[i].Check == preflight.CheckSnapshot {
				return &s.Preflight[i]
			}
		}
		return nil
	}

	// Newest snapshot (2026-11-01) vs fixtures.Now: recent enough.
	s := gather(fixtures.SnapshotKit{Timeshift: true, SudoList: true}, nil)
	if p := find(s); p != nil {
		t.Errorf("recent snapshot flagged: %+v (now %v)", p, fixtures.Now)
	}
	var out struct {
		Last string `json:"last_snapshot"`
	}
	if err := json.Unmarshal(s.JSON(), &out); err != nil || out.Last != "2026-10-22T08:00:00Z" {
		t.Errorf("json %s", s.JSON())
	}

	// The same snapshot with a short limit: a warning, and the exit code is
	// not changed by it.
	base := gather(fixtures.SnapshotKit{}, func(src *Sources) { src.Config.SnapshotMaxAgeDays = 1 })
	s = gather(fixtures.SnapshotKit{Timeshift: true, SudoList: true}, func(src *Sources) { src.Config.SnapshotMaxAgeDays = 1 })
	p := find(s)
	if p == nil || p.Severity != risk.Warn || !strings.Contains(p.Message, "days old") {
		t.Fatalf("problem %+v (now %v)", p, fixtures.Now)
	}
	if s.ExitCode() != base.ExitCode() {
		t.Errorf("a warning changed the exit code: %d vs %d", s.ExitCode(), base.ExitCode())
	}

	// Timeshift that can't be listed and nothing recorded: info.
	s = gather(fixtures.SnapshotKit{Timeshift: true}, func(src *Sources) { src.Config.SnapshotListSudo = false })
	if p := find(s); p == nil || p.Severity != risk.Info || !strings.Contains(p.Message, "No Timeshift snapshot is known") {
		t.Errorf("problem %+v", p)
	}

	// With the preflight switch off, no problem is added.
	s = gather(fixtures.SnapshotKit{Timeshift: true}, func(src *Sources) { src.Config.Preflight = false })
	if find(s) != nil {
		t.Error("problem with preflight off")
	}
}
