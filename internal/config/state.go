package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/leNITHISH/cronen/internal/bandwidth"
	"github.com/leNITHISH/cronen/internal/system"
)

// stateVersion is written to the state file so the format can change later.
// Version 1 files load as they are: the fields version 2 added are absent.
const stateVersion = 2

// Maintainer is the AUR maintainer and co-maintainers cronen last accepted
// for a package base.
type Maintainer struct {
	PkgBase string `json:"pkgbase"`
	// Maintainer is empty for an orphaned package.
	Maintainer    string   `json:"maintainer"`
	CoMaintainers []string `json:"comaintainers,omitempty"`
}

// Acknowledgement records that the user acknowledged flags on a package for
// one exact new version.
type Acknowledgement struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// Rules are the IDs of the rules whose flags were acknowledged, sorted.
	Rules []string `json:"rules"`
}

// State is what cronen remembers between runs. The zero value is an empty
// state and is ready to use.
type State struct {
	// LastUpgrade is when cronen last ran a successful upgrade, or the
	// zero time if it never has.
	LastUpgrade time.Time
	readNews    map[string]bool
	maintainers map[string]Maintainer
	acks        map[string]Acknowledgement

	// The bandwidth helper's records: the last 10 timed upgrades and
	// prefetches, and the pending download size once per day for the last
	// 90 days.
	upgrades, prefetches []bandwidth.Sample
	sizes                []bandwidth.Size
}

type stateFile struct {
	Version     int        `json:"version"`
	ReadNews    []string   `json:"read_news"`
	LastUpgrade *time.Time `json:"last_upgrade,omitempty"`
	// Maintainers was added in version 2.
	Maintainers []Maintainer `json:"aur_maintainers,omitempty"`
	// Acknowledgements was added in version 2.
	Acknowledgements []Acknowledgement `json:"acknowledgements,omitempty"`
	// The bandwidth helper's records were added in version 2.
	UpgradeSamples  []bandwidth.Sample `json:"upgrade_samples,omitempty"`
	PrefetchSamples []bandwidth.Sample `json:"prefetch_samples,omitempty"`
	PendingSizes    []bandwidth.Size   `json:"pending_sizes,omitempty"`
}

// IsRead reports whether the news item with this GUID was marked read.
func (s *State) IsRead(guid string) bool {
	return s.readNews[guid]
}

// MarkRead marks news items as read.
func (s *State) MarkRead(guids ...string) {
	if s.readNews == nil {
		s.readNews = map[string]bool{}
	}
	for _, g := range guids {
		if g != "" {
			s.readNews[g] = true
		}
	}
}

// Clone returns a deep copy of s, safe to hand to another goroutine while
// s keeps changing.
func (s State) Clone() State {
	c := State{LastUpgrade: s.LastUpgrade}
	c.MarkRead(s.ReadNews()...)
	for _, m := range s.Maintainers() {
		c.SetMaintainer(m)
	}
	for _, a := range s.Acknowledgements() {
		c.Acknowledge(a.Name, a.Version, a.Rules...)
	}
	c.upgrades = slices.Clone(s.upgrades)
	c.prefetches = slices.Clone(s.prefetches)
	c.sizes = slices.Clone(s.sizes)
	return c
}

// UpgradeSamples returns the timed upgrade hand-offs, oldest first.
func (s State) UpgradeSamples() []bandwidth.Sample { return slices.Clone(s.upgrades) }

// PrefetchSamples returns the timed prefetches, oldest first.
func (s State) PrefetchSamples() []bandwidth.Sample { return slices.Clone(s.prefetches) }

// PendingSizes returns the daily samples of the pending download, oldest
// first.
func (s State) PendingSizes() []bandwidth.Size { return slices.Clone(s.sizes) }

// AddUpgradeSample records a timed upgrade, keeping the last
// bandwidth.KeepSamples. Samples that can't tell anything are ignored.
func (s *State) AddUpgradeSample(x bandwidth.Sample) {
	if validSample(x) {
		s.upgrades = keepLast(append(s.upgrades, x), bandwidth.KeepSamples)
	}
}

// AddPrefetchSample records a timed prefetch, like AddUpgradeSample.
func (s *State) AddPrefetchSample(x bandwidth.Sample) {
	if validSample(x) {
		s.prefetches = keepLast(append(s.prefetches, x), bandwidth.KeepSamples)
	}
}

func validSample(x bandwidth.Sample) bool {
	return x.Bytes >= 0 && x.Packages >= 0 && x.Seconds > 0
}

// RecordPending records the size of the pending download for a day (the
// "2006-01-02" form), replacing an earlier sample of the same day and
// keeping the last bandwidth.KeepSizes days. It reports whether s changed.
func (s *State) RecordPending(day string, bytes int64) bool {
	if _, err := time.Parse(bandwidth.DayFormat, day); err != nil || bytes < 0 {
		return false
	}
	if n := len(s.sizes); n > 0 && s.sizes[n-1].Day == day {
		if s.sizes[n-1].Bytes == bytes {
			return false
		}
		s.sizes = slices.Clone(s.sizes)
		s.sizes[n-1].Bytes = bytes
		return true
	}
	if n := len(s.sizes); n > 0 && s.sizes[n-1].Day > day {
		// A clock that went backwards: don't disturb the order.
		return false
	}
	s.sizes = keepLast(append(slices.Clone(s.sizes), bandwidth.Size{Day: day, Bytes: bytes}), bandwidth.KeepSizes)
	return true
}

