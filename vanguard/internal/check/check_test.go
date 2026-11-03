package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func sig() *object.Signature {
	return &object.Signature{Name: "t", Email: "t@t", When: time.Now()}
}

func wf(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// scaffold builds a git repository with a vanguard store and one committed
// code file. Returns (root, storeDir).
func scaffold(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(root, ".vanguard")
	if err := os.MkdirAll(filepath.Join(store, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	wf(t, root, ".vanguard/vanguard.yaml", "version: 1\n")
	wf(t, root, ".vanguard/vanguard.lock", "{\"version\": 1, \"docs\": {}}\n")
	wf(t, root, ".vanguard/docs/payments.md",
		"---\nid: payments\nwatches:\n  paths: [\"src/payments/**\"]\n---\n# payments spec\n")
	wf(t, root, "src/payments/api.go", "package payments\n")
	w, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Add("."); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit("initial", &git.CommitOptions{Author: sig()}); err != nil {
		t.Fatal(err)
	}
	return root, store
}

func commitAll(t *testing.T, root string) string {
	t.Helper()
	repo, err := git.PlainOpen(root)
	if err != nil {
		t.Fatal(err)
	}
	w, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Add("."); err != nil {
		t.Fatal(err)
	}
	c, err := w.Commit("churn", &git.CommitOptions{Author: sig(), AllowEmptyCommits: true})
	if err != nil {
		t.Fatal(err)
	}
	return c.String()
}

func load(t *testing.T, store string) *Engine {
	t.Helper()
	e, err := Load(store, Options{Now: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestInitlessCheckUnobserved(t *testing.T) {
	root, store := scaffold(t)
	_ = root
	e := load(t, store)
	res, err := e.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	env := res.Envelope
	if len(env.Unobserved) != 1 || env.Unobserved[0] != "payments" {
		t.Fatalf("unobserved = %v", env.Unobserved)
	}
	if env.ExitBits != 0 {
		t.Fatalf("bootstrap gaps must not break CI: bits = %d", env.ExitBits)
	}
}

func TestObserveThenCheckFresh(t *testing.T) {
	root, store := scaffold(t)
	e := load(t, store)
	res, err := e.Observe(ObserveRequest{DocID: "payments"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Envelope.Lock.Changed {
		t.Fatal("lock must change on first observe")
	}
	// Fresh clone semantics: reload the engine from the written lock.
	e2 := load(t, store)
	res2, err := e2.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Envelope.Documents) != 1 || res2.Envelope.Documents[0].Status != StatusFresh {
		t.Fatalf("observed document must be fresh: %+v", res2.Envelope.Documents)
	}
	_ = root
}

func TestCodeChurnMakesStale(t *testing.T) {
	root, store := scaffold(t)
	e := load(t, store)
	if _, err := e.Observe(ObserveRequest{DocID: "payments"}); err != nil {
		t.Fatal(err)
	}
	wf(t, root, "src/payments/gateway.go", "package payments\n")
	commitAll(t, root)

	e2 := load(t, store)
	res, err := e2.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	d := res.Envelope.Documents[0]
	if d.Status != StatusStale {
		t.Fatalf("status = %s, want stale", d.Status)
	}
	if !containsString(d.TriggeredWatches, "paths") {
		t.Fatalf("triggered = %v", d.TriggeredWatches)
	}
	if len(d.ChangedPaths) != 1 || d.ChangedPaths[0] != "src/payments/gateway.go" {
		t.Fatalf("changed paths = %v", d.ChangedPaths)
	}
	if len(d.IntroducingCommits) != 1 {
		t.Fatalf("introducing commits = %v", d.IntroducingCommits)
	}
	if res.Envelope.ExitBits&BitStale == 0 {
		t.Fatalf("stale bit must be set: %d", res.Envelope.ExitBits)
	}
}

func TestDocumentEditMakesStale(t *testing.T) {
	root, store := scaffold(t)
	e := load(t, store)
	if _, err := e.Observe(ObserveRequest{DocID: "payments"}); err != nil {
		t.Fatal(err)
	}
	// Editing the document without observing must go stale via the
	// document axis — a cosmetic edit clears nothing.
	wf(t, root, ".vanguard/docs/payments.md",
		"---\nid: payments\nwatches:\n  paths: [\"src/payments/**\"]\n---\n# payments spec v2\n")
	commitAll(t, root)

	e2 := load(t, store)
	res, err := e2.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	d := res.Envelope.Documents[0]
	if d.Status != StatusStale || !d.DocumentChanged {
		t.Fatalf("status = %s document_changed = %v", d.Status, d.DocumentChanged)
	}
}

func TestUncommittedChurnIsDirty(t *testing.T) {
	root, store := scaffold(t)
	e := load(t, store)
	if _, err := e.Observe(ObserveRequest{DocID: "payments"}); err != nil {
		t.Fatal(err)
	}
	wf(t, root, "src/payments/api.go", "package payments // edited\n")

	e2 := load(t, store)
	res, err := e2.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	d := res.Envelope.Documents[0]
	if d.Status != StatusDirty {
		t.Fatalf("status = %s, want dirty", d.Status)
	}
	if res.Envelope.ExitBits&BitDirty == 0 {
		t.Fatalf("dirty bit missing: %d", res.Envelope.ExitBits)
	}
}

func TestObserveRefusesDirtyScope(t *testing.T) {
	root, store := scaffold(t)
	_ = root
	wf(t, filepath.Join(root, "src", "payments"), "x.go", "package x\n") // untracked churn
	// rewrite cleanly
	os.Remove(filepath.Join(root, "src", "payments", "x.go")) //nolint:errcheck // best-effort cleanup in a test
	wf(t, root, "src/payments/api.go", "package payments // edited\n")

	e := load(t, store)
	res, err := e.Observe(ObserveRequest{DocID: "payments"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Refusals) != 1 || res.Refusals[0].Reason != "watch scope dirty" {
		t.Fatalf("refusals = %+v", res.Refusals)
	}
	if res.Envelope.ExitBits&BitDirty == 0 {
		t.Fatalf("dirty bit missing: %d", res.Envelope.ExitBits)
	}
	if res.Envelope.Lock.Changed {
		t.Fatal("a refused observe must not write")
	}
}

func TestObserveRefusesUncommittedDocument(t *testing.T) {
	root, store := scaffold(t)
	// Uncommitted doc edit — even with a clean code scope.
	wf(t, root, ".vanguard/docs/payments.md",
		"---\nid: payments\nwatches:\n  paths: [\"src/payments/**\"]\n---\n# uncommitted\n")
	e := load(t, store)
	res, err := e.Observe(ObserveRequest{DocID: "payments"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Refusals) != 1 || res.Refusals[0].Reason != "document not committed" {
		t.Fatalf("refusals = %+v", res.Refusals)
	}
}

func TestLockLost(t *testing.T) {
	root, store := scaffold(t)
	if err := os.Remove(filepath.Join(store, "vanguard.lock")); err != nil {
		t.Fatal(err)
	}
	_ = root
	e := load(t, store)
	res, err := e.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	env := res.Envelope
	if !env.LockLost {
		t.Fatal("lock lost must be reported")
	}
	if env.ExitBits&BitStale == 0 {
		t.Fatalf("lock loss must set the stale bit by default: %d", env.ExitBits)
	}
}

func TestReviewAfterFires(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, ".vanguard")
	if err := os.MkdirAll(filepath.Join(store, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	wf(t, root, ".vanguard/vanguard.yaml", "version: 1\n")
	wf(t, root, ".vanguard/docs/glossary.md", "---\nid: glossary\nwatches:\n  review_after: 30d\n---\nbody")
	wf(t, root, "code.txt", "x") // hash mode: no .git

	e := load(t, store)
	if _, err := e.Observe(ObserveRequest{DocID: "glossary"}); err != nil {
		t.Fatal(err)
	}
	// Advance the clock past the review window.
	later := time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC)
	e2, err := Load(store, Options{Now: later})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e2.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	d := res.Envelope.Documents[0]
	if d.Status != StatusStale || !containsString(d.TriggeredWatches, "review_after") {
		t.Fatalf("status = %s triggered = %v", d.Status, d.TriggeredWatches)
	}
	// Re-observe (the review act) clears the clock — the engine's clock is
	// the attestation source, so observe at the advanced instant.
	e3, err := Load(store, Options{Now: later})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e3.Observe(ObserveRequest{DocID: "glossary"}); err != nil {
		t.Fatal(err)
	}
	e4, err := Load(store, Options{Now: later})
	if err != nil {
		t.Fatal(err)
	}
	res2, err := e4.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Envelope.Documents[0].Status != StatusFresh {
		t.Fatalf("review act must clear the clock: %s", res2.Envelope.Documents[0].Status)
	}
}

func TestStrictElevatesGaps(t *testing.T) {
	_, store := scaffold(t)
	e := load(t, store)
	res, err := e.Check(Selection{Mode: ModeAll, Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Envelope.ExitBits&BitGaps == 0 {
		t.Fatalf("strict must elevate gaps: %d", res.Envelope.ExitBits)
	}
}

func TestPathReverseLookup(t *testing.T) {
	_, store := scaffold(t)
	e := load(t, store)
	res, err := e.Check(Selection{Mode: ModePath, Path: "src/payments", PathIsDir: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Envelope.Documents) != 1 || res.Envelope.Documents[0].Doc != "payments" {
		t.Fatalf("documents = %+v", res.Envelope.Documents)
	}
	// Unrelated territory claims nothing.
	res2, err := e.Check(Selection{Mode: ModePath, Path: "docs", PathIsDir: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Envelope.Documents) != 0 {
		t.Fatalf("unrelated path must not match: %+v", res2.Envelope.Documents)
	}
}

func TestRangeModeLockfree(t *testing.T) {
	root, store := scaffold(t)
	c1 := commitAll(t, root)
	wf(t, root, "src/payments/gateway.go", "package payments\n")
	c2 := commitAll(t, root)

	e2 := load(t, store)
	res, err := e2.Check(Selection{Mode: ModeRange, From: c1, To: c2})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Envelope.AffectedInRange) != 1 {
		t.Fatalf("affected = %v", res.Envelope.AffectedInRange)
	}
	// Informational by default: exit 0 without --fail-on-affected.
	if res.Envelope.ExitBits != 0 {
		t.Fatalf("bits = %d", res.Envelope.ExitBits)
	}
	res2, err := e2.Check(Selection{Mode: ModeRange, From: c1, To: c2, FailOnAffected: true})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Envelope.ExitBits&BitAffectedInRange == 0 {
		t.Fatalf("--fail-on-affected must set bit 32: %d", res2.Envelope.ExitBits)
	}
}

func TestJSONEnvelopeShape(t *testing.T) {
	_, store := scaffold(t)
	e := load(t, store)
	res, err := e.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Render(res.Envelope, "json")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"\"mode\": \"all\"", "\"schema\": 1", "\"exit_bits\"", "\"unobserved\""} {
		if !strings.Contains(out, key) {
			t.Fatalf("envelope missing %s:\n%s", key, out)
		}
	}
	yamlOut, err := Render(res.Envelope, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(yamlOut, "mode: all") {
		t.Fatalf("yaml envelope:\n%s", yamlOut)
	}
}
