package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/leNITHISH/cronen/internal/summary"
)

func TestDemoQuitsOnQ(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	code := run(ctx, []string{"--demo"}, &stdout, &stderr,
		tea.WithInput(strings.NewReader("q")),
		tea.WithWindowSize(80, 24),
	)
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	if ctx.Err() != nil {
		t.Fatal("program did not quit before the timeout")
	}
	if !strings.Contains(stdout.String(), "cronen") {
		t.Errorf("output does not contain the title: %q", stdout.String())
	}
}

func TestFlags(t *testing.T) {
	tests := []struct {
		args []string
		code int
	}{
		{[]string{"--help"}, 0},
		{[]string{"--version"}, 0},
		{[]string{"--waybar"}, 2},
		{[]string{"--check", "--json"}, 2},
		{[]string{"--bogus"}, 2},
		{[]string{"extra"}, 2},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(context.Background(), tt.args, &stdout, &stderr); code != tt.code {
				t.Fatalf("exit code %d, want %d; stderr: %s", code, tt.code, stderr.String())
			}
		})
	}
}

func TestHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d; stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"--check", "--json", "--waybar", "--demo", "--version", "30  an unread news item", "config.toml", "cronen(1)"} {
		if !strings.Contains(out, want) {
			t.Errorf("help does not mention %q:\n%s", want, out)
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("help wrote to stderr: %q", stderr.String())
	}
}

func TestVersion(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })
	version = "1.2.3"

	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"--version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d; stderr: %s", code, stderr.String())
	}
	if got := stdout.String(); got != "cronen 1.2.3\n" {
		t.Errorf("got %q", got)
	}
}

// The non-interactive modes run against the fixtures with --demo, so they
// can be tested end to end without touching the system.
func TestDemoCheck(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--demo", "--check"}, &stdout, &stderr)
	if code != summary.ExitIntervention {
		t.Errorf("exit code %d, want %d; stderr: %s", code, summary.ExitIntervention, stderr.String())
	}
	out := stdout.String()
	if !strings.HasPrefix(out, "18 updates (2 AUR) · 11 flagged · 2 unread news · 1 .pacnew\n") {
		t.Errorf("output:\n%s", out)
	}
	if !strings.Contains(out, "‼ manual intervention: linux-firmware") {
		t.Errorf("no intervention line:\n%s", out)
	}
}

func TestDemoJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"--demo", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d; stderr: %s", code, stderr.String())
	}
	const want = `{"updates":18,"aur":2,"flagged":11,"critical":1,"unread_news":2,"pacnew":1,"reboot":true}` + "\n"
	if got := stdout.String(); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestDemoWaybar(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"--demo", "--json", "--waybar"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d; stderr: %s", code, stderr.String())
	}
	var got struct{ Text, Alt, Tooltip, Class string }
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, stdout.String())
	}
	if got.Text != "18" || got.Class != "critical" || got.Alt != "critical" {
		t.Errorf("got %+v", got)
	}
	if !strings.Contains(got.Tooltip, "linux-firmware &gt;= 20251021-1") {
		t.Errorf("tooltip not Pango-escaped: %q", got.Tooltip)
	}
}

// In demo mode the upgrade hand-off runs nothing and the TUI carries on.
func TestDemoUpgradeRunsNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	code := run(ctx, []string{"--demo"}, &stdout, &stderr,
		tea.WithInput(strings.NewReader("Uyq")),
		tea.WithWindowSize(100, 30),
	)
	if code != 0 || ctx.Err() != nil {
		t.Fatalf("exit code %d, ctx %v, stderr: %s", code, ctx.Err(), stderr.String())
	}
}
