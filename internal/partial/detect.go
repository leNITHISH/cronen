package partial

import (
	"slices"
	"time"

	"github.com/leNITHISH/cronen/internal/pacman"
)

// Transaction is a logged transaction that synced the databases without
// upgrading and installed something.
type Transaction struct {
	When    time.Time
	Command string
	// Packages are the packages it installed, upgraded or reinstalled,
	// sorted and without duplicates.
	Packages []string
}

// Find returns the partial-upgrade transactions in txs (any order),
// oldest first. A transaction counts when its command syncs without
// upgrading (see Command.SyncsWithoutUpgrade) and it installed or upgraded
// at least one package. Interrupted transactions count too: whatever they
// logged did happen.
func Find(txs []pacman.LogTransaction) []Transaction {
	var out []Transaction
	for _, tx := range txs {
		if t, ok := Check(tx); ok {
			out = append(out, t)
		}
	}
	slices.SortStableFunc(out, func(a, b Transaction) int { return a.When.Compare(b.When) })
	return out
}

// Check reports whether tx is a partial upgrade.
func Check(tx pacman.LogTransaction) (Transaction, bool) {
	c, ok := ParseCommand(tx.Command)
	if !ok || !c.SyncsWithoutUpgrade() {
		return Transaction{}, false
	}
	var pkgs []string
	for _, ch := range tx.Changes {
		switch ch.Action {
		case pacman.ActionInstalled, pacman.ActionUpgraded, pacman.ActionReinstalled:
			pkgs = append(pkgs, ch.Name)
		}
	}
	if len(pkgs) == 0 {
		return Transaction{}, false
	}
	slices.Sort(pkgs)
	return Transaction{When: tx.Start, Command: tx.Command, Packages: slices.Compact(pkgs)}, true
}

// Since returns the transactions of ts that came after last, the time of
// the last full upgrade. A zero last (unknown) keeps them all.
func Since(ts []Transaction, last time.Time) []Transaction {
	var out []Transaction
	for _, t := range ts {
		if last.IsZero() || t.When.After(last) {
			out = append(out, t)
		}
	}
	return out
}

// Packages returns the names every transaction of ts installed or
// upgraded, as a set.
func Packages(ts []Transaction) map[string]bool {
	set := map[string]bool{}
	for _, t := range ts {
		for _, p := range t.Packages {
			set[p] = true
		}
	}
	return set
}
