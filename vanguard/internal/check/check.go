package check

import (
	"fmt"
	"os"
	"time"

	"github.com/vanguard-lint/vanguard/internal/docmodel"
	"github.com/vanguard-lint/vanguard/internal/lockfile"
	"github.com/vanguard-lint/vanguard/internal/pathmatch"
	"github.com/vanguard-lint/vanguard/internal/scope"
)

// Result is one check run: the envelope plus the run-level fatal report.
type Result struct {
	Envelope *Envelope
	Fatals   []Violation
	Refusals []Refused
}

// Check computes freshness for the selected documents in the requested mode.
// Selection: all documents (mode all), one id or docs/ path (mode doc), a
// path reverse lookup (mode path), or a ref range (mode range).
func (e *Engine) Check(selection Selection) (*Result, error) {
	switch selection.Mode {
	case ModeAll:
		return e.checkAll(selection)
	case ModeDoc:
		return e.checkDoc(selection)
	case ModePath:
		return e.checkPath(selection)
	case ModeRange:
		return e.checkRange(selection)
	default:
		return nil, usageError("unknown mode %q", selection.Mode)
	}
}

// Selection names what a check run covers.
type Selection struct {
	Mode            string
	DocID           string
	Path            string
	PathIsDir       bool
	From, To        string
	Strict          bool
	FailOnAffected  bool
	RunBindings     bool
	MaxRecoveryWalk int
}

// checkAll — two-phase: structure (discovery already fails closed on parse
// and id errors; schema enforcement is phase-1 for later milestones), then
// freshness for every document.
func (e *Engine) checkAll(sel Selection) (*Result, error) {
	res := &Result{}
	env := &Envelope{Mode: ModeAll, Schema: SchemaVersion, Documents: []Document{}}
	bits := 0

	unobservedCount := 0
	for i := range e.Docs {
		doc := &e.Docs[i]
		d, watched := e.evaluate(doc, sel)
		if !watched {
			env.Unwatched = append(env.Unwatched, doc.ID)
		}
		if d.Status == StatusUnobserved {
			unobservedCount++
			env.Unobserved = append(env.Unobserved, doc.ID)
		}
		if d.Status != "" {
			env.Documents = append(env.Documents, d)
		}
		if d.DocumentChanged || containsString(d.TriggeredWatches, "paths") {
			bits |= BitStale
		}
		if d.Status == StatusDirty {
			bits |= BitDirty
		}
	}
	sortStrings(env.Unwatched)

	// Gap categories set no bit by default; --strict elevates them.
	if sel.Strict && (unobservedCount > 0 || len(env.Unwatched) > 0) {
		bits |= BitGaps
	}

	// Lost lock: lockfile absent while every discovered document reports
	// unobserved — sets the stale bit by default, all mode only.
	if e.lockLost(unobservedCount) {
		env.LockLost = true
		bits |= BitStale
		env.Hints = append(env.Hints,
			"lock lost: restore from git (git checkout -- .vanguard/vanguard.lock) or bootstrap (vanguard init, then observe --all)")
	}
	env.ExitBits = bits
	sortDocs(env.Documents)
	res.Envelope = env
	return res, nil
}

// evaluate computes one document's freshness. Returns (entry, watched):
// watched=false means the document declares no watches (unwatched section).
func (e *Engine) evaluate(doc *docmodel.Doc, sel Selection) (Document, bool) {
	d := Document{Doc: doc.ID}

	hasPaths := len(doc.PathGlobs) > 0
	hasClock := doc.ReviewAfterSeconds > 0
	if !hasPaths && !hasClock {
		// No watches: unwatched. Document-side drift still applies once
		// observed — an observed unwatched document that changed is stale
		// by the document axis.
		if entry, ok := e.Lock.Docs[doc.ID]; ok {
			if e.ContentHash(doc) != entry.ContentHash {
				d.Status = StatusStale
				d.DocumentChanged = true
				d.Hints = append(d.Hints,
					fmtHint("re-run vanguard observe %s after committing the edit", doc.ID))
			}
		}
		return d, false
	}

	entry, ok := e.Lock.Docs[doc.ID]
	if !ok {
		d.Status = StatusUnobserved
		d.Hints = append(d.Hints,
			fmtHint("never attested — run: vanguard observe %s", doc.ID))
		return d, true
	}
	return e.evaluateObserved(doc, entry, &d), true
}

