package duration

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"90d", 90 * 24 * time.Hour},
		{"24h", 24 * time.Hour},
		{"2w", 14 * 24 * time.Hour},
		{"1mo", 30 * 24 * time.Hour},
		{"1y", 365 * 24 * time.Hour},
		{"7", 7 * 24 * time.Hour}, // bare integer defaults to days
		{" 5m-invalid ", 0},
	}
	for _, tc := range tests {
		got, err := Parse(tc.in)
		if tc.in == " 5m-invalid " {
			if err == nil {
				t.Fatalf("minutes are not in the grammar: %q must fail", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("Parse(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseTunable(t *testing.T) {
	for _, s := range []string{"5m", "600s", "2m30s"} {
		if _, err := ParseTunable(s); err != nil {
			t.Fatalf("ParseTunable(%q): %v", s, err)
		}
	}
}

func TestRejections(t *testing.T) {
	for _, s := range []string{"", "1.5d", "1h30m", "30s", "-2d", "d", "1D", "mo"} {
		if _, err := Parse(s); err == nil {
			t.Fatalf("Parse(%q) must fail", s)
		}
	}
}
