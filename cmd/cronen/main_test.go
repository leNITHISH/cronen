package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
		{[]string{"--check"}, 1},
		{[]string{"--json"}, 1},
		{[]string{"--json", "--waybar"}, 1},
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
