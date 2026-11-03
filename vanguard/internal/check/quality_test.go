package check

// Quality criteria benchmarks (Q-3.c, Q-3.d, Q-4.a-d, Q-5.d): every test here
// maps to a criteria-tree leaf. A failing test means the tool is wrong — fix
// the tool, never the expectation.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/vanguard-lint/vanguard/internal/lockfile"
	"gopkg.in/yaml.v3"
)

// scaffoldMulti builds a git repository with a store holding the given
// documents (id, watch glob) plus one matching code file per document.
func scaffoldMulti(t *testing.T, docs map[string]string) (string, string) {
	t.Helper()
	root, store := scaffold(t)
	repo, err := git.PlainOpen(root)
	if err != nil {
		t.Fatal(err)
	}
	w, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	for id, glob := range docs {
		if id == "payments" {
			continue
		}
		wf(t, root, ".vanguard/docs/"+id+".md",
			"---\nid: "+id+"\nschema: none\nwatches:\n  paths: [\""+glob+"\"]\n---\n# "+id+" spec\n")
		wf(t, root, strings.TrimSuffix(glob, "**"), "package "+id+"\n")
	}
	if _, err := w.Add("."); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit("docs", &git.CommitOptions{Author: sig(), AllowEmptyCommits: true}); err != nil {
		t.Fatal(err)
	}
	return root, store
}

func treeHashes(root string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(raw)
		return nil
	})
	return out, err
}

