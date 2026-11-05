package check

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/vanguard-lint/vanguard/internal/docmodel"
	"github.com/vanguard-lint/vanguard/internal/lockfile"
	"github.com/vanguard-lint/vanguard/internal/pathmatch"
	"github.com/vanguard-lint/vanguard/internal/provider"
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
	res, err := e.check(selection)
	if err != nil {
		return nil, err
	}
	// Store-level advisories ride on every check — never an exit bit.
	res.Envelope.Warnings = append(res.Envelope.Warnings, e.advisories()...)
	return res, nil
}

// advisories collects the advisory warnings every check carries: discovery
// skips (file + reason) and orphaned lock entries whose id no longer exists
// in discovery.
func (e *Engine) advisories() []string {
	var out []string
	for _, w := range e.Warnings {
		out = append(out, fmt.Sprintf("discovery skipped %s: %s", w.File, w.Reason))
	}
	var orphans []string
	for id := range e.Lock.Docs {
		if docByID(e.Docs, id) == nil {
			orphans = append(orphans, id)
		}
	}
	sort.Strings(orphans)
	for _, id := range orphans {
		out = append(out, fmt.Sprintf(
			"orphaned lock entry %q — the id no longer exists in discovery; reclaim with: vanguard observe --prune", id))
	}
	return out
}

func (e *Engine) check(selection Selection) (*Result, error) {
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

// staleBit returns the exit-bit contribution of one document's staleness
// signals: any triggered watch axis or a document-side change sets bit 2.
func staleBit(d Document) int {
	if d.DocumentChanged || len(d.TriggeredWatches) > 0 {
		return BitStale
	}
	return 0
}

// applyStructure copies the document's declared-schema findings into the
// envelope and escalates them to the fatal bit: a document that violates
// the contract it declared is a broken store, loud by default.
func (e *Engine) applyStructure(doc *docmodel.Doc, d *Document, bits *int) {
	vs := e.StructureViolations[doc.ID]
	for _, v := range vs {
		d.Violations = append(d.Violations, Violation{
			Rule:    v.Rule,
			File:    v.File,
			Line:    v.Line,
			Message: v.Message,
		})
	}
	if len(vs) > 0 {
		*bits |= BitFatal
	}
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
		e.applyStructure(doc, &d, &bits)
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
		bits |= staleBit(d)
		if d.Status == StatusDirty {
			bits |= BitDirty
		}
	}
	sortStrings(env.Unwatched)

	// Gap categories set no bit by default; --strict elevates them.
	if sel.Strict && (unobservedCount > 0 || len(env.Unwatched) > 0) {
		bits |= BitGaps
	}
	sortStrings(env.Unobserved)

	// Lost lock: lockfile absent while every discovered document reports
	// unobserved — sets the stale bit by default, all mode only.
	if e.lockLost(unobservedCount, len(env.Unwatched)) {
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
	d := Document{Doc: doc.ID, Target: doc.Target}

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
		if len(doc.Bindings) > 0 && !sel.RunBindings {
			// Referenced-but-not-run: pending even without a baseline.
			d.Bindings = e.BindingsFor(doc.ID, doc.Bindings)
		}
		return d, true
	}
	if len(doc.Bindings) > 0 && !sel.RunBindings {
		// Referenced-but-not-run: the pending notation is informational,
		// never a status.
		d.Bindings = e.BindingsFor(doc.ID, doc.Bindings)
	}
	return e.evaluateObserved(doc, entry, &d, sel.MaxRecoveryWalk), true
}

