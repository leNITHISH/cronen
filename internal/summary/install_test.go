package summary

import (
	"context"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/fixtures"
	"github.com/leNITHISH/cronen/internal/ops"
)

func installGuard() ops.Guard {
	return ops.Guard{RealDB: fixtures.RealDBPath, ArchiveBase: fixtures.ArchiveBase}
}

func TestResolveInstall(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.AURHelper = "paru"
	res, err := ResolveInstall(ctx, fixtures.Runner(), fixtures.Fetcher(), fixtures.FS(), cfg, fixtures.SyncDBPath, []string{"htop", "paru"})
	if err != nil || strings.Join(res.Repo, ",") != "htop" || strings.Join(res.AUR, ",") != "paru" {
		t.Fatalf("got %+v, %v", res, err)
	}

	cfg.AURHelper = ""
	_, err = ResolveInstall(ctx, fixtures.Runner(), fixtures.Fetcher(), fixtures.FS(), cfg, fixtures.SyncDBPath, []string{"paru"})
	if err == nil || !strings.Contains(err.Error(), "aur_helper") {
		t.Fatalf("aur without a helper: %v", err)
	}
	_, err = ResolveInstall(ctx, fixtures.Runner(), fixtures.Fetcher(), fixtures.FS(), cfg, fixtures.SyncDBPath, []string{"nosuchpkg"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown: %v", err)
	}
	cfg.AUR = false
	_, err = ResolveInstall(ctx, fixtures.Runner(), fixtures.Fetcher(), fixtures.FS(), cfg, fixtures.SyncDBPath, []string{"nosuchpkg"})
	if err == nil || !strings.Contains(err.Error(), "not in the repositories") {
		t.Fatalf("aur off: %v", err)
	}
}

func TestResolveInstallRefusesBeforeAnyCommand(t *testing.T) {
	for _, names := range [][]string{nil, {"-Syu"}, {"a b"}, {"A"}, {"htop", "htop"}} {
		r := fixtures.Runner()
		if _, err := ResolveInstall(context.Background(), r, fixtures.Fetcher(), fixtures.FS(), config.Default(), fixtures.SyncDBPath, names); err == nil {
			t.Errorf("%q accepted", names)
		}
		if len(r.Calls()) != 0 {
			t.Errorf("%q ran %v", names, r.Calls())
		}
	}
	// No checkupdates database: unavailable, nothing run.
	r := fixtures.Runner()
	if _, err := ResolveInstall(context.Background(), r, fixtures.Fetcher(), fixtures.FS(), config.Default(), "/nonexistent/db", []string{"htop"}); err == nil || len(r.Calls()) != 0 {
		t.Errorf("no database: %v %v", err, r.Calls())
	}
}

func TestPlanUpgradeInstall(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	r := fixtures.Runner()
	p, err := PlanUpgradeInstall(ctx, r, cfg, installGuard(), fixtures.SyncDBPath, Resolution{Repo: []string{"htop"}})
	if err != nil || p.Blocked() {
		t.Fatalf("got %+v, %v", p, err)
	}
	if strings.Join(p.Command, " ") != "sudo pacman -Syu -- htop" || p.Items[0].Name != "htop" || p.Packages != 20 {
		t.Errorf("plan: %v, %d packages", p.Command, p.Packages)
	}
	// The dry run is the last thing run: -Sup, never a sync.
	calls := r.Calls()
	if got := calls[len(calls)-1]; !strings.HasPrefix(got, "pacman -Sup --dbpath "+fixtures.SyncDBPath) {
		t.Errorf("ran %s", got)
	}

	// With an AUR helper the hand-off is the helper's, AUR names included.
	cfg.AURHelper = "yay"
	p, err = PlanUpgradeInstall(ctx, fixtures.Runner(), cfg, installGuard(), fixtures.SyncDBPath, Resolution{Repo: []string{"htop"}, AUR: []string{"paru"}})
	if err != nil || p.Blocked() || strings.Join(p.Command, " ") != "yay -Syu -- htop paru" {
		t.Fatalf("helper: %+v, %v", p, err)
	}

	// A dry run that fails leaves nothing to run.
	p, err = PlanUpgradeInstall(ctx, fixtures.Runner(), config.Default(), installGuard(), fixtures.SyncDBPath, Resolution{Repo: []string{"nosuchpkg"}})
	if err != nil || !p.Blocked() || p.Command != nil {
		t.Fatalf("failing dry run: %+v, %v", p, err)
	}
	// AUR names with no helper are an error before anything runs.
	r = fixtures.Runner()
	if _, err := PlanUpgradeInstall(ctx, r, config.Default(), installGuard(), fixtures.SyncDBPath, Resolution{AUR: []string{"paru"}}); err == nil || len(r.Calls()) != 0 {
		t.Fatalf("aur without helper: %v %v", err, r.Calls())
	}
}
