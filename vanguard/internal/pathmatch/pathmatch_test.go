package pathmatch

import "testing"

// TestPathVectors covers the eleven mandatory vectors from the design
// (docs/designs/vanguard.md, "The check --path decision procedure").
func TestPathVectors(t *testing.T) {
	g := func(s string) string {
		c, err := Canonical(s)
		if err != nil {
			t.Fatalf("Canonical(%q): %v", s, err)
		}
		return c
	}
	tests := []struct {
		name  string
		glob  string
		p     string
		isDir bool
		want  bool
	}{
		{"1: dir arg, narrower glob", g("src/payments/api/**"), "src/payments", true, true},
		{"2: dir arg, broader glob", g("src/**"), "src/payments", true, true},
		{"3: first literal segment prunes", g("docs/**"), "src/payments", true, false},
		{"4: file arg, direct match", g("src/**/*.go"), "src/payments/refund.go", false, true},
		{"5: potential deeper file", g("src/*/middleware.go"), "src/local", true, true},
		{"6: no ** recursion", g("src/*.go"), "src/a/b", true, false},
		{"7: repository-wide matches all", g("**"), "any/thing/at/all", true, true},
		{"8: review-after-only never matches", "", "src/payments", true, false},
		{"9: mid-glob ** absorbs zero", g("src/**/test/*.go"), "src/a", true, true},
		{"10: later ** never rescues earlier", g("src/**/test/*.go"), "docs", true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.glob == "" {
				return // vector 8: no path scope exists to ask
			}
			if got := CouldClaim(tc.glob, tc.p, tc.isDir); got != tc.want {
				t.Fatalf("CouldClaim(%q, %q, dir=%v) = %v, want %v",
					tc.glob, tc.p, tc.isDir, got, tc.want)
			}
		})
	}

	t.Run("11: fused ** is a validation error, never a match question", func(t *testing.T) {
		if _, err := Canonical("src/a**b/x"); err == nil {
			t.Fatal("fused ** must be rejected")
		}
	})
}

func TestNormalization(t *testing.T) {
	tests := []struct{ in, want string }{
		{"src/a/**", "src/a/**"},
		{"./src/a/**", "src/a/**"},
		{"src/a/**/", "src/a/**"},
		{"src/a/", "src/a/**"},
		{"src/a", "src/a/**"},
		{".", "**"},
		{"./", "**"},
		{"**", "**"},
		{"./**", "**"},
		{"src/**/**/x", "src/**/x"},
	}
	for _, tc := range tests {
		got, err := Canonical(tc.in)
		if err != nil {
			t.Fatalf("Canonical(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("Canonical(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEquivalentSpellingsHashTheSameScope(t *testing.T) {
	// The subtree/filtered boundary must classify equivalent spellings
	// identically.
	if Classify(mustCanonical(t, "src/payments/**")) != Subtree {
		t.Fatal("src/payments/** must be subtree")
	}
	if Classify(mustCanonical(t, "src/payments/**/*.go")) != Enumerated {
		t.Fatal("filtered glob must be enumerated")
	}
	if Classify(mustCanonical(t, "src/*/middleware.go")) != Enumerated {
		t.Fatal("single-level * is filtered, not recursive")
	}
	if Classify(mustCanonical(t, "**")) != RepositoryWide {
		t.Fatal("** must be repository-wide")
	}
}

func TestBraceExpansion(t *testing.T) {
	got, err := Expand("src/{payments,api}/**")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"src/payments/**", "src/api/**"}
	if len(got) != len(want) {
		t.Fatalf("Expand = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Expand = %v, want %v", got, want)
		}
	}

	if got, err := Expand("{[ab],c}.md"); err != nil || len(got) != 2 {
		t.Fatalf("class inside alternative: %v %v", got, err)
	}
}

func TestRejections(t *testing.T) {
	bad := []string{
		"!src/**",       // negation
		"src/a**b/x",    // fused **
		"src/{a,{b,c}}", // nested braces
		"src/{a,}",      // empty alternative
		"src/{a",        // unterminated brace
		"src/[abc",      // unterminated class
		"src/[!abc]",    // ! class negation
		`src/trailing\`, // trailing lone backslash
		"",              // empty
	}
	for _, g := range bad {
		if _, err := Expand(g); err == nil {
			t.Fatalf("Expand(%q) must fail", g)
		}
	}
}

func TestEscapeSemantics(t *testing.T) {
	c := mustCanonical(t, `src/\[weird\]/*.md`)
	if !Match(c, `src/[weird]/a.md`) {
		t.Fatal("escaped brackets must match literally")
	}
	if Match(c, "src/x/a.md") {
		t.Fatal("escaped brackets must not act as a class")
	}
}

func TestMatching(t *testing.T) {
	tests := []struct {
		glob, path string
		want       bool
	}{
		{"src/payments/**", "src/payments", true},
		{"src/payments/**", "src/payments/api/x.go", true},
		{"src/payments/**", "src/other/x.go", false},
		{"**", "deeply/nested/file.txt", true},
		{"src/**/*.go", "src/x.go", true},
		{"src/**/*.go", "src/a/b/c.go", true},
		{"docs/*.md", "docs/a.md", true},
		{"docs/*.md", "docs/sub/a.md", false},
		{"file?.md", "file1.md", true},
		{"file?.md", "file12.md", false},
		{"[abc].md", "b.md", true},
		{"[abc].md", "d.md", false},
		{"[^abc].md", "d.md", true},
		{"[^abc].md", "a.md", false},
		{"[a-c].md", "b.md", true},
		{".vanguard/**", ".vanguard/vanguard.lock", true},
	}
	for _, tc := range tests {
		if got := Match(mustCanonical(t, tc.glob), tc.path); got != tc.want {
			t.Fatalf("Match(%q, %q) = %v, want %v", tc.glob, tc.path, got, tc.want)
		}
	}
	expanded, err := Expand("{a,b}/**")
	if err != nil || len(expanded) != 2 {
		t.Fatalf("Expand: %v %v", expanded, err)
	}
	if !Match(expanded[1], "b/x") {
		t.Fatal("expanded brace alternative must match")
	}
}

func TestDotfilesMatch(t *testing.T) {
	if !Match(mustCanonical(t, "**"), ".github/workflows/ci.yml") {
		t.Fatal("dotfiles must match like any file")
	}
}

func mustCanonical(t *testing.T, glob string) string {
	t.Helper()
	c, err := Canonical(glob)
	if err != nil {
		t.Fatalf("Canonical(%q): %v", glob, err)
	}
	return c
}
