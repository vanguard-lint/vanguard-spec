package check

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/vanguard-lint/vanguard/internal/docmodel"
	"github.com/vanguard-lint/vanguard/internal/lockfile"
)

// ObserveRequest names what to attest.
type ObserveRequest struct {
	DocID string // required unless All or Prune
	All   bool   // guarded bootstrap hatch: only the unobserved set
	Prune bool   // standalone gc pass; cannot combine with the above
	At    time.Time
}

// Observe attests documents — the only lockfile-mutating path. Preconditions
// per document: structure verifies (discovery already enforced it), the
// watch scope is clean, and the document itself is committed (git mode).
// Refusals are per-document and differentiated by kind; a CAS conflict
// aborts the whole run with no write.
func (e *Engine) Observe(req ObserveRequest) (*Result, error) {
	if req.Prune {
		if req.DocID != "" || req.All {
			return nil, usageError("--prune is a standalone invocation — it cannot combine with a doc id or --all")
		}
		return e.prune()
	}
	if req.DocID == "" && !req.All {
		return nil, usageError("the document id is required — mass-attestation without --all is a usage error")
	}

	var targets []*docmodel.Doc
	if req.All {
		// The guarded sweep walks only the currently-unobserved set —
		// never stale documents.
		for i := range e.Docs {
			if _, ok := e.Lock.Docs[e.Docs[i].ID]; !ok {
				targets = append(targets, &e.Docs[i])
			}
		}
	} else {
		doc := docByID(e.Docs, req.DocID)
		if doc == nil {
			return nil, usageError("no document with id %q — documents live under .vanguard/docs/", req.DocID)
		}
		targets = []*docmodel.Doc{doc}
	}

	res := &Result{}
	env := &Envelope{Mode: ModeObserve, Schema: SchemaVersion, Documents: []Document{}}
	bits := 0
	changed := false

	// Repository state guards (git mode), per target: a half-finished
	// merge/rebase is not a stable code state to attest — the documents
	// bound to that target refuse. A detached HEAD is allowed but noted:
	// the anchor describes that exact commit.
	stateChecked := map[string]bool{}
	for _, doc := range targets {
		prov := e.ProvFor(doc.Target)
		if stateChecked[doc.Target] {
			continue
		}
		stateChecked[doc.Target] = true
		gi, ok := prov.(interface {
			HeadState() (detached, midMerge bool, err error)
		})
		if !ok {
			continue
		}
		detached, midMerge, stateErr := gi.HeadState()
		if stateErr != nil {
			continue
		}
		if midMerge {
			for _, d := range targets {
				if d.Target != doc.Target {
					continue
				}
				res.Refusals = append(res.Refusals, Refused{Doc: d.ID,
					Reason: "merge or rebase in progress",
					Hints:  []string{"finalize the merge/rebase, then observe"}})
			}
			env.Lock = LockReport{Changed: false, Path: lockRelPath(e.LockPath)}
			env.Refused = res.Refusals
			env.ExitBits = bits | BitFatal
			res.Envelope = env
			return res, nil
		}
		if detached {
			env.Warnings = append(env.Warnings,
				"HEAD detached — anchor describes this commit, re-attest on the intended branch")
		}
	}

	for _, doc := range targets {
		entry, err := e.attest(doc, req.At)
		if err != nil {
			refusal := Refused{Doc: doc.ID, Reason: err.reason}
			refusal.Offending = err.offending
			refusal.Hints = append(refusal.Hints, err.hint)
			if len(err.warns) > 0 {
				env.Warnings = append(env.Warnings, err.warns...)
			}
			res.Refusals = append(res.Refusals, refusal)
			bits |= err.bit
			continue
		}
		e.Lock.Docs[doc.ID] = entry
		changed = true
		env.Documents = append(env.Documents, Document{
			Doc:        doc.ID,
			Target:     entry.Target,
			ObservedAt: entry.ObservedAt,
			Anchor:     entry.Anchor,
		})
		// Hash-mode bloat advisory: a repository-wide watch enumerates
		// per-file hashes; past the bound, suggest narrowing.
		prov := e.ProvFor(doc.Target)
		if !prov.GitMode() && len(doc.PathGlobs) > 0 {
			if snap, snapErr := prov.Snapshot(doc.PathGlobs); snapErr == nil && len(snap) > 10000 {
				env.Warnings = append(env.Warnings, fmt.Sprintf(
					"hash-mode scope for %s exceeds 10,000 files (%d) — consider narrowing the watch", doc.ID, len(snap)))
			}
		}
	}

	if changed {
		if err := lockfile.Save(e.LockPath, e.Lock, e.LockRaw); err != nil {
			// The CAS conflict aborts the whole run with no partial write.
			return nil, usageError("%s", err.Error())
		}
		// Refresh the CAS baseline: the next observe on this engine must
		// compare against the bytes just written, not the pre-save ones.
		e.LockRaw = lockfile.Serialize(e.Lock)
		e.LockSeen = true
	}
	env.Lock = LockReport{Changed: changed, Path: lockRelPath(e.LockPath)}
	if changed {
		// The lock is committed like go.sum — the write is not the end of
		// the attestation; the commit of the lock diff is.
		env.Hints = append(env.Hints,
			"commit the lock: git add .vanguard/vanguard.lock && git commit")
	}
	env.ExitBits = bits
	sortDocs(env.Documents)
	env.Refused = res.Refusals
	res.Envelope = env
	return res, nil
}

