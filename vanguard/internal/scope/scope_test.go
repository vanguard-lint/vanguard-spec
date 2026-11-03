package scope

import "testing"

func entries() []Entry {
	return []Entry{
		{"src/payments/api.go", "100644", "aaa1"},
		{"src/payments/gateway.go", "100644", "bbb2"},
		{"src/payments/README.md", "100644", "ccc3"},
		{"src/api/router.go", "100644", "ddd4"},
		{".vanguard/vanguard.lock", "100644", "eee5"},
	}
}

func TestHashEntriesDeterministic(t *testing.T) {
	a := HashEntries(entries())
	// Reverse the input order: the formula sorts by path bytes first.
	rev := entries()
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	if b := HashEntries(rev); a != b {
		t.Fatalf("input order must not matter: %s vs %s", a, b)
	}
	if want := "sha256:"; len(a) < len(want) || a[:len(want)] != want {
		t.Fatalf("hash must be kind-prefixed: %s", a)
	}
}

func TestHashEntriesPathIsPartOfTheHash(t *testing.T) {
	// Swapping contents between two paths must change the hash.
	x := []Entry{{"a.txt", "100644", "h1"}, {"b.txt", "100644", "h2"}}
	y := []Entry{{"a.txt", "100644", "h2"}, {"b.txt", "100644", "h1"}}
	if HashEntries(x) == HashEntries(y) {
		t.Fatal("paths participate: swapped contents must re-key")
	}
	// Renaming a file must change the hash.
	z := []Entry{{"c.txt", "100644", "h1"}, {"b.txt", "100644", "h2"}}
	if HashEntries(x) == HashEntries(z) {
		t.Fatal("rename must re-key the hash")
	}
	// Mode is part of the hash.
	m := []Entry{{"a.txt", "100755", "h1"}, {"b.txt", "100644", "h2"}}
	if HashEntries(x) == HashEntries(m) {
		t.Fatal("mode must participate")
	}
	// Empty list hashes distinctly from any non-empty one.
	if HashEntries(nil) == HashEntries(x) {
		t.Fatal("empty scope hash must differ from non-empty")
	}
}

func TestHashEnumerated(t *testing.T) {
	got := HashEnumerated("src/payments/**/*.go", entries(), ".vanguard")
	// Only the two .go files under src/payments match; the store entry is
	// filtered by prefix even when it would match.
	want := HashEntries([]Entry{
		{"src/payments/api.go", "100644", "aaa1"},
		{"src/payments/gateway.go", "100644", "bbb2"},
	})
	if got != want {
		t.Fatalf("enumerated hash: got %s want %s", got, want)
	}
}

func TestFilterStore(t *testing.T) {
	got := FilterStore(entries(), ".vanguard")
	for _, e := range got {
		if e.Path == ".vanguard/vanguard.lock" {
			t.Fatal("store entry must be filtered")
		}
	}
	if len(got) != 4 {
		t.Fatalf("kept %d entries, want 4", len(got))
	}
	if len(FilterStore(entries(), "")) != 5 {
		t.Fatal("empty store dir filters nothing")
	}
}

func TestHashRepositoryWide(t *testing.T) {
	root := []Entry{
		{"src", "040000", "treeA"},
		{"docs", "040000", "treeB"},
		{".vanguard", "040000", "treeC"},
		{"README.md", "100644", "fff6"},
	}
	withStore := HashRepositoryWide(root, ".vanguard")
	without := HashEntries([]Entry{
		{"docs", "040000", "treeB"},
		{"README.md", "100644", "fff6"},
		{"src", "040000", "treeA"},
	})
	if withStore != without {
		t.Fatalf("store must be excluded by name: got %s want %s", withStore, without)
	}
}

func TestCombine(t *testing.T) {
	// Globs are sorted as written (canonical bytes); the caller's order
	// must not matter.
	a := Combine([]Pair{
		{"src/a/**", "sha256:x"},
		{"src/b/**", "sha256:y"},
	})
	b := Combine([]Pair{
		{"src/b/**", "sha256:y"},
		{"src/a/**", "sha256:x"},
	})
	if a != b {
		t.Fatal("glob order must not matter")
	}
	// Rename respelling a glob re-keys its hash.
	c := Combine([]Pair{
		{"src/aa/**", "sha256:x"},
		{"src/b/**", "sha256:y"},
	})
	if a == c {
		t.Fatal("glob text is part of the hash: respelling must re-key")
	}
	// Single glob is the degenerate one-element list.
	single := Combine([]Pair{{"src/a/**", "sha256:x"}})
	if single == a {
		t.Fatal("degenerate list must differ from the pair")
	}
}

func TestSameKind(t *testing.T) {
	if !SameKind("tree:abc", "tree:def") {
		t.Fatal("same kind must be comparable")
	}
	if SameKind("tree:abc", "sha256:abc") {
		t.Fatal("kind mismatch must not be comparable")
	}
	if !SameKind("sha256:abc", "sha256:def") {
		t.Fatal("same kind with different values stays comparable")
	}
}
