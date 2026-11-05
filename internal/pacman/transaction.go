package pacman

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/leNITHISH/cronen/internal/system"
)

// UpdateDetail adds the transaction's numbers to one pending repo update.
type UpdateDetail struct {
	Update
	Repo string
	// DownloadSize is in bytes, 0 when the package is already cached.
	DownloadSize int64
	// OldSize and NewSize are installed sizes in bytes. OldSize is 0 when
	// pacman -Qi had nothing to say.
	OldSize, NewSize int64
	// BuildDate is when the new version was built, zero if unknown.
	BuildDate time.Time
	// OldProvides and NewProvides are the Provides of the installed and the
	// new version (pacman -Qi and -Si), for soname changes.
	OldProvides, NewProvides []string
}

// NewPackage is a package the upgrade pulls in: a new dependency or the
// replacement of an installed package.
type NewPackage struct {
	Name, Version, Repo string
	DownloadSize, Size  int64
	BuildDate           time.Time
	// NeededBy are the packages in the transaction that depend on it.
	NeededBy []string
	// Replaces are the installed packages it replaces.
	Replaces []string
}

// RemovedPackage is an installed package a replacement removes.
type RemovedPackage struct {
	Name, Version string
	Size          int64
	ReplacedBy    string
}

// Transaction is what `pacman -Syu` would do, as far as pacman's dry run
// and the sync database can tell.
type Transaction struct {
	// Updates are the pending repo updates, in the order pacman installs
	// them.
	Updates []UpdateDetail
	New     []NewPackage
	Removed []RemovedPackage
	// Held are the updates IgnorePkg or IgnoreGroup hold back. They are
	// not part of the transaction.
	Held []Held
	// DownloadBytes is the total download.
	DownloadBytes int64
	// PrefetchedBytes is the size of the packages already in the cache,
	// which the print lists with a download size of 0: what a prefetch (or
	// an earlier, interrupted upgrade) already brought in. The whole
	// download is DownloadBytes + PrefetchedBytes.
	PrefetchedBytes int64
	// SizeDelta is the change in installed size, in bytes: what the new and
	// upgraded packages take, minus what the replaced and removed ones
	// took.
	SizeDelta int64
}

// TransactionInput is everything BuildTransaction combines.
type TransactionInput struct {
	// Updates are the pending updates from checkupdates.
	Updates []Update
	// Printed is the -Sup print.
	Printed []Printed
	// Installed holds the names of every installed package.
	Installed map[string]bool
	// Sync is pacman -Si for the printed packages, Local is pacman -Qi for
	// the upgraded and replaced ones.
	Sync, Local map[string]Info
	Held        []Held
}

// BuildTransaction sorts the printed packages into updates and new
// packages, finds what the new ones replace and what they are needed by,
// and adds up the sizes.
func BuildTransaction(in TransactionInput) Transaction {
	old := map[string]Update{}
	for _, u := range in.Updates {
		old[u.Name] = u
	}
	printed := map[string]bool{}
	for _, p := range in.Printed {
		printed[p.Name] = true
	}

	tx := Transaction{Held: in.Held}
	for _, p := range in.Printed {
		tx.DownloadBytes += p.DownloadSize
		sync := in.Sync[p.Name]
		if p.DownloadSize == 0 {
			tx.PrefetchedBytes += sync.DownloadSize
		}
		tx.SizeDelta += sync.InstalledSize

		if in.Installed[p.Name] {
			u, ok := old[p.Name]
			if !ok {
				u = Update{Name: p.Name, OldVersion: in.Local[p.Name].Version, NewVersion: p.Version}
			}
			d := UpdateDetail{
				Update: u, Repo: p.Repo, DownloadSize: p.DownloadSize,
				OldSize: in.Local[p.Name].InstalledSize, NewSize: sync.InstalledSize,
				BuildDate:   sync.BuildDate,
				OldProvides: in.Local[p.Name].Provides, NewProvides: sync.Provides,
			}
			tx.SizeDelta -= d.OldSize
			tx.Updates = append(tx.Updates, d)
			continue
		}

		n := NewPackage{
			Name: p.Name, Version: p.Version, Repo: p.Repo,
			DownloadSize: p.DownloadSize, Size: sync.InstalledSize, BuildDate: sync.BuildDate,
			NeededBy: neededBy(p.Name, in),
		}
		for _, r := range sync.Replaces {
			gone := depName(r)
			if !in.Installed[gone] || printed[gone] {
				continue
			}
			n.Replaces = append(n.Replaces, gone)
			if !slices.ContainsFunc(tx.Removed, func(x RemovedPackage) bool { return x.Name == gone }) {
				loc := in.Local[gone]
				tx.Removed = append(tx.Removed, RemovedPackage{Name: gone, Version: loc.Version, Size: loc.InstalledSize, ReplacedBy: p.Name})
				tx.SizeDelta -= loc.InstalledSize
			}
		}
		tx.New = append(tx.New, n)
	}
	slices.SortStableFunc(tx.New, func(a, b NewPackage) int { return strings.Compare(a.Name, b.Name) })
	slices.SortStableFunc(tx.Removed, func(a, b RemovedPackage) int { return strings.Compare(a.Name, b.Name) })
	return tx
}

