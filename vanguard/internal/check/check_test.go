package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/vanguard-lint/vanguard/internal/lockfile"
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
		"---\nid: payments\nschema: none\nwatches:\n  paths: [\"src/payments/**\"]\n---\n# payments spec\n")
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
	// document axis â€” a cosmetic edit clears nothing.
	wf(t, root, ".vanguard/docs/payments.md",
		"---\nid: payments\nschema: none\nwatches:\n  paths: [\"src/payments/**\"]\n---\n# payments spec v2\n")
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
	// Uncommitted doc edit â€” even with a clean code scope.
	wf(t, root, ".vanguard/docs/payments.md",
		"---\nid: payments\nschema: none\nwatches:\n  paths: [\"src/payments/**\"]\n---\n# uncommitted\n")
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

// TestLockLostWithUnwatchedDoc pins the full-discovery-set reading of the
// lost-lock signature: an unwatched document has no attestation to lose, so
// its presence must not silence the detector when the lockfile is gone.
func TestLockLostWithUnwatchedDoc(t *testing.T) {
	root, store := scaffold(t)
	wf(t, root, ".vanguard/docs/loose.md",
		"---\nid: loose\nschema: none\n---\n# no watches declared\n")
	commitAll(t, root)
	if err := os.Remove(filepath.Join(store, "vanguard.lock")); err != nil {
		t.Fatal(err)
	}
	e := load(t, store)
	res, err := e.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	env := res.Envelope
	if !env.LockLost {
		t.Fatal("lock lost must be reported even with an unwatched document")
	}
	if env.ExitBits&BitStale == 0 {
		t.Fatalf("lock loss must set the stale bit by default: %d", env.ExitBits)
	}
}

