package summary

import (
	"context"
	"fmt"
	"time"

	"github.com/leNITHISH/cronen/internal/config"
	"github.com/leNITHISH/cronen/internal/mirror"
	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/preflight"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/system"
)

// LoadPreflight reads the system side of the preflight facts, for the
// pending repo updates. A nil repo (checkupdates failed) still gets the
// checks that don't depend on the updates.
func LoadPreflight(fsys system.FS, disk system.Disk, repo []pacman.Update) (preflight.Facts, error) {
	names := make([]string, len(repo))
	for i, u := range repo {
		names[i] = u.Name
	}
	return preflight.Gather(fsys, disk, names)
}

// LoadMirror compares the first mirror with the reference mirror.
func LoadMirror(ctx context.Context, fsys system.FS, f system.Fetcher, fresh bool) (mirror.Result, error) {
	res, err := mirror.Check(ctx, fsys, f, fresh)
	if err != nil {
		return res, fmt.Errorf("mirror: %w", err)
	}
	return res, nil
}

// AssessPreflight adds what the pending upgrade needs to the system facts
// and runs the checks. tx is the transaction preview (nil while it is
// unavailable, which skips the space checks that need it), mir the mirror
// result (nil if not checked), and last the time of the last full upgrade.
func AssessPreflight(facts preflight.Facts, tx *pacman.Transaction, mir *mirror.Result, last time.Time, cfg config.Config, now time.Time) []preflight.Problem {
	facts.Now = now
	facts.LastUpgrade = last
	if tx != nil {
		facts.Download, facts.SizeDelta = tx.DownloadBytes, tx.SizeDelta
	}
	if mir != nil {
		facts.MirrorKnown = true
		facts.MirrorRoot = mir.Root
		facts.MirrorLag = mir.Lag()
		facts.MirrorLimit = time.Duration(cfg.MirrorLagHours) * time.Hour
	}
	return preflight.Check(facts)
}

// CriticalProblems counts the critical problems among ps.
func CriticalProblems(ps []preflight.Problem) int {
	n := 0
	for _, p := range ps {
		if p.Severity == risk.Critical {
			n++
		}
	}
	return n
}