// evaluateObserved computes the freshness chain for a document with a
// baseline: stale (any condition) > dirty (only uncommitted churn) > fresh.
func (e *Engine) evaluateObserved(doc *docmodel.Doc, entry lockfile.Entry, d *Document) Document {
	stale := false

	// Shape mismatch: the entry's kind must fit the current provider mode —
	// a mode flip reclassifies as unobserved, never mis-read.
	hasPaths := len(doc.PathGlobs) > 0
	if hasPaths {
		if e.Prov.GitMode() && entry.Anchor == "" {
			d.Status = StatusUnobserved
			d.Hints = append(d.Hints, "target mode changed — run observe to record baselines in the current mode")
			return *d
		}
		if !e.Prov.GitMode() && entry.ScopeSnapshot == nil {
			d.Status = StatusUnobserved
			d.Hints = append(d.Hints, "target mode changed — run observe to record baselines in the current mode")
			return *d
		}
	}

	// Document axis: content_hash covers the whole file, frontmatter
	// included.
	if e.ContentHash(doc) != entry.ContentHash {
		stale = true
		d.DocumentChanged = true
		d.Hints = append(d.Hints,
			fmtHint("re-run vanguard observe %s after committing the edit", doc.ID))
	}

	// Clock axis: review_after compares against the attestation instant.
	if doc.ReviewAfterSeconds > 0 {
		observed, err := time.Parse(time.RFC3339, entry.ObservedAt)
		if err != nil {
			// A corrupt observed_at fails closed on this document.
			d.Status = StatusStale
			d.Violations = append(d.Violations, Violation{
				Rule: "lock-entry", File: ".vanguard/vanguard.lock",
				Message: fmtHint("entry %q has an unparseable observed_at %q", doc.ID, entry.ObservedAt),
			})
			return *d
		}
		if e.Now.Sub(observed).Seconds() > doc.ReviewAfterSeconds {
			stale = true
			d.TriggeredWatches = append(d.TriggeredWatches, "review_after")
			d.Hints = append(d.Hints,
				fmtHint("review_after exceeded — run: vanguard observe %s to record the review act", doc.ID))
		}
	}

	// Paths axis: net-state rule — compare the scope hash at HEAD (or the
	// snapshot) against the lock. History is attribution only.
	if hasPaths {
		if e.Prov.GitMode() {
			stalePaths, changed, commits := e.gitPathsAxis(doc, entry, d)
			if stalePaths {
				stale = true
				d.TriggeredWatches = append(d.TriggeredWatches, "paths")
				d.ChangedPaths = changed
				d.IntroducingCommits = commits
				d.Hints = append(d.Hints,
					fmtHint("inspect the commits, update the document, commit, then run: vanguard observe %s", doc.ID))
			}
		} else {
			if e.hashPathsAxis(doc, entry) {
				stale = true
				d.TriggeredWatches = append(d.TriggeredWatches, "paths")
				d.Hints = append(d.Hints,
					fmtHint("watched files changed — run: vanguard observe %s", doc.ID))
			}
		}
	}

	if stale {
		d.Status = StatusStale
		return *d
	}
	// Dirty may only hold when staleness does not: uncommitted churn in the
	// watch scope.
	if dirty, err := e.Prov.Dirty(doc.PathGlobs); err == nil && len(dirty) > 0 {
		d.Status = StatusDirty
		return *d
	}
	d.Status = StatusFresh
	return *d
}

// gitPathsAxis decides the paths axis and attribution for git mode.
func (e *Engine) gitPathsAxis(doc *docmodel.Doc, entry lockfile.Entry, d *Document) (stale bool, changed, commits []string) {
	head, err := e.Prov.HEAD()
	if err != nil || head == "" {
		// Unborn target: nothing committed — the scope cannot match the
		// attested state.
		return true, nil, nil
	}
	current, err := e.Prov.ScopeHash(doc.PathGlobs)
	if err != nil {
		return true, nil, nil
	}
	if !scope.SameKind(current, entry.ScopeTreeHash) {
		// Kind mismatch means the globs changed — stale with a re-observe
		// hint, never an internal error.
		d.Hints = append(d.Hints, "the watch globs changed since the attestation — re-run observe")
		return true, nil, nil
	}
	if current == entry.ScopeTreeHash {
		return false, nil, nil
	}
	// Stale. Attribution availability follows the three-path resolution;
	// status is already decided by the net-state rule.
	reachable, err := e.Prov.AnchorReachable(entry.Anchor)
	if err == nil && reachable {
		changed, commits, _ = e.Prov.Attribution(entry.Anchor, head, doc.PathGlobs)
		return true, changed, commits
	}
	recovered, err := e.Prov.RecoveredAnchor(doc.PathGlobs, entry.ScopeTreeHash, e.recoveryBound())
	if err == nil && recovered != "" {
		d.AnchorRecovered = true
		changed, commits, _ = e.Prov.Attribution(recovered, head, doc.PathGlobs)
		return true, changed, commits
	}
	d.AnchorOrphaned = true
	return true, nil, nil
}

func (e *Engine) recoveryBound() int {
	return 1000
}

// hashPathsAxis compares current scoped content hashes against the snapshot.
func (e *Engine) hashPathsAxis(doc *docmodel.Doc, entry lockfile.Entry) bool {
	snap, err := e.Prov.Snapshot(doc.PathGlobs)
	if err != nil {
		return true // loud on IO failure
	}
	if len(snap) != len(entry.ScopeSnapshot) {
		return true
	}
	for path, sum := range snap {
		want, ok := entry.ScopeSnapshot[path]
		if !ok || want != sum {
			return true
		}
	}
	return false
}