// neededBy lists the other printed packages whose Depends On names name or
// something it provides.
func neededBy(name string, in TransactionInput) []string {
	names := map[string]bool{name: true}
	for _, p := range in.Sync[name].Provides {
		names[depName(p)] = true
	}
	var by []string
	for _, p := range in.Printed {
		if p.Name == name {
			continue
		}
		for _, d := range in.Sync[p.Name].DependsOn {
			if names[depName(d)] {
				by = append(by, p.Name)
				break
			}
		}
	}
	slices.Sort(by)
	return by
}

// depName strips a version constraint from a dependency: "libfoo.so=1-64"
// and "bar>=2" become "libfoo.so" and "bar".
func depName(d string) string {
	if i := strings.IndexAny(d, "<>=:"); i >= 0 {
		return d[:i]
	}
	return d
}

// TotalBytes is the whole download of the upgrade, counting what is
// already in the cache.
func (t Transaction) TotalBytes() int64 {
	return t.DownloadBytes + t.PrefetchedBytes
}

// Packages is how many packages the upgrade installs: the upgraded and the
// new ones.
func (t Transaction) Packages() int {
	return len(t.Updates) + len(t.New)
}

// PreviewTransaction asks pacman what -Syu would do, against the database
// checkupdates synced at db, and combines the answers with the pending
// updates. Any failure is an error: a half-built preview would mislead.
func PreviewTransaction(ctx context.Context, r system.Runner, fsys system.FS, db string, updates []Update, loc *time.Location) (Transaction, error) {
	if err := CheckSyncDB(fsys, db); err != nil {
		return Transaction{}, err
	}
	printed, err := SyncPrint(ctx, r, db)
	if err != nil {
		return Transaction{}, err
	}
	installed, err := InstalledNames(ctx, r)
	if err != nil {
		return Transaction{}, err
	}
	held, err := HeldPackages(ctx, r, db)
	if err != nil {
		return Transaction{}, err
	}

	names := make([]string, len(printed))
	for i, p := range printed {
		names[i] = p.Name
	}
	sync, err := SyncInfo(ctx, r, db, names, loc)
	if err != nil {
		return Transaction{}, err
	}

	// The installed side: every package being upgraded, and every installed
	// package a new one replaces.
	var local []string
	for _, p := range printed {
		if installed[p.Name] {
			local = append(local, p.Name)
			continue
		}
		for _, rep := range sync[p.Name].Replaces {
			if gone := depName(rep); installed[gone] {
				local = append(local, gone)
			}
		}
	}
	var localInfo map[string]Info
	if len(local) > 0 {
		if localInfo, err = LocalInfo(ctx, r, local, loc); err != nil {
			return Transaction{}, err
		}
	}

	return BuildTransaction(TransactionInput{
		Updates: updates, Printed: printed, Installed: installed,
		Sync: sync, Local: localInfo, Held: held,
	}), nil
}
