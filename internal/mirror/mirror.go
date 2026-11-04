// Package mirror finds out how far behind the mirror pacman downloads from
// is. A stale mirror makes checkupdates (and so cronen) miss updates.
package mirror

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/leNITHISH/cronen/internal/system"
)

// MirrorlistPath is pacman's mirror list.
const MirrorlistPath = "/etc/pacman.d/mirrorlist"

// ReferenceRoot is the mirror the others are compared with.
const ReferenceRoot = "https://geo.mirror.pkgbuild.com"

// ErrNoMirror reports a mirrorlist with no usable http(s) server.
var ErrNoMirror = errors.New("no http(s) Server in the mirrorlist")

// Root returns the root of the first usable mirror in a mirrorlist: the
// first uncommented `Server =` line, skipping file:// mirrors, with the
// `/$repo/os/$arch` suffix stripped. Only http and https servers count,
// since cronen fetches from it.
//
// Needs checking on Arch: the Server line form on a current mirrorlist.
func Root(mirrorlist []byte) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(mirrorlist))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "Server" {
			continue
		}
		val = strings.TrimSpace(val)
		if i := strings.Index(val, "$repo"); i >= 0 {
			val = val[:i]
		}
		val = strings.TrimRight(val, "/")
		u, err := url.Parse(val)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			continue
		}
		return val, nil
	}
	return "", ErrNoMirror
}

// ParseLastUpdate parses a mirror's lastupdate file: the Unix time of the
// last change to the repositories that the mirror has.
//
// Needs checking on Arch: that the file holds just the timestamp.
func ParseLastUpdate(data []byte) (time.Time, error) {
	s := strings.TrimSpace(string(data))
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}, fmt.Errorf("bad lastupdate %q", truncate(s, 40))
	}
	return time.Unix(n, 0).UTC(), nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Result is how fresh the mirror is.
type Result struct {
	// Root is the mirror's root URL.
	Root string
	// Mirror and Reference are their lastupdate times.
	Mirror, Reference time.Time
}

// Lag is how far the mirror is behind the reference mirror. A mirror that
// is ahead of it isn't behind.
func (r Result) Lag() time.Duration {
	return max(0, r.Reference.Sub(r.Mirror))
}

// Check reads the mirrorlist and compares the first mirror's lastupdate
// with the reference mirror's. fresh bypasses the HTTP cache.
func Check(ctx context.Context, fsys system.FS, f system.Fetcher, fresh bool) (Result, error) {
	list, err := fsys.ReadFile(MirrorlistPath)
	if err != nil {
		return Result{}, fmt.Errorf("mirrorlist: %w", err)
	}
	root, err := Root(list)
	if err != nil {
		return Result{}, err
	}
	res := Result{Root: root}
	if res.Mirror, err = lastUpdate(ctx, f, root, fresh); err != nil {
		return Result{}, err
	}
	if res.Reference, err = lastUpdate(ctx, f, ReferenceRoot, fresh); err != nil {
		return Result{}, err
	}
	return res, nil
}

func lastUpdate(ctx context.Context, f system.Fetcher, root string, fresh bool) (time.Time, error) {
	body, err := f.Get(ctx, root+"/lastupdate", fresh)
	if err != nil {
		return time.Time{}, err
	}
	t, err := ParseLastUpdate(body)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s/lastupdate: %w", root, err)
	}
	return t, nil
}
