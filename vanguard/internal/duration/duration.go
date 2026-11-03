// Package duration parses vanguard's fixed duration grammar: an integer plus
// one unit — h (hours), d (days, the default unit for a bare integer),
// w (weeks = 7d), mo (months = 30d), y (years = 365d). Months and years are
// fixed-length conversions: determinism beats calendar exactness. Bare
// seconds, floats, and multiple units are errors.
package duration

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseTunable parses an engine tunable duration (clock_skew_tolerance,
// per-binding timeout) using the standard Go duration grammar — these accept
// sub-hour units like 5m or 600s, unlike the review_after grammar above.
func ParseTunable(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}

// Parse converts a declared duration into a time.Duration.
func Parse(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	unit := "d"
	num := s
	switch {
	case strings.HasSuffix(s, "mo"):
		unit, num = "mo", s[:len(s)-2]
	case strings.HasSuffix(s, "h"), strings.HasSuffix(s, "d"),
		strings.HasSuffix(s, "w"), strings.HasSuffix(s, "y"):
		unit, num = s[len(s)-1:], s[:len(s)-1]
	}
	n, err := strconv.Atoi(num)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid duration %q: want an integer plus one unit (h, d, w, mo, y)", s)
	}
	switch unit {
	case "h":
		return time.Duration(n) * time.Hour, nil
	case "d":
		return time.Duration(n) * 24 * time.Hour, nil
	case "w":
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	case "mo":
		return time.Duration(n) * 30 * 24 * time.Hour, nil
	case "y":
		return time.Duration(n) * 365 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid duration %q", s)
	}
}
