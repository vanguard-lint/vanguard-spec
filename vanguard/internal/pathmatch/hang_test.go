package pathmatch

// Q-3.b-3: a malicious glob must fail fast or finish fast — never hang.
// Every adversarial input is bounded by a wall-clock deadline in a separate
// goroutine; a hang fails the test.

import (
	"strings"
	"testing"
	"time"
)

func boundedExpand(t *testing.T, glob string) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := Expand(glob)
		done <- err
	}()
	select {
	case err2 := <-done:
		_ = err2
	case <-time.After(2 * time.Second):
		t.Fatalf("glob expansion hung: %q", truncate(glob))
	}
}

func boundedCanonical(t *testing.T, glob string) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := Canonical(glob)
		done <- err
	}()
	select {
	case err2 := <-done:
		_ = err2
	case <-time.After(2 * time.Second):
		t.Fatalf("canonicalization hung: %q", truncate(glob))
	}
}

func truncate(s string) string {
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}

func TestAdversarialGlobsNeverHang(t *testing.T) {
	cases := []string{
		"src/{a{a{a{a{a{a{a{a{a{a",
		strings.Repeat("{a,b}", 40),
		strings.Repeat("x{", 500),
		"src/**/**/**/**/**/**/**/**/**/**/**",
		strings.Repeat("*", 500),
		"src/[z-a]/gone",
		strings.Repeat("[", 300),
		"src/{a..b}{c..d}{e..f}{g..h}{i..j}{k..l}",
		strings.Repeat("{", 200) + strings.Repeat("}", 200),
		"**/" + strings.Repeat("*/", 100) + "deep/**",
	}
	for _, glob := range cases {
		boundedExpand(t, glob)
		boundedCanonical(t, glob)
	}
}

// Q-3.b-3 companion: bounded expansion — the brace blowup that IS legal
// grammar must still terminate with a finite result set.
func TestLegalBraceExpansionBounded(t *testing.T) {
	glob := "src/{a,b}/{c,d}/{e,f}/*.go"
	done := make(chan struct{}, 1)
	var out []string
	go func() {
		out, _ = Expand(glob)
		done <- struct{}{}
	}()
	select {
	case err2 := <-done:
		_ = err2
		if len(out) != 8 {
			t.Fatalf("expected 8 expansions, got %d", len(out))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("legal brace expansion hung")
	}
}