func diffKeys(a, b map[string]string) []string {
	var keys []string
	for k, v := range a {
		if b[k] != v {
			keys = append(keys, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// Q-3.c-1: identical runs must render byte-identical JSON.
func TestJSONDeterministicAcrossRuns(t *testing.T) {
	_, store := scaffoldMulti(t, map[string]string{"billing": "src/billing/**"})
	e := load(t, store)
	if _, err := e.Observe(ObserveRequest{DocID: "payments"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Observe(ObserveRequest{DocID: "billing"}); err != nil {
		t.Fatal(err)
	}
	e2 := load(t, store)
	r1, err := e2.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := e2.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	j1, err := Render(r1.Envelope, "json")
	if err != nil {
		t.Fatal(err)
	}
	j2, err := Render(r2.Envelope, "json")
	if err != nil {
		t.Fatal(err)
	}
	if j1 != j2 {
		t.Fatalf("two identical runs produced different JSON:\n%s\n---\n%s", j1, j2)
	}
}

// Q-3.c-2: envelope arrays are sorted by id bytes.
func TestEnvelopeArraysSorted(t *testing.T) {
	_, store := scaffoldMulti(t, map[string]string{"zeta": "src/zeta/**", "alpha": "src/alpha/**", "mid": "src/mid/**"})
	e := load(t, store)
	res, err := e.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	env := res.Envelope
	sorted := func(s []string) bool {
		return sort.StringsAreSorted(s)
	}
	if !sorted(env.Unobserved) && len(env.Unobserved) > 1 {
		t.Fatalf("unobserved not sorted: %v", env.Unobserved)
	}
	if !sorted(env.Unwatched) && len(env.Unwatched) > 1 {
		t.Fatalf("unwatched not sorted: %v", env.Unwatched)
	}
	for i := 1; i < len(env.Documents); i++ {
		if env.Documents[i-1].Doc > env.Documents[i].Doc {
			t.Fatalf("documents not sorted: %s > %s", env.Documents[i-1].Doc, env.Documents[i].Doc)
		}
	}
}

// Q-4.c-1: JSON and YAML envelopes carry identical values.
func TestJSONYAMLRenderEquivalence(t *testing.T) {
	_, store := scaffoldMulti(t, map[string]string{"billing": "src/billing/**"})
	e := load(t, store)
	res, err := e.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	j, err := Render(res.Envelope, "json")
	if err != nil {
		t.Fatal(err)
	}
	y, err := Render(res.Envelope, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	var jv, yv any
	if err := json.Unmarshal([]byte(j), &jv); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(y), &yv); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalizeNumbers(jv), normalizeNumbers(yv)) {
		t.Fatalf("json and yaml envelopes diverge:\njson: %#v\nyaml: %#v", jv, yv)
	}
}

// normalizeNumbers folds every numeric leaf to float64 so json (float64) and
// yaml (int) representations compare equal.
func normalizeNumbers(v any) any {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case float64:
		return x
	case map[string]any:
		for k, e := range x {
			x[k] = normalizeNumbers(e)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = normalizeNumbers(e)
		}
		return x
	default:
		return v
	}
}

// Q-4.a-2: observe mutates exactly one file — the lockfile.
func TestObserveMutatesOnlyTheLock(t *testing.T) {
	root, store := scaffold(t)
	before, err := treeHashes(root)
	if err != nil {
		t.Fatal(err)
	}
	e := load(t, store)
	if _, err := e.Observe(ObserveRequest{DocID: "payments"}); err != nil {
		t.Fatal(err)
	}
	after, err := treeHashes(root)
	if err != nil {
		t.Fatal(err)
	}
	changed := diffKeys(before, after)
	if len(changed) != 1 || changed[0] != ".vanguard/vanguard.lock" {
		t.Fatalf("observe must touch only the lock, changed: %v", changed)
	}
}

// Q-5.d-1: check writes zero bytes — read-only over every mode.
func TestCheckMutatesNothing(t *testing.T) {
	root, store := scaffoldMulti(t, map[string]string{"billing": "src/billing/**"})
	e := load(t, store)
	before, err := treeHashes(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Check(Selection{Mode: ModeAll}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Check(Selection{Mode: ModeDoc, DocID: "payments"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Check(Selection{Mode: ModePath, Path: "src/payments/api.go", PathIsDir: false}); err != nil {
		t.Fatal(err)
	}
	res, err := e.Check(Selection{Mode: ModeAll, RunBindings: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.ApplyBindings(res.Envelope); err != nil {
		t.Fatal(err)
	}
	after, err := treeHashes(root)
	if err != nil {
		t.Fatal(err)
	}
	if changed := diffKeys(before, after); len(changed) != 0 {
		t.Fatalf("check must write nothing, changed: %v", changed)
	}
}

// Q-4.a-1: the refusal matrix — every refusal kind carries its designed bit.
func TestRefusalMatrix(t *testing.T) {
	base, store := scaffold(t)
	e := load(t, store)

	// unknown document id and bare observe are usage errors, not refusals.
	if _, err := e.Observe(ObserveRequest{DocID: "ghost"}); err == nil ||
		!strings.Contains(err.Error(), "no document with id") {
		t.Fatalf("unknown id must be a usage error, got %v", err)
	}
	if _, err := e.Observe(ObserveRequest{}); err == nil ||
		!strings.Contains(err.Error(), "usage error") {
		t.Fatalf("bare observe must be a usage error, got %v", err)
	}
	if _, err := e.Observe(ObserveRequest{DocID: "payments", Prune: true}); err == nil ||
		!strings.Contains(err.Error(), "standalone invocation") {
		t.Fatalf("prune combined with a doc id must be a usage error, got %v", err)
	}

	// committed baseline, then time abuse is refused with bit 1.
	commitAll(t, base)
	e = load(t, store)
	if _, err := e.Observe(ObserveRequest{DocID: "payments"}); err != nil {
		t.Fatal(err)
	}
	e2 := load(t, store)
	res, err := e2.Observe(ObserveRequest{DocID: "payments",
		At: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}) // future vs Now
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Refusals) != 1 || res.Refusals[0].Reason != "future or earlier observed_at" ||
		res.Envelope.ExitBits&BitFatal == 0 {
		t.Fatalf("future --at must refuse with bit 1: %+v bits=%d", res.Refusals, res.Envelope.ExitBits)
	}
	res, err = e2.Observe(ObserveRequest{DocID: "payments",
		At: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}) // before the first attestation
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Refusals) != 1 || res.Envelope.ExitBits&BitFatal == 0 {
		t.Fatalf("backward --at must refuse with bit 1: %+v", res.Refusals)
	}

	// dirty watch scope refuses with bit 4.
	wf(t, base, "src/payments/uncommitted.go", "package payments\n")
	e3 := load(t, store)
	res, err = e3.Observe(ObserveRequest{DocID: "payments"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Refusals) != 1 || res.Envelope.ExitBits&BitDirty == 0 ||
		len(res.Refusals[0].Offending) == 0 {
		t.Fatalf("dirty scope must refuse with bit 4 and name offending paths: %+v", res.Refusals)
	}

	// an uncommitted document refuses with bit 1 (git mode requires the
	// document itself to be committed).
	root2, store2 := scaffold(t)
	wf(t, root2, ".vanguard/docs/newdoc.md",
		"---\nid: newdoc\nschema: none\n---\n# newdoc\n")
	e4 := load(t, store2)
	res, err = e4.Observe(ObserveRequest{DocID: "newdoc"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Refusals) != 1 || res.Refusals[0].Reason != "document not committed" ||
		res.Envelope.ExitBits&BitFatal == 0 {
		t.Fatalf("uncommitted document must refuse with bit 1: %+v bits=%d", res.Refusals, res.Envelope.ExitBits)
	}
}

// Q-1.1.e-2 / Q-4.d-2: --all attests only the unobserved set; a stale
// document is never auto-attested.
func TestObserveAllTouchesOnlyUnobserved(t *testing.T) {
	root, store := scaffoldMulti(t, map[string]string{"billing": "src/billing/**"})
	e := load(t, store)
	if _, err := e.Observe(ObserveRequest{DocID: "payments"}); err != nil {
		t.Fatal(err)
	}
	// payments becomes stale; billing stays unobserved.
	wf(t, root, "src/payments/api.go", "package payments // churn\n")
	commitAll(t, root)

	e2 := load(t, store)
	res, err := e2.Observe(ObserveRequest{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Envelope.Pruned) != 0 {
		t.Fatalf("--all must not prune: %v", res.Envelope.Pruned)
	}
	lock, _, err := lockfile.Load(filepath.Join(store, "vanguard.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lock.Docs["billing"]; !ok {
		t.Fatal("--all must attest the unobserved billing document")
	}
	if _, ok := lock.Docs["payments"]; !ok {
		t.Fatal("payments was already attested and must stay so")
	}
	// payments is stale again only after churn; --all must NOT have
	// refreshed it: its observed_at must be the earlier one.
	if lock.Docs["payments"].ObservedAt != "2026-11-03T12:00:00Z" {
		t.Fatalf("--all must not re-attest a stale document: %s", lock.Docs["payments"].ObservedAt)
	}
}

// Q-1.1.b-2: A -> B -> A is fresh, with attribution naming both commits.
func TestAtoBtoAFreshWithAttribution(t *testing.T) {
	root, store := scaffold(t)
	wf(t, root, "src/payments/api.go", "package payments // state A\n")
	commitAll(t, root)
	e := load(t, store)
	if _, err := e.Observe(ObserveRequest{DocID: "payments"}); err != nil {
		t.Fatal(err)
	}
	// B churns, A' restores the exact A bytes.
	wf(t, root, "src/payments/api.go", "package payments // state B\n")
	commitAll(t, root)
	wf(t, root, "src/payments/api.go", "package payments // state A\n")
	commitAll(t, root)

	e2 := load(t, store)
	res, err := e2.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	d := res.Envelope.Documents[0]
	if d.Status != StatusFresh {
		t.Fatalf("A->B->A must be fresh, got %s (changed=%v)", d.Status, d.ChangedPaths)
	}
	if len(d.IntroducingCommits) != 2 {
		t.Fatalf("attribution must name the two commits, got %v", d.IntroducingCommits)
	}
	if d.DocumentChanged || len(d.TriggeredWatches) != 0 {
		t.Fatalf("net-fresh state must not trigger watches: %+v", d)
	}
}

// Q-2.2.e-1: an entry whose shape does not fit the current provider mode
// reclassifies as unobserved with a hint — never an internal error.
func TestShapeMismatchReclassifiesAsUnobserved(t *testing.T) {
	root, store := scaffold(t)
	commitAll(t, root)
	e := load(t, store)
	if _, err := e.Observe(ObserveRequest{DocID: "payments"}); err != nil {
		t.Fatal(err)
	}
	// Corrupt the entry: strip the anchor (a git-mode field) and serialize.
	lockPath := filepath.Join(store, "vanguard.lock")
	lock, raw, err := lockfile.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	entry := lock.Docs["payments"]
	entry.Anchor = "" // git-mode entry without an anchor: shape mismatch
	lock.Docs["payments"] = entry
	if err := lockfile.Save(lockPath, lock, raw); err != nil {
		t.Fatal(err)
	}
	e2 := load(t, store)
	res, err := e2.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatalf("shape mismatch must not be an internal error: %v", err)
	}
	d := res.Envelope.Documents[0]
	if d.Status != StatusUnobserved {
		t.Fatalf("shape mismatch must reclassify as unobserved, got %s", d.Status)
	}
	if len(d.Hints) == 0 || !strings.Contains(d.Hints[0], "observe") {
		t.Fatalf("shape mismatch must carry an actionable hint: %v", d.Hints)
	}
}

// Q-3.d-1: the clone-kind matrix — every degradation carries a hint and no
// internal error: full repo with lock, repo without lock, non-git directory.
func TestCloneKindMatrix(t *testing.T) {
	// kind 1: full repo + lock (the scaffold).
	_, store := scaffoldMulti(t, map[string]string{"billing": "src/billing/**"})
	e := load(t, store)
	res, err := e.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatalf("full clone must check cleanly: %v", err)
	}
	if res.Envelope.Schema != SchemaVersion {
		t.Fatalf("schema version = %d", res.Envelope.Schema)
	}

	// kind 2: repo without a lockfile — lock-lost degradation, not a crash.
	_, store2 := scaffold(t)
	if err := os.Remove(filepath.Join(store2, "vanguard.lock")); err != nil {
		t.Fatal(err)
	}
	e2 := load(t, store2)
	res2, err := e2.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatalf("missing lock must degrade, not crash: %v", err)
	}
	if !res2.Envelope.LockLost {
		t.Fatal("missing lock with all-unobserved must report lock_lost")
	}

	// kind 3: no git at all — hash mode, still a valid check.
	root3, store3 := scaffold(t)
	if err := os.RemoveAll(filepath.Join(root3, ".git")); err != nil {
		t.Fatal(err)
	}
	e3, err := Load(store3, Options{Now: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("hash mode must load: %v", err)
	}
	if e3.Prov.GitMode() {
		t.Fatal("a store without .git must fall back to hash mode")
	}
	if _, err := e3.Check(Selection{Mode: ModeAll}); err != nil {
		t.Fatalf("hash mode must check: %v", err)
	}
}

// Q-3.d-2: an orphaned anchor is a soft degradation with attribution, never
// a hard failure.
func TestOrphanAnchorIsSoft(t *testing.T) {
	root, store := scaffold(t)
	commitAll(t, root)
	e := load(t, store)
	if _, err := e.Observe(ObserveRequest{DocID: "payments"}); err != nil {
		t.Fatal(err)
	}
	// Force the orphan path: forge the anchor to an unreachable oid AND the
	// scope hash to a baseline that exists nowhere in history (same kind
	// prefix, garbage digest), then churn the scope. Recovery must fail,
	// the document degrades to stale-orphaned, and nothing is fatal.
	lockPath := filepath.Join(store, "vanguard.lock")
	lock, raw, err := lockfile.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	entry := lock.Docs["payments"]
	entry.Anchor = "0000000000000000000000000000000000000001"
	parts := strings.SplitN(entry.ScopeTreeHash, ":", 2)
	kind := entry.ScopeTreeHash
	if len(parts) == 2 {
		kind = parts[0]
	}
	entry.ScopeTreeHash = kind + ":deadbeef00000000000000000000000000000000000000000000000000000000"
	lock.Docs["payments"] = entry
	if err := lockfile.Save(lockPath, lock, raw); err != nil {
		t.Fatal(err)
	}
	wf(t, root, "src/payments/api.go", "package payments // churn\n")
	commitAll(t, root)
	e2 := load(t, store)
	res, err := e2.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatalf("orphan anchor must degrade, not crash: %v", err)
	}
	d := res.Envelope.Documents[0]
	if !d.AnchorOrphaned {
		t.Fatalf("orphan anchor must be flagged: %+v", d)
	}
	if res.Envelope.ExitBits&BitFatal != 0 {
		t.Fatalf("orphan anchor is soft — no fatal bit: %d", res.Envelope.ExitBits)
	}
}

// Q-4.b-1 / Q-4.b-2: every hint across every scenario is a parseable,
// runnable vanguard command.
func TestHintsAreRunnableCommands(t *testing.T) {
	mk1 := func(t *testing.T) (string, string) {
		return scaffoldMulti(t, map[string]string{"billing": "src/billing/**"})
	}
	mk2 := func(t *testing.T) (string, string) { return scaffold(t) }
	seen := 0
	for _, mk := range []func(*testing.T) (string, string){mk1, mk2} {
		_, store := mk(t)
		e := load(t, store)
		res, err := e.Check(Selection{Mode: ModeAll})
		if err != nil {
			t.Fatal(err)
		}
		var hints []string
		for _, d := range res.Envelope.Documents {
			hints = append(hints, d.Hints...)
		}
		for _, h := range hints {
			seen++
			m := regexp.MustCompile(`vanguard ([a-z]+)`).FindStringSubmatch(h)
			if m == nil {
				t.Fatalf("hint carries no parseable command: %q", h)
			}
			switch m[1] {
			case "check", "observe", "init", "verify":
			default:
				t.Fatalf("hint references an unknown subcommand: %q", h)
			}
		}
	}
	if seen == 0 {
		t.Fatal("expected at least one hint across scenarios")
	}
}

// Q-4.c-2: golden envelope per mode — the shape is pinned for a fixed store
// and clock.
func TestGoldenEnvelopesPerMode(t *testing.T) {
	root, store := scaffoldMulti(t, map[string]string{"billing": "src/billing/**"})
	e := load(t, store)
	res, err := e.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	j, err := Render(res.Envelope, "json")
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(j), &env); err != nil {
		t.Fatal(err)
	}
	if env["schema"] != float64(SchemaVersion) || env["mode"] != "all" {
		t.Fatalf("golden header mismatch: %v", env)
	}
	if env["exit_bits"] != float64(0) {
		t.Fatalf("bootstrap gaps set no bit: %v", env["exit_bits"])
	}
	docs, ok := env["documents"].([]any)
	if !ok || len(docs) != 2 {
		t.Fatalf("golden documents: %v", env["documents"])
	}
	got := []string{}
	for _, d := range docs {
		dm := d.(map[string]any)
		got = append(got, dm["doc"].(string))
		if dm["status"] != "unobserved" {
			t.Fatalf("unobserved bootstrap expected, got %v", dm["status"])
		}
	}
	if !sort.StringsAreSorted(got) || got[0] != "billing" || got[1] != "payments" {
		t.Fatalf("golden doc ids: %v", got)
	}

	// doc mode pins a single document.
	res2, err := e.Check(Selection{Mode: ModeDoc, DocID: "payments"})
	if err != nil {
		t.Fatal(err)
	}
	j2, err := Render(res2.Envelope, "json")
	if err != nil {
		t.Fatal(err)
	}
	var env2 map[string]any
	if err := json.Unmarshal([]byte(j2), &env2); err != nil {
		t.Fatal(err)
	}
	if env2["mode"] != "doc" || env2["documents"].([]any)[0].(map[string]any)["doc"] != "payments" {
		t.Fatalf("doc-mode golden mismatch: %v", env2)
	}
	if _, has := env2["unwatched"]; has {
		t.Fatal("doc mode must not carry the all-mode unwatched axis")
	}

	// path mode pins only the documents claiming the looked-up path.
	res3, err := e.Check(Selection{Mode: ModePath, Path: "src/billing/x.go", PathIsDir: false})
	if err != nil {
		t.Fatal(err)
	}
	j3, err := Render(res3.Envelope, "json")
	if err != nil {
		t.Fatal(err)
	}
	var env3 map[string]any
	if err := json.Unmarshal([]byte(j3), &env3); err != nil {
		t.Fatal(err)
	}
	if env3["mode"] != "path" {
		t.Fatalf("path-mode golden mismatch: %v", env3)
	}
	docs3 := env3["documents"].([]any)
	if len(docs3) != 1 || docs3[0].(map[string]any)["doc"] != "billing" {
		t.Fatalf("path lookup must answer exactly billing: %v", docs3)
	}

	// range mode reports the affected set between two refs.
	repo, err := git.PlainOpen(root)
	if err != nil {
		t.Fatal(err)
	}
	headRef, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	head := headRef.Hash().String()
	logIter, err := repo.Log(&git.LogOptions{From: headRef.Hash()})
	if err != nil {
		t.Fatal(err)
	}
	var base string
	count := 0
	if err := logIter.ForEach(func(c *object.Commit) error {
		count++
		base = c.Hash.String()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count < 2 {
		t.Fatalf("scaffold must have >=2 commits, got %d", count)
	}
	res4, err := e.Check(Selection{Mode: ModeRange, From: base, To: head})
	if err != nil {
		t.Fatal(err)
	}
	j4, err := Render(res4.Envelope, "json")
	if err != nil {
		t.Fatal(err)
	}
	var env4 map[string]any
	if err := json.Unmarshal([]byte(j4), &env4); err != nil {
		t.Fatal(err)
	}
	if env4["mode"] != "range" {
		t.Fatalf("range-mode golden mismatch: %v", env4)
	}
	if _, has := env4["affected_in_range"]; !has {
		t.Fatalf("range mode must carry affected_in_range: %v", env4)
	}
}

// Q-1.1.b-1: the three anchor-resolution paths (fast, recovery, fallback)
// agree on the status for the same net state — reachable anchor and
// recovered baseline both land on the net-state rule, the orphan degrades
// softly, and nothing misclassifies.
func TestRecoveredAnchorPathAgreesWithFastPath(t *testing.T) {
	root, store := scaffold(t)
	wf(t, root, "src/payments/api.go", "package payments // state A\n")
	wf(t, root, "src/payments/extra.go", "package payments // extra A\n")
	commitAll(t, root)
	e := load(t, store)
	if _, err := e.Observe(ObserveRequest{DocID: "payments"}); err != nil {
		t.Fatal(err)
	}
	// B: churn BOTH files; C: revert only api.go — the current tree no
	// longer matches the baseline (extra.go differs) but the ancestor
	// baseline exists in history: the recovery path must find it.
	wf(t, root, "src/payments/api.go", "package payments // state B\n")
	wf(t, root, "src/payments/extra.go", "package payments // extra B\n")
	commitAll(t, root)
	wf(t, root, "src/payments/api.go", "package payments // state A\n")
	commitAll(t, root)

	// Forge the anchor to an unreachable oid so only the hash walk can
	// recover the baseline.
	lockPath := filepath.Join(store, "vanguard.lock")
	lock, raw, err := lockfile.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	entry := lock.Docs["payments"]
	entry.Anchor = "0000000000000000000000000000000000000001"
	lock.Docs["payments"] = entry
	if err := lockfile.Save(lockPath, lock, raw); err != nil {
		t.Fatal(err)
	}

	e2 := load(t, store)
	res, err := e2.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	d := res.Envelope.Documents[0]
	if !d.AnchorRecovered {
		t.Fatalf("recovery path must recover the baseline from the hash: %+v", d)
	}
	if res.Envelope.ExitBits&BitFatal != 0 {
		t.Fatalf("recovered baseline must not be fatal: %d", res.Envelope.ExitBits)
	}
}

// Q-2.2.b-3: a same-document CAS conflict is recovered by re-running
// observe — the loser reloads the winner's bytes and its save succeeds.
func TestConflictRecoveredByRerun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vanguard.lock")
	captured := lockfile.Serialize(&lockfile.Lock{Version: 1, Docs: map[string]lockfile.Entry{}})
	if err := os.WriteFile(path, captured, 0o644); err != nil {
		t.Fatal(err)
	}
	// Winner writes a billing entry from the baseline.
	winner := &lockfile.Lock{Version: 1, Docs: map[string]lockfile.Entry{
		"billing": {ObservedAt: "2026-11-03T10:00:00Z", ContentHash: "sha256:bb"},
	}}
	if err := lockfile.Save(path, winner, captured); err != nil {
		t.Fatalf("winner: %v", err)
	}
	// The stale writer still holds the pre-winner capture: refused.
	loser := &lockfile.Lock{Version: 1, Docs: map[string]lockfile.Entry{
		"payments": {ObservedAt: "2026-11-03T11:00:00Z", ContentHash: "sha256:cc"},
	}}
	if err := lockfile.Save(path, loser, captured); err == nil {
		t.Fatal("stale capture must be refused")
	}
	// Re-run: load the winner's bytes and re-apply the loser's intent.
	fresh, raw2, err := lockfile.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	fresh.Docs["payments"] = loser.Docs["payments"]
	if err := lockfile.Save(path, fresh, raw2); err != nil {
		t.Fatalf("re-run must recover: %v", err)
	}
	parsed, err := lockfile.Parse(mustRead(t, path))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Docs) != 2 || parsed.Docs["payments"].ObservedAt != "2026-11-03T11:00:00Z" {
		t.Fatalf("merged lock wrong: %+v", parsed.Docs)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
