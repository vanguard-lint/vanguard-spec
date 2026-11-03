package lockfile

// Q-2.2.a-2: the losing side of a CAS race exits with an error and leaves
// the file byte-identical to the winner's write — no partial bytes, no
// interleaving.

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestCASLoserLeavesWinnerBytes: two racers save different docs from the
// same captured baseline. Exactly one wins; the loser errors; the file on
// disk is byte-identical to the winner's serialization.
func TestCASLoserLeavesWinnerBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vanguard.lock")
	lock := sampleLock()
	captured := Serialize(lock)
	if err := os.WriteFile(path, captured, 0o644); err != nil {
		t.Fatal(err)
	}

	type outcome struct {
		name string
		err  error
	}
	results := make(chan outcome, 2)
	var wg sync.WaitGroup
	for i, name := range []string{"racer-a", "racer-b"} {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			mine := &Lock{Version: 1, Docs: map[string]Entry{
				name: {ObservedAt: "2026-11-03T12:00:00Z", ContentHash: "sha256:" + name},
			}}
			results <- outcome{name, Save(path, mine, captured)}
		}(i, name)
	}
	wg.Wait()
	close(results)

	winners := 0
	losers := 0
	for r := range results {
		if r.err == nil {
			winners++
		} else {
			losers++
			if !contains(errText(r.err), "lock changed during observe") {
				t.Fatalf("loser must report the CAS conflict, got: %v", r.err)
			}
		}
	}
	if winners != 1 || losers != 1 {
		t.Fatalf("exactly one winner expected, got %d winners / %d losers", winners, losers)
	}

	// The file must be byte-identical to a fresh serialization of the
	// winner's lock — the loser never interleaved.
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(onDisk)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Docs) != 1 {
		t.Fatalf("winner's lock must hold exactly its own doc, got %v", parsed.Docs)
	}
	for name := range parsed.Docs {
		if name != "racer-a" && name != "racer-b" {
			t.Fatalf("unexpected doc in winning lock: %s", name)
		}
	}
	// And re-serializing must reproduce the exact bytes (canonical form).
	if string(Serialize(parsed)) != string(onDisk) {
		t.Fatal("the file is not the winner's canonical serialization")
	}
	// No temp files left behind by the race.
	matches, _ := filepath.Glob(path + ".tmp-*")
	if len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
}

func errText(err error) string { return err.Error() }

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
