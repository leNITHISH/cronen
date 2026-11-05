package bandwidth

import (
	"cmp"
	"fmt"
	"slices"
	"time"
)

// Sample is one measured hand-off: an upgrade (download and install) or a
// prefetch (download only).
type Sample struct {
	// Time is when the hand-off finished.
	Time time.Time `json:"time"`
	// Bytes is what it had to download.
	Bytes int64 `json:"bytes"`
	// Packages is how many packages it upgraded or fetched.
	Packages int `json:"packages"`
	// Seconds is how long the hand-off took.
	Seconds float64 `json:"seconds"`
}

// Size is the size of the pending download on one day.
type Size struct {
	// Day is the calendar day, "2006-01-02".
	Day string `json:"day"`
	// Bytes is the whole download, counting what is already cached.
	Bytes int64 `json:"bytes"`
}

// Limits on how much history state keeps.
const (
	KeepSamples = 10
	KeepSizes   = 90
)

// DayFormat is the layout of Size.Day.
const DayFormat = "2006-01-02"

// minBytes is the smallest download whose speed means anything: below it
// the time is mostly connection setup.
const minBytes = 1 << 20

// Behind returns how many whole days have passed since the last full
// upgrade. ok is false when that time is unknown.
func Behind(last, now time.Time) (days int, ok bool) {
	if last.IsZero() {
		return 0, false
	}
	d := now.Sub(last)
	if d < 0 {
		return 0, true
	}
	return int(d / (24 * time.Hour)), true
}

// minTrendSpan is how far apart the samples of a trend must be.
const minTrendSpan = 7

// Trend returns how fast the pending download grows, in bytes per week. It
// looks at the latest stretch of samples that never shrink: a drop means an
// upgrade (or a removal) emptied the backlog, and the stretch before it says
// nothing about what is piling up now. ok is false until that stretch spans
// at least a week, and when the download isn't growing. sizes must be in
// date order.
func Trend(sizes []Size) (perWeek int64, ok bool) {
	i := len(sizes) - 1
	if i < 1 {
		return 0, false
	}
	for i > 0 && sizes[i-1].Bytes <= sizes[i].Bytes {
		i--
	}
	first, last := sizes[i], sizes[len(sizes)-1]
	a, errA := time.Parse(DayFormat, first.Day)
	b, errB := time.Parse(DayFormat, last.Day)
	if errA != nil || errB != nil {
		return 0, false
	}
	span := int(b.Sub(a).Round(24*time.Hour) / (24 * time.Hour))
	if span < minTrendSpan || last.Bytes <= first.Bytes {
		return 0, false
	}
	return (last.Bytes - first.Bytes) * 7 / int64(span), true
}

// Estimate is the predicted time of an upgrade.
type Estimate struct {
	// OK is false when there are no samples to predict from.
	OK       bool
	Duration time.Duration
	// Prefetches and Upgrades count the samples the prediction used.
	Prefetches, Upgrades int
	// Combined reports that there was no prefetch to measure the download
	// speed with, so the speed of whole upgrades (install time included)
	// was used and no install time was added on top.
	Combined bool
	// DownloadOnly reports that only prefetches were known, so no install
	// time is in the prediction.
	DownloadOnly bool
}

// EstimateTime predicts how long an upgrade of bytes to download and
// packages to install takes. The model is deliberately simple:
//
//   - the download speed is the median over the prefetch samples (the best
//     data, since nothing else runs during them);
//   - the install time per package is the median, over the upgrade samples,
//     of what is left of an upgrade after its download, per package;
//   - without prefetches, the median speed of whole upgrades is used on its
//     own.
//
// Samples too small to measure a speed with are ignored for the speed, and
// medians keep one slow day (or one the user left sitting at a prompt) from
// ruling the estimate.
func EstimateTime(prefetch, upgrades []Sample, bytes int64, packages int) Estimate {
	speeds := func(ss []Sample) []float64 {
		var out []float64
		for _, s := range ss {
			if s.Bytes >= minBytes && s.Seconds >= 1 {
				out = append(out, float64(s.Bytes)/s.Seconds)
			}
		}
		return out
	}
	var e Estimate
	pre := speeds(prefetch)
	if len(pre) > 0 {
		speed := median(pre)
		e.Prefetches = len(pre)
		e.OK = true
		secs := float64(bytes) / speed
		var install []float64
		for _, s := range upgrades {
			if s.Packages <= 0 || s.Seconds <= 0 {
				continue
			}
			rest := s.Seconds - float64(s.Bytes)/speed
			install = append(install, max(0, rest)/float64(s.Packages))
		}
		if len(install) > 0 {
			e.Upgrades = len(install)
			secs += median(install) * float64(packages)
		} else {
			e.DownloadOnly = true
		}
		e.Duration = seconds(secs)
		return e
	}
	whole := speeds(upgrades)
	if len(whole) == 0 {
		return Estimate{}
	}
	e.OK, e.Combined, e.Upgrades = true, true, len(whole)
	e.Duration = seconds(float64(bytes) / median(whole))
	return e
}

func seconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}

// median returns the middle value (the mean of the two middle ones for an
// even count) of a non-empty slice.
func median(v []float64) float64 {
	v = slices.Clone(v)
	slices.SortFunc(v, cmp.Compare[float64])
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}

// NoEstimate is shown instead of a guess when there are no samples.
const NoEstimate = "no estimate yet"

// Text is the estimate as shown to the user: "~14 min (from your last 6
// upgrades)", or NoEstimate.
func (e Estimate) Text() string {
	if !e.OK {
		return NoEstimate
	}
	var basis string
	switch {
	case e.Combined:
		basis = "from your last " + count(e.Upgrades, "upgrade", "upgrades")
	case e.DownloadOnly:
		basis = "download only, from your last " + count(e.Prefetches, "prefetch", "prefetches")
	default:
		basis = "from your last " + count(e.Prefetches, "prefetch", "prefetches") + " and " + count(e.Upgrades, "upgrade", "upgrades")
	}
	d := Duration(e.Duration)
	if e.Duration >= 30*time.Second {
		d = "~" + d
	}
	return d + " (" + basis + ")"
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// Duration formats a duration roughly: "<1 min", "14 min", "1 h 20 min".
func Duration(d time.Duration) string {
	min := int((d + 30*time.Second) / time.Minute)
	switch {
	case d < 30*time.Second:
		return "<1 min"
	case min < 60:
		return fmt.Sprintf("%d min", min)
	}
	// Past an hour, five minutes is the resolution that means anything.
	min = (min + 2) / 5 * 5
	if min%60 == 0 {
		return fmt.Sprintf("%d h", min/60)
	}
	return fmt.Sprintf("%d h %d min", min/60, min%60)
}