// TestScopeSchemeMigrationFailsLoud pins the scheme-migration behavior: a
// lock written under the v1 combined digest ("sha256:") must go stale with
// the globs-or-scheme re-run-observe hint â€” never silently fresh, and the
// hint names the scheme, not phantom commits to inspect.
func TestScopeSchemeMigrationFailsLoud(t *testing.T) {
	root, store := scaffold(t)
	wf(t, root, ".vanguard/vanguard.lock",
		`{"version":1,"docs":{"payments":{"anchor":"0123456789abcdef0123456789abcdef01234567","observed_at":"2026-11-03T10:00:00Z","scope_tree_hash":"sha256:deadbeef","scope_file_count":1,"content_hash":"sha256:beef"}}}`)
	commitAll(t, root)
	e := load(t, store)
	res, err := e.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	env := res.Envelope
	var doc *Document
	for i := range env.Documents {
		if env.Documents[i].Doc == "payments" {
			doc = &env.Documents[i]
		}
	}
	if doc == nil {
		t.Fatal("payments missing from the report")
	}
	if doc.Status != StatusStale {
		t.Fatalf("a v1-scheme lock must go stale: %s", doc.Status)
	}
	found := false
	for _, h := range doc.Hints {
		if strings.Contains(h, "scope-hash scheme changed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("migration must hint the scheme, got hints %v", doc.Hints)
	}
	for _, h := range doc.Hints {
		if strings.Contains(h, "inspect the commits") {
			t.Fatalf("a scheme migration must not send the user commit-hunting: %v", doc.Hints)
		}
	}
}

func TestReviewAfterFires(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, ".vanguard")
	if err := os.MkdirAll(filepath.Join(store, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	wf(t, root, ".vanguard/vanguard.yaml", "version: 1\n")
	wf(t, root, ".vanguard/docs/glossary.md", "---\nid: glossary\nschema: none\nwatches:\n  review_after: 30d\n---\nbody")
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
	// Re-observe (the review act) clears the clock â€” the engine's clock is
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

// --- schema enforcement (structure layer M2) ---

const testProfile = `{
  "x-vanguard": {
    "format": "markdown",
    "required_sections": ["Interface"],
    "citation_sections": [],
    "citation_pattern": ""
  }
}`

// scaffoldBound scaffolds a store whose document declares a real schema.
func scaffoldBound(t *testing.T) (string, string) {
	t.Helper()
	root, store := scaffold(t)
	if err := os.MkdirAll(filepath.Join(store, "schemas"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "schemas", "pkg.schema.json"), []byte(testProfile), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, store
}

func TestMissingSchemaFieldIsFatal(t *testing.T) {
	root, store := scaffold(t)
	// A document without any schema field is unattestable silence.
	if err := os.WriteFile(filepath.Join(root, ".vanguard", "docs", "ghost.md"),
		[]byte("---\nid: ghost\nwatches:\n  paths: [\"src/**\"]\n---\n# ghost spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(store, Options{Now: time.Now()}); err == nil ||
		!strings.Contains(err.Error(), "declares no schema") {
		t.Fatalf("missing schema field must be fatal, got %v", err)
	}
}

func TestUnknownSchemaNameIsFatal(t *testing.T) {
	root, store := scaffold(t)
	if err := os.WriteFile(filepath.Join(root, ".vanguard", "docs", "payments.md"),
		[]byte("---\nid: payments\nschema: ghost\nwatches:\n  paths: [\"src/payments/**\"]\n---\n# payments spec\n"), 0o644); err != nil {
		t.Fatal("write failed")
	}
	if _, err := Load(store, Options{Now: time.Now()}); err == nil ||
		!strings.Contains(err.Error(), "declared but not found") {
		t.Fatalf("unknown schema must be fatal, got %v", err)
	}
}

func TestCorruptSchemaFileIsFatal(t *testing.T) {
	root, store := scaffold(t)
	if err := os.MkdirAll(filepath.Join(store, "schemas"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "schemas", "pkg.schema.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".vanguard", "docs", "payments.md"),
		[]byte("---\nid: payments\nschema: pkg\nwatches:\n  paths: [\"src/payments/**\"]\n---\n# payments spec\n"), 0o644); err != nil {
		t.Fatal("write failed")
	}
	if _, err := Load(store, Options{Now: time.Now()}); err == nil ||
		!strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("corrupt schema must be fatal, got %v", err)
	}
}
func TestStructureViolationIsFatalBitAndListed(t *testing.T) {
	root, store := scaffoldBound(t)
	// Missing the required Interface section.
	if err := os.WriteFile(filepath.Join(root, ".vanguard", "docs", "payments.md"),
		[]byte("---\nid: payments\nschema: pkg\nwatches:\n  paths: [\"src/payments/**\"]\n---\n# payments spec\n\nNo interface here.\n"), 0o644); err != nil {
		t.Fatal("write failed")
	}
	e := load(t, store)
	res, err := e.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	env := res.Envelope
	if env.ExitBits&BitFatal == 0 {
		t.Fatalf("structure violations must set the fatal bit: bits = %d", env.ExitBits)
	}
	if len(env.Documents) != 1 || len(env.Documents[0].Violations) == 0 {
		t.Fatalf("violations must be listed on the document: %+v", env.Documents)
	}
	if env.Documents[0].Violations[0].Rule != "structure_missing_section" {
		t.Fatalf("rule = %s", env.Documents[0].Violations[0].Rule)
	}
}

func TestCompliantDocumentIsFreshNotFatal(t *testing.T) {
	root, store := scaffoldBound(t)
	if err := os.WriteFile(filepath.Join(root, ".vanguard", "docs", "payments.md"),
		[]byte("---\nid: payments\nschema: pkg\nwatches:\n  paths: [\"src/payments/**\"]\n---\n# payments spec\n\n## Interface\n\n- api [payments.go:API]\n"), 0o644); err != nil {
		t.Fatal("write failed")
	}
	commitAll(t, root)
	e := load(t, store)
	if _, err := e.Observe(ObserveRequest{DocID: "payments"}); err != nil {
		t.Fatal(err)
	}
	e2 := load(t, store)
	res, err := e2.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	if res.Envelope.ExitBits != 0 {
		t.Fatalf("compliant document must be clean: bits = %d", res.Envelope.ExitBits)
	}
}

func TestObserveRefusesStructureViolation(t *testing.T) {
	root, store := scaffoldBound(t)
	if err := os.WriteFile(filepath.Join(root, ".vanguard", "docs", "payments.md"),
		[]byte("---\nid: payments\nschema: pkg\nwatches:\n  paths: [\"src/payments/**\"]\n---\n# payments spec\n\nNo interface here.\n"), 0o644); err != nil {
		t.Fatal("write failed")
	}
	commitAll(t, root)
	e := load(t, store)
	res, err := e.Observe(ObserveRequest{DocID: "payments"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Refusals) != 1 || res.Refusals[0].Reason != "structure violations" {
		t.Fatalf("observe must refuse a schema-violating document: %+v", res.Refusals)
	}
	if res.Envelope.ExitBits&BitFatal == 0 {
		t.Fatalf("refusal must be fatal: bits = %d", res.Envelope.ExitBits)
	}
}

// Advisory warnings ride on every check: an orphaned lock entry whose id
// no longer exists in discovery is flagged, never silently kept.
func TestOrphanLockEntryAdvisory(t *testing.T) {
	_, store := scaffold(t)
	e := load(t, store)
	e.Lock.Docs["ghost"] = lockfile.Entry{ObservedAt: "2026-11-03T12:00:00Z"}
	res, err := e.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range res.Envelope.Warnings {
		if strings.Contains(w, `orphaned lock entry "ghost"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("orphan advisory missing from warnings: %v", res.Envelope.Warnings)
	}
	if res.Envelope.ExitBits != 0 {
		t.Fatalf("advisory must never set an exit bit, got %d", res.Envelope.ExitBits)
	}
}

// A referenced binding on an unobserved document still reports pending â€”
// no baseline needed for the notation.
func TestBindingsPendingOnUnobserved(t *testing.T) {
	_, store := scaffoldBindings(t)
	e := bindingsEngine(t, store)
	res, err := e.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	docs := res.Envelope.Documents
	if len(docs) != 1 || docs[0].Status != StatusUnobserved {
		t.Fatalf("document must be unobserved: %+v", docs)
	}
	if len(docs[0].Bindings) != 1 || docs[0].Bindings[0].Name != "fail" || docs[0].Bindings[0].Status != "pending" {
		t.Fatalf("referenced binding must report pending on an unobserved doc: %+v", docs[0].Bindings)
	}
}

// --no-color / NO_COLOR strip ANSI from the pretty renderer: the CLICOLOR
// escape hatch colors it, the no-color escape hatch wins back.
func TestNoColorDegradesToText(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	_, store := scaffold(t)
	e := load(t, store)
	res, err := e.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	NoColor = false
	colored := renderPretty(res.Envelope, e)
	NoColor = true
	plain := renderPretty(res.Envelope, e)
	NoColor = false
	if !strings.Contains(colored, "\x1b[") {
		t.Fatal("forced-color run must carry ANSI styling")
	}
	if strings.Contains(plain, "\x1b[") {
		t.Fatal("NoColor run must carry no ANSI styling")
	}
	if plain != renderText(res.Envelope) {
		t.Fatal("NoColor pretty must degrade to the text rendering")
	}
}

// Text mode must surface refusals with their reason and hints — a bare
// "0 attested" is a silent failure (G8).
func TestTextRenderShowsRefusals(t *testing.T) {
	env := &Envelope{Mode: ModeObserve, Schema: SchemaVersion}
	env.Refused = append(env.Refused, Refused{Doc: "spec", Reason: "watch scope dirty",
		Offending: []string{"src/main.go"}, Hints: []string{"commit the churn, then re-run observe"}})
	out := renderText(env)
	if !strings.Contains(out, "REFUSED  spec  watch scope dirty") || !strings.Contains(out, "commit the churn") {
		t.Fatalf("text render must surface refusals:\n%s", out)
	}
}

// Amending the attested anchor rewrites history under an identical net
// state: the check stays fresh (net-state rule) but must NOTE the
// orphaned anchor — never silently green.
func TestFreshAmendedAnchorIsNoted(t *testing.T) {
	root, store := scaffold(t)
	e := load(t, store)
	if _, err := e.Observe(ObserveRequest{All: true}); err != nil {
		t.Fatal(err)
	}
	repo, err := git.PlainOpen(root)
	if err != nil {
		t.Fatal(err)
	}
	w, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit("amended", &git.CommitOptions{Amend: true, Author: sig(), AllowEmptyCommits: true}); err != nil {
		t.Fatal(err)
	}
	e2 := load(t, store)
	res, err := e2.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	d := res.Envelope.Documents[0]
	if d.Status != StatusFresh || !d.AnchorRecovered {
		t.Fatalf("amended anchor must stay fresh with the recovered note, got status=%s recovered=%v orphaned=%v",
			d.Status, d.AnchorRecovered, d.AnchorOrphaned)
	}
}

func TestDirtyEnvelopeCarriesWhatAndWhere(t *testing.T) {
	root, store := scaffold(t)
	e := load(t, store)
	if _, err := e.Observe(ObserveRequest{DocID: "payments"}); err != nil {
		t.Fatal(err)
	}
	wf(t, root, "src/payments/api.go", "package payments // churned\n")

	res, err := load(t, store).Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	d := res.Envelope.Documents[0]
	if d.Status != StatusDirty {
		t.Fatalf("status = %s, want dirty", d.Status)
	}
	seen := false
	for _, p := range d.ChangedPaths {
		if strings.Contains(p, "api.go") {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("changed_paths = %v, want the churned file", d.ChangedPaths)
	}
	if len(d.Hints) == 0 {
		t.Fatal("dirty document must carry a next-command hint")
	}
}

func TestMissingDocsRootIsActionable(t *testing.T) {
	_, store := scaffold(t)
	if err := os.RemoveAll(filepath.Join(store, "docs")); err != nil {
		t.Fatal(err)
	}
	_, err := Load(store, Options{Now: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)})
	if err == nil {
		t.Fatal("missing document root must be fatal")
	}
	if !strings.Contains(err.Error(), "does not exist") || !strings.Contains(err.Error(), "vanguard init") {
		t.Fatalf("fatal must name the path and the recovery, got: %v", err)
	}
}
