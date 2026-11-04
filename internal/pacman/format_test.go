package pacman

import "testing"

func TestFormatSize(t *testing.T) {
	tests := []struct {
		in         int64
		size, diff string
	}{
		{0, "0 B", "+0 B"},
		{512, "512 B", "+512 B"},
		{1536, "1.5 KiB", "+1.5 KiB"},
		{10 * 1024, "10 KiB", "+10 KiB"},
		{929069795, "886 MiB", "+886 MiB"},
		{27053259, "26 MiB", "+26 MiB"},
		{5 << 30, "5.0 GiB", "+5.0 GiB"},
		{-3 << 20, "3.0 MiB", "-3.0 MiB"},
	}
	for _, tt := range tests {
		if got := FormatSize(max(tt.in, -tt.in)); got != tt.size {
			t.Errorf("FormatSize(%d) = %q, want %q", tt.in, got, tt.size)
		}
		if got := FormatDelta(tt.in); got != tt.diff {
			t.Errorf("FormatDelta(%d) = %q, want %q", tt.in, got, tt.diff)
		}
	}
}