// evaluateObserved computes the freshness chain for a document with a
// baseline: stale (any condition) > dirty (only uncommitted churn) > fresh.
// Every target-side question routes through the document's own provider —
// the store provider never answers a question about code.
func (e *Engine) evaluateObserved(doc *docmodel.Doc, entry lockfile.Entry, d *Document, maxWalk int) Document {
	stale := false
	prov := e.ProvFor(doc.Target)

	// Shape mismatch: the entry's kind must fit the current provider mode —
	// a mode flip reclassifies as unobserved, never mis-read.
	hasPaths := len(doc.PathGlobs) > 0
	if hasPaths {
		if prov.GitMode() && entry.Anchor == "" {
			d.Status = StatusUnobserved
			d.Hints = append(d.Hints, "target mode changed — run observe to record baselines in the current mode")
			return *d
		}
		if !prov.GitMode() && entry.ScopeSnapshot == nil {
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
		if prov.GitMode() {
			stalePaths, changed, commits, kindMismatch := e.gitPathsAxis(prov, doc, entry, d, maxWalk)
			if len(changed) > 0 {
				// Attribution is informational on every path — fresh or
				// stale, it names what touched the scope.
				d.ChangedPaths = changed
				d.IntroducingCommits = commits
			}
			if stalePaths {
				stale = true
				d.TriggeredWatches = append(d.TriggeredWatches, "paths")
				if !kindMismatch {
					// Commit-hunting advice only fits a real net change;
					// a kind mismatch moved the baseline itself.
					d.Hints = append(d.Hints,
						fmtHint("inspect the commits, update the document, commit, then run: vanguard observe %s", doc.ID))
				}
			}
		} else {
			if e.hashPathsAxis(prov, doc, entry) {
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
	// watch scope. Provider warnings (the blobless-clone loud case) ride
	// along as hints — advisory, never a status.
	if dirty, warnings, err := prov.Dirty(doc.PathGlobs); err == nil {
		if len(warnings) > 0 {
			d.Hints = append(d.Hints, warnings...)
		}
		if len(dirty) > 0 {
			d.Status = StatusDirty
			// A machine consumer must not need a second observe run to
			// learn what churned: the what rides on the document, the next
			// command rides in its hints.
			d.ChangedPaths = dirty
			d.Hints = append(d.Hints, "commit or stash the churn in the watch scope, then re-run check")
			return *d
		}
	}
	d.Status = StatusFresh
	return *d
}

// gitPathsAxis decides the paths axis and attribution for git mode. prov is
// the document's own target provider.
func (e *Engine) gitPathsAxis(prov provider.Provider, doc *docmodel.Doc, entry lockfile.Entry, d *Document, maxWalk int) (stale bool, changed, commits []string, kindMismatch bool) {
	head, err := prov.HEAD()
	if err != nil || head == "" {
		// Unborn target: nothing committed — the scope cannot match the
		// attested state.
		return true, nil, nil, false
	}
	current, err := prov.ScopeHash(doc.PathGlobs)
	if err != nil {
		return true, nil, nil, false
	}
	if !scope.SameKind(current, entry.ScopeTreeHash) {
		// Kind mismatch means the globs or the scope-hash scheme changed —
		// stale with a re-run-observe hint, never an internal error. No
		// commit attribution: there is nothing to inspect, the attestation
		// baseline itself moved.
		d.Hints = append(d.Hints, "the watch globs or the scope-hash scheme changed since the attestation — re-run observe")
		return true, nil, nil, true
	}
	if current == entry.ScopeTreeHash {
		// Net-fresh: the state matches the attestation. Attribution stays
		// informational — A -> B -> A is fresh AND names the commits that
		// touched the scope since the anchor (Q-1.1.b-2).
		reachable, rerr := prov.AnchorReachable(entry.Anchor)
		if rerr == nil && reachable {
			changed, commits, _ = prov.Attribution(entry.Anchor, head, doc.PathGlobs)
		} else if rerr == nil {
			// Unreachable anchor under an identical net state (amend/squash
			// rewrote history): the design's recovery flow still applies —
			// a baseline matched inside the bound is a recovered anchor,
			// exhaustion is an orphaned one. Either way the note surfaces;
			// history rewriting under a fresh attestation is never silent.
			if recovered, werr := prov.RecoveredAnchor(doc.PathGlobs, entry.ScopeTreeHash, e.recoveryBound(maxWalk)); werr == nil && recovered != "" {
				d.AnchorRecovered = true
			} else {
				d.AnchorOrphaned = true
			}
		}
		return false, changed, commits, false
	}
	// Stale. Attribution availability follows the three-path resolution;
	// status is already decided by the net-state rule.
	reachable, err := prov.AnchorReachable(entry.Anchor)
	if err == nil && reachable {
		changed, commits, _ = prov.Attribution(entry.Anchor, head, doc.PathGlobs)
		return true, changed, commits, false
	}
	recovered, err := prov.RecoveredAnchor(doc.PathGlobs, entry.ScopeTreeHash, e.recoveryBound(maxWalk))
	if err == nil && recovered != "" {
		d.AnchorRecovered = true
		changed, commits, _ = prov.Attribution(recovered, head, doc.PathGlobs)
		return true, changed, commits, false
	}
	d.AnchorOrphaned = true
	return true, nil, nil, false
}

func (e *Engine) recoveryBound(maxWalk int) int {
	if maxWalk > 0 {
		return maxWalk
	}
	return 1000
}

// hashPathsAxis compares current scoped content hashes against the snapshot.
func (e *Engine) hashPathsAxis(prov provider.Provider, doc *docmodel.Doc, entry lockfile.Entry) bool {
	snap, err := prov.Snapshot(doc.PathGlobs)
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

// checkDoc resolves the argument as an exact document id — the only
// per-document selector (paths belong to check --path).
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
	bits := staleBit(d)
	if d.Status == StatusDirty {
		bits |= BitDirty
	}
	e.applyStructure(doc, &d, &bits)
	env.Documents[len(env.Documents)-1] = d
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
		var bitsDoc int
		e.applyStructure(doc, &d, &bitsDoc)
		if d.Status == StatusFresh && len(d.Violations) == 0 {
			continue // fresh matches are omitted
		}
		if d.Status == StatusUnobserved {
			d.Hints = append(d.Hints, "run a full vanguard check to see whether the whole lock is missing")
		}
		env.Documents = append(env.Documents, d)
		bits |= staleBit(d)
		if d.Status == StatusDirty {
			bits |= BitDirty
		}
		bits |= bitsDoc
	}
	env.ExitBits = bits
	sortDocs(env.Documents)
	res.Envelope = env
	return res, nil
}

// checkRange — Path-Hit Attribution, lockfile-free. affected ≠ stale.
func (e *Engine) checkRange(sel Selection) (*Result, error) {
	if !e.Prov.GitMode() && len(e.targetProviders) == 0 {
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
		prov := e.ProvFor(doc.Target)
		if !prov.GitMode() {
			continue // a git-less target has no history to attribute
		}
		changed, commits, err := prov.Attribution(sel.From, sel.To, doc.PathGlobs)
		if err != nil {
			return nil, err
		}
		if len(changed) == 0 && len(commits) == 0 {
			continue
		}
		env.AffectedInRange = append(env.AffectedInRange, doc.ID)
		env.Documents = append(env.Documents, Document{
			Doc:                doc.ID,
			Target:             doc.Target,
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
		results, anyFailed := e.RunBindings(doc.ID, doc.Target, doc.Bindings)
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
