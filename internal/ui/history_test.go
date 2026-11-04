package ui

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/leNITHISH/cronen/internal/config"
	"strings"
	"testing"
)

func historyScreen(t *testing.T, opts Options) Model {
	t.Helper()
	return send(t, loaded(t, opts, 100, 30), press("4"))
}

// selectTx moves the History selection to the transaction whose command
// line contains cmd.
func selectTx(t *testing.T, m Model, cmd string) Model {
	t.Helper()
	for i, r := range m.hist.rows {
		if r.change < 0 && strings.Contains(m.hist.txs[r.tx].Command, cmd) {
			m.hist.sel, m.hist.selKey = i, r.key()
			m.hist.scroll(m.bodyHeight())
			m.refreshDetail()
			return m
		}
	}
	t.Fatalf("no transaction %q", cmd)
	return m
}

func TestHistoryScreen(t *testing.T) {
	opts, _, _ := realOptions()
	m := historyScreen(t, opts)
	// Newest first: the firefox downgrade is on top.
	if len(m.hist.txs) != 9 {
		t.Fatalf("%d transactions, want 9", len(m.hist.txs))
	}
	if first := m.hist.txs[0]; !strings.Contains(first.Command, "firefox-143.0-1") {
		t.Errorf("newest = %+v", first)
	}
	golden(t, "history", plain(m))
	checkSize(t, m, 100, 30)

	// The cut-off transaction is marked, and its old kernel is in the cache.
	m = selectTx(t, m, "pacman -Syu")
	for i, r := range m.hist.rows {
		if m.hist.txs[r.tx].Interrupted {
			m.hist.sel, m.hist.selKey = i, r.key()
		}
	}
	m.refreshDetail()
	out := plain(m)
	row, _ := m.hist.selected()
	flat := strings.Join(strings.Fields(ansi.Strip(m.historyDetail(row, 100))), " ")
	for _, want := range []string{"interrupted", "● the old version is still in the package cache", "↑ linux 6.17.2.arch1-1 → 6.17.3.arch1-1 ●"} {
		if !strings.Contains(flat, want) {
			t.Errorf("detail lacks %q:\n%s", want, out)
		}
	}
	golden(t, "history-interrupted", out)
	checkSize(t, m, 100, 30)
}

func TestHistoryPackageFilter(t *testing.T) {
	opts, _, _ := realOptions()
	m := historyScreen(t, opts)
	m = send(t, m, press("/"))
	m = typeText(t, m, "nvidia-utils")
	m = send(t, m, press("enter"))
	if len(m.hist.rows) != 2 {
		t.Fatalf("%d rows for nvidia-utils, want 2", len(m.hist.rows))
	}
	for _, r := range m.hist.rows {
		if r.change < 0 {
			t.Errorf("transaction row in a package view: %+v", r)
		}
	}
	golden(t, "history-package", plain(m))
	checkSize(t, m, 100, 30)

	// A filter that matches nothing says so; esc clears it.
	m = send(t, m, press("esc"))
	if m.filterQuery() != "" || len(m.hist.rows) != 9 {
		t.Errorf("esc: filter %q, %d rows", m.filterQuery(), len(m.hist.rows))
	}
	m = send(t, m, press("/"))
	m = typeText(t, m, "zzz")
	if !strings.Contains(plain(m), "no package matches") {
		t.Errorf("no empty-state message:\n%s", plain(m))
	}
}

func TestHistoryOff(t *testing.T) {
	opts, runner, _ := realOptions()
	cfg := config.Default()
	cfg.History = false
	opts.Config = &cfg
	m := historyScreen(t, opts)
	if !strings.Contains(plain(m), "history is off") || m.histSec.loading {
		t.Errorf("history = false:\n%s", plain(m))
	}
	_ = runner
	m = send(t, m, press("D"))
	if m.screen == screenConfirm {
		t.Error("D opened a modal with history off")
	}
}

func TestHistoryUnreadableLog(t *testing.T) {
	opts, _, fsys := realOptions()
	fsys.ReadErr = errPermission
	m := historyScreen(t, opts)
	if !strings.Contains(plain(m), "✗") || m.histSec.err == nil {
		t.Errorf("a failed read should show an inline error:\n%s", plain(m))
	}
	// The rest of the UI is unaffected.
	m = send(t, m, press("1"))
	if !strings.Contains(plain(m), "REPO") {
		t.Errorf("updates broke:\n%s", plain(m))
	}
}