func keepLast[T any](v []T, n int) []T {
	if len(v) > n {
		return slices.Clone(v[len(v)-n:])
	}
	return v
}

// ReadNews returns the GUIDs marked read, sorted.
func (s *State) ReadNews() []string {
	out := make([]string, 0, len(s.readNews))
	for g := range s.readNews {
		out = append(out, g)
	}
	slices.Sort(out)
	return out
}

// Maintainer returns the recorded maintainer of a package base.
func (s State) Maintainer(pkgbase string) (Maintainer, bool) {
	m, ok := s.maintainers[pkgbase]
	return m, ok
}

// SetMaintainer records m as the accepted maintainer of m.PkgBase. It is
// how a flagged change is cleared: after a successful upgrade, or when the
// user acknowledges it.
func (s *State) SetMaintainer(m Maintainer) {
	if m.PkgBase == "" {
		return
	}
	if s.maintainers == nil {
		s.maintainers = map[string]Maintainer{}
	}
	m.CoMaintainers = slices.Sorted(slices.Values(m.CoMaintainers))
	s.maintainers[m.PkgBase] = m
}

// Maintainers returns every recorded maintainer, sorted by package base.
func (s State) Maintainers() []Maintainer {
	out := make([]Maintainer, 0, len(s.maintainers))
	for _, m := range s.maintainers {
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b Maintainer) int { return strings.Compare(a.PkgBase, b.PkgBase) })
	return out
}

// Acknowledged returns the IDs of the rules acknowledged for exactly this
// version of the package. A different version has none: a new version
// brings its flags back.
func (s State) Acknowledged(name, version string) []string {
	a, ok := s.acks[name]
	if !ok || a.Version != version {
		return nil
	}
	return slices.Clone(a.Rules)
}

// Acknowledge records rules as acknowledged for the package at version,
// keeping the rules already acknowledged for that same version. Older
// versions' acknowledgements are replaced. It reports whether s changed.
func (s *State) Acknowledge(name, version string, rules ...string) bool {
	if name == "" || version == "" || len(rules) == 0 {
		return false
	}
	var merged []string
	if old, ok := s.acks[name]; ok && old.Version == version {
		merged = old.Rules
	}
	merged = slices.Compact(slices.Sorted(slices.Values(append(slices.Clone(merged), rules...))))
	if old, ok := s.acks[name]; ok && old.Version == version && slices.Equal(old.Rules, merged) {
		return false
	}
	if s.acks == nil {
		s.acks = map[string]Acknowledgement{}
	}
	s.acks[name] = Acknowledgement{Name: name, Version: version, Rules: merged}
	return true
}

// Acknowledgements returns every recorded acknowledgement, sorted by name.
func (s State) Acknowledgements() []Acknowledgement {
	out := make([]Acknowledgement, 0, len(s.acks))
	for _, a := range s.acks {
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b Acknowledgement) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// MarshalJSON implements json.Marshaler.
func (s State) MarshalJSON() ([]byte, error) {
	f := stateFile{
		Version: stateVersion, ReadNews: s.ReadNews(), Maintainers: s.Maintainers(), Acknowledgements: s.Acknowledgements(),
		UpgradeSamples: s.upgrades, PrefetchSamples: s.prefetches, PendingSizes: s.sizes,
	}
	if !s.LastUpgrade.IsZero() {
		t := s.LastUpgrade
		f.LastUpgrade = &t
	}
	return json.Marshal(f)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *State) UnmarshalJSON(data []byte) error {
	var f stateFile
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	if f.Version > stateVersion {
		return fmt.Errorf("state version %d is newer than supported version %d", f.Version, stateVersion)
	}
	*s = State{}
	if f.LastUpgrade != nil {
		s.LastUpgrade = *f.LastUpgrade
	}
	s.MarkRead(f.ReadNews...)
	for _, m := range f.Maintainers {
		s.SetMaintainer(m)
	}
	for _, a := range f.Acknowledgements {
		s.Acknowledge(a.Name, a.Version, a.Rules...)
	}
	for _, x := range f.UpgradeSamples {
		s.AddUpgradeSample(x)
	}
	for _, x := range f.PrefetchSamples {
		s.AddPrefetchSample(x)
	}
	for _, x := range f.PendingSizes {
		s.RecordPending(x.Day, x.Bytes)
	}
	return nil
}

// LoadState reads the state file at path. A missing file yields an empty
// State and a nil error. An unreadable or corrupt file yields an empty
// State and an error; callers can carry on with it.
func LoadState(fsys system.FS, path string) (State, error) {
	data, err := fsys.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("reading state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("state %s: %w", path, err)
	}
	return s, nil
}

// SaveState atomically writes the state file at path, creating its
// directory if needed.
func SaveState(fsys system.FS, path string, s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := fsys.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing state: %w", err)
	}
	return nil
}