// prune drops lock entries whose id no longer exists in discovery.
func (e *Engine) prune() (*Result, error) {
	res := &Result{}
	env := &Envelope{Mode: ModeObserve, Schema: SchemaVersion, Documents: []Document{}}
	changed := false
	for id := range e.Lock.Docs {
		if docByID(e.Docs, id) == nil {
			delete(e.Lock.Docs, id)
			changed = true
			env.Pruned = append(env.Pruned, id)
		}
	}
	if changed {
		if err := lockfile.Save(e.LockPath, e.Lock, e.LockRaw); err != nil {
			return nil, usageError("%s", err.Error())
		}
		e.LockRaw = lockfile.Serialize(e.Lock)
		e.LockSeen = true
	}
	env.Lock = LockReport{Changed: changed, Path: lockRelPath(e.LockPath)}
	res.Envelope = env
	return res, nil
}

// refusal carries a per-document precondition failure.
type refusal struct {
	bit       int
	reason    string
	offending []string
	hint      string
	warns     []string // advisory warnings riding along (blobless clone, ...)
}

// attest enforces the preconditions, then records the baseline. The split
// is the 0.4.0 contract: questions about the document go to the store
// provider, questions about code go to the document's target provider.
func (e *Engine) attest(doc *docmodel.Doc, at time.Time) (lockfile.Entry, *refusal) {
	now := e.Now
	if at.IsZero() {
		at = now
	} else {
		at = at.UTC()
	}
	prov := e.ProvFor(doc.Target)

	// Structure gate: a document that violates the schema it declared is
	// not attestable — observing garbage would launder it into FRESH.
	if vs := e.StructureViolations[doc.ID]; len(vs) > 0 {
		rules := make([]string, 0, len(vs))
		for _, v := range vs {
			rules = append(rules, fmt.Sprintf("%s (line %d)", v.Rule, v.Line))
		}
		return lockfile.Entry{}, &refusal{
			bit:       BitFatal,
			reason:    "structure violations",
			offending: rules,
			hint:      "fix the document to satisfy its declared schema, or remove the schema: field",
		}
	}

	// Future-time guard: an attestation more than the skew tolerance in the
	// future makes review_after un-fireable forever.
	if at.Sub(now) > e.Cfg.ClockSkewTolerance {
		return lockfile.Entry{}, &refusal{
			bit:    BitFatal,
			reason: "future or earlier observed_at",
			hint:   "re-run observe --at with the correct clock",
		}
	}

	// Monotonicity: an attestation cannot un-happen, regardless of source.
	if prev, ok := e.Lock.Docs[doc.ID]; ok {
		if prevTime, err := time.Parse(time.RFC3339, prev.ObservedAt); err == nil && at.Before(prevTime) {
			return lockfile.Entry{}, &refusal{
				bit:    BitFatal,
				reason: "future or earlier observed_at",
				hint:   "pass a later --at, or resolve the clock source",
			}
		}
	}

	entry := lockfile.Entry{
		ObservedAt:  at.Format(time.RFC3339),
		ContentHash: e.ContentHash(doc),
		Target:      doc.Target,
	}

	if prov.GitMode() {
		head, err := prov.HEAD()
		if err != nil || head == "" {
			return lockfile.Entry{}, &refusal{
				bit:    BitFatal,
				reason: "document not committed",
				hint:   "make the first commit, then observe",
			}
		}
		// Scope-clean precondition: never anchor against uncommitted churn.
		if len(doc.PathGlobs) > 0 {
			dirty, warnings, err := prov.Dirty(doc.PathGlobs)
			if err != nil {
				return lockfile.Entry{}, &refusal{bit: BitFatal, reason: "IO failure",
					hint: "the worktree state could not be read"}
			}
			if len(dirty) > 0 {
				return lockfile.Entry{}, &refusal{
					bit:       BitDirty,
					reason:    "watch scope dirty",
					offending: dirty,
					hint:      "commit the churn, then re-run observe",
					warns:     warnings,
				}
			}
		}
		// The document itself must be committed in the repository that
		// contains the store — the store provider's question, never the
		// target's (the v1 decoupled-refusal bug asked the wrong repo).
		// No store repository → skipped, as in 0.3.0.
		if e.StoreProv != nil {
			if status, err := e.StoreProv.FileStatus(storeRelDoc(filepath.Dir(e.StoreDir), e.StoreDir, doc.RelPath)); err == nil && status != "clean" {
				return lockfile.Entry{}, &refusal{
					bit:    BitFatal,
					reason: "document not committed",
					hint:   "commit the document in the repository that contains it, then re-run observe",
				}
			}
		}
		if len(doc.PathGlobs) > 0 {
			snap, snapErr := prov.Snapshot(doc.PathGlobs)
			if snapErr != nil {
				return lockfile.Entry{}, &refusal{bit: BitFatal, reason: "IO failure",
					hint: "the scope state could not be computed"}
			}
			if len(snap) == 0 {
				return lockfile.Entry{}, &refusal{
					bit:    BitFatal,
					reason: "config error",
					hint:   "the watch globs match zero files at the target — likely a typo; observe refuses an empty scope",
				}
			}
			scopeHash, hashErr := prov.ScopeHash(doc.PathGlobs)
			if hashErr != nil {
				return lockfile.Entry{}, &refusal{bit: BitFatal, reason: "IO failure",
					hint: "the scope state could not be computed"}
			}
			entry.Anchor = head
			entry.ScopeTreeHash = scopeHash
			entry.ScopeFileCount = len(snap)
		}
	} else {
		// HashProvider mode: no HEAD, no clean concept — the anchor is the
		// stored scope_snapshot.
		if len(doc.PathGlobs) > 0 {
			snap, err := prov.Snapshot(doc.PathGlobs)
			if err != nil {
				return lockfile.Entry{}, &refusal{bit: BitFatal, reason: "IO failure",
					hint: "the scope state could not be read"}
			}
			if len(snap) == 0 {
				return lockfile.Entry{}, &refusal{
					bit:    BitFatal,
					reason: "config error",
					hint:   "the watch globs match zero files at the target — likely a typo; observe refuses an empty scope",
				}
			}
			entry.ScopeSnapshot = snap
		}
	}
	return entry, nil
}

// emptyScopeHash is retired: the zero-match refusal now keys off the
// snapshot file count, which works in both provider modes.

func lockRelPath(path string) string {
	return ".vanguard/vanguard.lock"
}

// storeRelDoc renders the document's path relative to the given repository
// root — the store provider's root when checking the committed-document
// precondition.
func storeRelDoc(repoRoot, storeDir, rel string) string {
	// The document's git path inside the repository that contains the
	// store: the store's docs dir expressed relative to that repo root.
	// Root-level store → ".vanguard/docs/<rel>"; a store nested deeper
	// keeps its full prefix.
	p, err := filepath.Rel(repoRoot, filepath.Join(storeDir, "docs", rel))
	if err != nil {
		return ".vanguard/docs/" + rel
	}
	return filepath.ToSlash(p)
}
