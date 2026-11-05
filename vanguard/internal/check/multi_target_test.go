package check

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/vanguard-lint/vanguard/internal/lockfile"
	"github.com/vanguard-lint/vanguard/internal/provider"
)

// scaffoldPair builds the decoupled fixture: a store repository (specs) and
// a separate target repository (code). The v1 engine refused observe here
// forever — the committed-document precondition evaluated document paths
// against the target, where they do not exist.
func scaffoldPair(t *testing.T) (storeRoot, store, targetRoot string) {
	t.Helper()
	storeRoot = t.TempDir()
	targetRoot = t.TempDir()
	if _, err := git.PlainInit(storeRoot, false); err != nil {
		t.Fatal(err)
	}
	if _, err := git.PlainInit(targetRoot, false); err != nil {
		t.Fatal(err)
	}
	store = filepath.Join(storeRoot, ".vanguard")
	if err := os.MkdirAll(filepath.Join(store, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	wf(t, storeRoot, ".vanguard/vanguard.yaml",
		fmt.Sprintf("version: 1\ntargets:\n  payments: %s\n", targetRoot))
	wf(t, storeRoot, ".vanguard/docs/payments.md",
		"---\nid: payments\nschema: none\ntarget: payments\nwatches:\n  paths: [\"src/payments/**\"]\n---\n# payments spec\n")
	wf(t, targetRoot, "src/payments/api.go", "package payments\n")
	commitAll(t, storeRoot)
	commitAll(t, targetRoot)
	return storeRoot, store, targetRoot
}

// TestDecoupledStoreFullLifecycle reproduces the v1 refusal case and walks
// the full cycle: observe against a foreign target, then a target commit
// turns the document stale with attribution.
func TestDecoupledStoreFullLifecycle(t *testing.T) {
	storeRoot, store, targetRoot := scaffoldPair(t)
	_ = storeRoot
	e := load(t, store)

	res, err := e.Observe(ObserveRequest{DocID: "payments"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Refusals) > 0 {
		t.Fatalf("decoupled observe refused: %+v", res.Refusals)
	}
	entry, ok := e.Lock.Docs["payments"]
	if !ok {
		t.Fatal("no lock entry written")
	}
	if entry.Target != "payments" {
		t.Fatalf("entry target = %q, want payments", entry.Target)
	}
	tp, err := provider.OpenGit(targetRoot, ".vanguard")
	if err != nil {
		t.Fatal(err)
	}
	head, _ := tp.HEAD()
	if entry.Anchor != head {
		t.Fatalf("anchor %q, want target HEAD %q", entry.Anchor, head)
	}

	// The target churns and commits — the decoupled store must see it.
	wf(t, targetRoot, "src/payments/api.go", "package payments // v2\n")
	commitAll(t, targetRoot)

	e2 := load(t, store)
	res2, err := e2.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	var d *Document
	for i := range res2.Envelope.Documents {
		if res2.Envelope.Documents[i].Doc == "payments" {
			d = &res2.Envelope.Documents[i]
		}
	}
	if d == nil {
		t.Fatal("payments missing from envelope")
	}
	if d.Status != StatusStale {
		t.Fatalf("status = %q, want stale", d.Status)
	}
	if len(d.IntroducingCommits) == 0 {
		t.Fatal("no attribution across the decoupled boundary")
	}
	if d.Target != "payments" {
		t.Fatalf("envelope target = %q, want payments", d.Target)
	}
}

// TestDecoupledStoreRefusesUncommittedDoc proves the committed-document
// precondition now asks the store provider: an uncommitted document in the
// store repo is refused even though the target is clean.
func TestDecoupledStoreRefusesUncommittedDoc(t *testing.T) {
	_, store, targetRoot := scaffoldPair(t)
	wf(t, filepath.Dir(store), ".vanguard/docs/late.md",
		"---\nid: late\nschema: none\ntarget: payments\nwatches:\n  paths: [\"src/payments/**\"]\n---\n# not yet committed\n")
	e := load(t, store)
	res, err := e.Observe(ObserveRequest{DocID: "late"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Refusals) != 1 || res.Refusals[0].Reason != "document not committed" {
		t.Fatalf("want a document-not-committed refusal, got %+v", res.Refusals)
	}
	_ = targetRoot
}

// TestOneStoreThreeTargets: one store, one git target, one hash target.
// Each document follows its own provider; churn in one target leaves the
// other fresh.
func TestOneStoreThreeTargets(t *testing.T) {
	storeRoot := t.TempDir()
	gitTarget := t.TempDir()
	hashTarget := t.TempDir()
	if _, err := git.PlainInit(storeRoot, false); err != nil {
		t.Fatal(err)
	}
	if _, err := git.PlainInit(gitTarget, false); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(storeRoot, ".vanguard")
	if err := os.MkdirAll(filepath.Join(store, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	wf(t, storeRoot, ".vanguard/vanguard.yaml",
		fmt.Sprintf("version: 1\ntargets:\n  alpha: %s\n  gamma: %s\n", gitTarget, hashTarget))
	wf(t, storeRoot, ".vanguard/docs/alpha.md",
		"---\nid: alpha\nschema: none\ntarget: alpha\nwatches:\n  paths: [\"src/**\"]\n---\n# alpha spec\n")
	wf(t, storeRoot, ".vanguard/docs/gamma.md",
		"---\nid: gamma\nschema: none\ntarget: gamma\nwatches:\n  paths: [\"data/**\"]\n---\n# gamma spec\n")
	wf(t, gitTarget, "src/a.go", "package a\n")
	wf(t, hashTarget, "data/list.csv", "id\n1\n")
	commitAll(t, storeRoot)
	commitAll(t, gitTarget)

	e := load(t, store)
	res, err := e.Observe(ObserveRequest{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Refusals) > 0 {
		t.Fatalf("refused: %+v", res.Refusals)
	}
	if e.Lock.Docs["alpha"].Target != "alpha" || e.Lock.Docs["gamma"].Target != "gamma" {
		t.Fatalf("entry targets not recorded: %+v %+v", e.Lock.Docs["alpha"], e.Lock.Docs["gamma"])
	}

	// Churn both targets: git-target commit + hash-target net change.
	wf(t, gitTarget, "src/a.go", "package a // v2\n")
	commitAll(t, gitTarget)
	wf(t, hashTarget, "data/list.csv", "id\n1\n2\n")

	e2 := load(t, store)
	res2, err := e2.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]Document{}
	for i := range res2.Envelope.Documents {
		statuses[res2.Envelope.Documents[i].Doc] = res2.Envelope.Documents[i]
	}
	if got := statuses["alpha"].Status; got != StatusStale {
		t.Fatalf("alpha = %q, want stale (git target)", got)
	}
	if len(statuses["alpha"].IntroducingCommits) == 0 {
		t.Fatal("alpha: expected attribution from its git target")
	}
	if got := statuses["gamma"].Status; got != StatusStale {
		t.Fatalf("gamma = %q, want stale (hash target)", got)
	}
	if len(statuses["gamma"].IntroducingCommits) != 0 {
		t.Fatal("gamma: a git-less target must not invent attribution")
	}

	// Text render annotates non-primary bindings.
	out, err := Render(res2.Envelope, "text")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "alpha@alpha") || !strings.Contains(out, "gamma@gamma") {
		t.Fatalf("text render missing @target annotation:\n%s", out)
	}
}

// TestInRepoStoreLockHasNoTargetField: 0.3.0 stores serialize byte-identical
// locks — no target field on primary-target entries.
func TestInRepoStoreLockHasNoTargetField(t *testing.T) {
	_, store := scaffold(t)
	e := load(t, store)
	if _, err := e.Observe(ObserveRequest{DocID: "payments"}); err != nil {
		t.Fatal(err)
	}
	raw := lockfile.Serialize(e.Lock)
	if strings.Contains(string(raw), "\"target\"") {
		t.Fatalf("primary-target entry must not serialize a target field:\n%s", raw)
	}
}

// TestUnknownTargetNameIsFatal: a document bound to a target that does not
// exist would attest against nothing — fatal at load.
func TestUnknownTargetNameIsFatal(t *testing.T) {
	root, store := scaffold(t)
	wf(t, root, ".vanguard/vanguard.yaml", "version: 1\ntargets:\n  other: ../somewhere\n")
	wf(t, root, ".vanguard/docs/payments.md",
		"---\nid: payments\nschema: none\ntarget: payments\nwatches:\n  paths: [\"src/payments/**\"]\n---\n# payments spec\n")
	if _, err := Load(store, Options{Now: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}); err == nil {
		t.Fatal("want a fatal for an unknown target name")
	}
}

// TestMissingTargetPathIsFatal: a declared target pointing at nothing is a
// config error, never a silent fall into hash mode.
func TestMissingTargetPathIsFatal(t *testing.T) {
	root, store := scaffold(t)
	missing := filepath.Join(t.TempDir(), "nowhere")
	wf(t, root, ".vanguard/vanguard.yaml",
		fmt.Sprintf("version: 1\ntargets:\n  ghost: %s\n", missing))
	if _, err := Load(store, Options{Now: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}); err == nil {
		t.Fatal("want a fatal for a missing target path")
	}
}

// TestLockEntryUnknownTargetIsFatal: the lock cannot outlive a target
// rename silently.
func TestLockEntryUnknownTargetIsFatal(t *testing.T) {
	root, store := scaffold(t)
	wf(t, root, ".vanguard/vanguard.lock",
		"{\"version\": 1, \"docs\": {\"payments\": {\"observed_at\": \"2026-11-03T12:00:00Z\", \"content_hash\": \"sha256v2:abc\", \"target\": \"renamed-away\"}}}\n")
	if _, err := Load(store, Options{Now: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}); err == nil {
		t.Fatal("want a fatal for a lock entry naming an unconfigured target")
	}
}

// TestOldLockWithoutTargetReadsClean: 0.3.0 locks (no target fields) load
// unchanged beside a targets: map — zero migration.
func TestOldLockWithoutTargetReadsClean(t *testing.T) {
	root, store := scaffold(t)
	sibling := t.TempDir()
	wf(t, root, ".vanguard/vanguard.yaml",
		fmt.Sprintf("version: 1\ntargets:\n  other: %s\n", sibling))
	e := load(t, store)
	d, _ := docByID2(e, "payments")
	if d == nil {
		t.Fatal("payments not discovered")
	}
	if _, ok := e.Lock.Docs["payments"]; ok {
		t.Fatal("expected unobserved (lock reset)")
	}
}

func docByID2(e *Engine, id string) (*Document, bool) {
	res, err := e.Check(Selection{Mode: ModeDoc, DocID: id})
	if err != nil {
		return nil, false
	}
	for i := range res.Envelope.Documents {
		if res.Envelope.Documents[i].Doc == id {
			return &res.Envelope.Documents[i], true
		}
	}
	return nil, false
}
