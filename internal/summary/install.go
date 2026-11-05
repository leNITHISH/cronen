package summary

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/leNITHISH/cronen/internal/aur"
	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/ops"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/system"
)

// Resolution says where each requested package comes from.
type Resolution struct {
	// Repo are the names found in the checkupdates database, AUR the ones
	// only the AUR has (usable only with an AUR helper).
	Repo, AUR []string
}

// All is every requested name, repo first.
func (r Resolution) All() []string { return append(slices.Clone(r.Repo), r.AUR...) }

// ResolveInstall validates names like every other name and looks each up in
// the checkupdates database at db (pacman -Si --dbpath), and in the AUR for
// the ones the repos don't have. A name that is nowhere, or only in the AUR
// without an aur_helper, is an error and nothing else happens.
func ResolveInstall(ctx context.Context, r system.Runner, f system.Fetcher, fsys system.FS, cfg config.Config, db string, names []string) (Resolution, error) {
	var res Resolution
	if err := ops.CheckInstallNames(names); err != nil {
		return res, err
	}
	if err := pacman.CheckSyncDB(fsys, db); err != nil {
		return res, err
	}
	var missing []string
	for _, n := range names {
		_, err := pacman.SyncInfo(ctx, r, db, []string{n}, time.UTC)
		var exitErr *system.ExitError
		switch {
		case err == nil:
			res.Repo = append(res.Repo, n)
		case errors.As(err, &exitErr) && bytes.Contains(exitErr.Stderr, []byte("was not found")):
			missing = append(missing, n)
		default:
			return res, fmt.Errorf("looking up %s: %w", n, err)
		}
	}
	if len(missing) == 0 {
		return res, nil
	}
	if !cfg.AUR || f == nil {
		return res, fmt.Errorf("not in the repositories: %s", strings.Join(missing, ", "))
	}
	infos, err := aur.NewClient(f, r).Info(ctx, missing, false)
	if err != nil {
		return res, fmt.Errorf("not in the repositories: %s (and the AUR lookup failed: %w)", strings.Join(missing, ", "), err)
	}
	var nowhere, needHelper []string
	for _, n := range missing {
		switch _, ok := infos[n]; {
		case !ok:
			nowhere = append(nowhere, n)
		case cfg.AURHelper == "":
			needHelper = append(needHelper, n)
		default:
			res.AUR = append(res.AUR, n)
		}
	}
	switch {
	case len(nowhere) > 0:
		return res, fmt.Errorf("not found in the repositories or the AUR: %s", strings.Join(nowhere, ", "))
	case len(needHelper) > 0:
		return res, fmt.Errorf("%s is only in the AUR; cronen is not an AUR helper, so set aur_helper = \"paru\" or \"yay\" in config.toml to install it", strings.Join(needHelper, ", "))
	}
	return res, nil
}

// PlanUpgradeInstall is option A: the dry run as the user (what -Syu plus the
// repo packages would do, from the checkupdates database), and the hand-off
// that would follow. The dry run is guarded before it runs.
func PlanUpgradeInstall(ctx context.Context, r system.Runner, cfg config.Config, g ops.Guard, db string, res Resolution) (ops.InstallPlan, error) {
	if len(res.AUR) > 0 && cfg.AURHelper == "" {
		return ops.InstallPlan{}, errors.New("AUR packages need aur_helper")
	}
	dry, err := ops.UpgradeDryArgv(db, res.Repo)
	if err != nil {
		return ops.InstallPlan{}, err
	}
	names := res.Repo
	if cfg.AURHelper != "" {
		names = res.All()
	}
	cmd, err := ops.UpgradeInstallArgv(cfg.PrivilegeArgv(), cfg.AURHelper, names)
	if err != nil {
		return ops.InstallPlan{}, err
	}
	in := ops.InstallPlanInput{Option: ops.OptionUpgrade, Names: res.Repo, AUR: res.AUR, DryRun: dry, Command: cmd, Guard: g}
	if err := g.Verify(dry); err != nil {
		in.Failed, in.RunErr = true, err
		return ops.BuildInstallPlan(in), nil
	}
	out, _, err := r.Run(ctx, dry[0], dry[1:]...)
	in.Out = out
	if err != nil {
		in.Failed, in.RunErr = true, err
		var exitErr *system.ExitError
		if errors.As(err, &exitErr) {
			in.Stderr = exitErr.Stderr
		}
	}
	return ops.BuildInstallPlan(in), nil
}