// checkDoc resolves the argument by the fixed precedence: exact document id
// first, then a path into docs/.
func (e *Engine) checkDoc(sel Selection) (*Result, error) {
	doc := docByID(e.Docs, sel.DocID)
	if doc == nil {
		return nil, usageError("no document with id %q — documents live under .vanguard/docs/", sel.DocID)
	}
	res := &Result{}
	env := &Envelope{Mode: ModeDoc, Schema: SchemaVersion, Documents: []Document{}}
	d, watched := e.evaluate(doc, sel)
	if !watched {
		env.Unwatched = append(env.Unwatched, doc.ID)
	}
	env.Documents = append(env.Documents, d)
	bits := 0
	if d.DocumentChanged || containsString(d.TriggeredWatches, "paths") {
		bits |= BitStale
	}
	if d.Status == StatusDirty {
		bits |= BitDirty
	}
	env.ExitBits = bits
	res.Envelope = env
	return res, nil
}

// checkPath — reverse lookup: documents whose watch globs claim territory
// under the path, reporting stale or unobserved matches only.
func (e *Engine) checkPath(sel Selection) (*Result, error) {
	res := &Result{}
	env := &Envelope{Mode: ModePath, Schema: SchemaVersion, Documents: []Document{}}
	bits := 0
	for i := range e.Docs {
		doc := &e.Docs[i]
		claims := false
		for _, glob := range doc.PathGlobs {
			if pathmatch.CouldClaim(glob, sel.Path, sel.PathIsDir) {
				claims = true
				break
			}
		}
		if !claims {
			continue
		}
		d, _ := e.evaluate(doc, sel)
		if d.Status == StatusFresh {
			continue // fresh matches are omitted
		}
		if d.Status == StatusUnobserved {
			d.Hints = append(d.Hints, "run a full vanguard check to see whether the whole lock is missing")
		}
		env.Documents = append(env.Documents, d)
		if d.DocumentChanged || containsString(d.TriggeredWatches, "paths") {
			bits |= BitStale
		}
		if d.Status == StatusDirty {
			bits |= BitDirty
		}
	}
	env.ExitBits = bits
	sortDocs(env.Documents)
	res.Envelope = env
	return res, nil
}

// checkRange — Path-Hit Attribution, lockfile-free. affected ≠ stale.
func (e *Engine) checkRange(sel Selection) (*Result, error) {
	if !e.Prov.GitMode() {
		return nil, usageError("range attribution is unavailable — the target has no git history")
	}
	res := &Result{}
	env := &Envelope{Mode: ModeRange, Schema: SchemaVersion, Documents: []Document{}}
	bits := 0
	for i := range e.Docs {
		doc := &e.Docs[i]
		if len(doc.PathGlobs) == 0 {
			continue // no path scope exists to ask
		}
		changed, commits, err := e.Prov.Attribution(sel.From, sel.To, doc.PathGlobs)
		if err != nil {
			return nil, err
		}
		if len(changed) == 0 && len(commits) == 0 {
			continue
		}
		env.AffectedInRange = append(env.AffectedInRange, doc.ID)
		env.Documents = append(env.Documents, Document{
			Doc:                doc.ID,
			ChangedPaths:       changed,
			IntroducingCommits: commits,
		})
	}
	sortStrings(env.AffectedInRange)
	sortDocs(env.Documents)
	if sel.FailOnAffected && len(env.AffectedInRange) > 0 {
		bits |= BitAffectedInRange
	}
	env.ExitBits = bits
	res.Envelope = env
	return res, nil
}

// ApplyBindings resolves or executes the binding references for every
// document in the envelope. Without execution, references report as pending;
// with execution the provenance line goes to stderr and any failure sets
// bit 16. Binding results are transient — never persisted.
func (e *Engine) ApplyBindings(env *Envelope) error {
	commands := len(e.Cfg.Bindings)
	fmt.Fprintln(os.Stderr, e.ProvenanceLine(commands))
	for i := range env.Documents {
		doc := docByID(e.Docs, env.Documents[i].Doc)
		if doc == nil || len(doc.Bindings) == 0 {
			continue
		}
		results, anyFailed := e.RunBindings(doc.ID, doc.Bindings)
		env.Documents[i].Bindings = results
		if anyFailed {
			env.ExitBits |= BitBindingFailed
		}
	}
	return nil
}

func sortDocs(docs []Document) {
	for i := 1; i < len(docs); i++ {
		for j := i; j > 0 && docs[j].Doc < docs[j-1].Doc; j-- {
			docs[j], docs[j-1] = docs[j-1], docs[j]
		}
	}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// UsageError marks a bad invocation (exit code 1 with a hint, not a crash).
type UsageError struct{ msg string }

func (e *UsageError) Error() string { return e.msg }

func usageError(format string, args ...any) error {
	return &UsageError{msg: fmt.Sprintf(format, args...)}
}
