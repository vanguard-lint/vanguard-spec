package check

import (
	"fmt"
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

	for _, doc := range targets {
		entry, err := e.attest(doc, req.At)
		if err != nil {
			refusal := Refused{Doc: doc.ID, Reason: err.reason}
			refusal.Offending = err.offending
			refusal.Hints = append(refusal.Hints, err.hint)
			res.Refusals = append(res.Refusals, refusal)
			bits |= err.bit
			continue
		}
		e.Lock.Docs[doc.ID] = entry
		changed = true
		env.Documents = append(env.Documents, Document{
			Doc:        doc.ID,
			ObservedAt: entry.ObservedAt,
			Anchor:     entry.Anchor,
		})
	}

	if changed {
		if err := lockfile.Save(e.LockPath, e.Lock, e.LockRaw); err != nil {
			// The CAS conflict aborts the whole run with no partial write.
			return nil, usageError("%s", err.Error())
		}
	}
	env.Lock = LockReport{Changed: changed, Path: lockRelPath(e.LockPath)}
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
}

// attest enforces the preconditions, then records the baseline.
func (e *Engine) attest(doc *docmodel.Doc, at time.Time) (lockfile.Entry, *refusal) {
	now := e.Now
	if at.IsZero() {
		at = now
	} else {
		at = at.UTC()
	}

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
	}

	if e.Prov.GitMode() {
		head, err := e.Prov.HEAD()
		if err != nil || head == "" {
			return lockfile.Entry{}, &refusal{
				bit:    BitFatal,
				reason: "document not committed",
				hint:   "make the first commit, then observe",
			}
		}
		// Scope-clean precondition: never anchor against uncommitted churn.
		if len(doc.PathGlobs) > 0 {
			dirty, err := e.Prov.Dirty(doc.PathGlobs)
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
				}
			}
		}
		// The document itself must be committed in the repository that
		// contains it. The store is not a git repository → skipped.
		if status, err := e.Prov.FileStatus(storeRelDoc(e.StoreDir, doc.RelPath)); err == nil && status != "clean" {
			return lockfile.Entry{}, &refusal{
				bit:    BitFatal,
				reason: "document not committed",
				hint:   "commit the document in the repository that contains it, then re-run observe",
			}
		}
		if len(doc.PathGlobs) > 0 {
			snap, snapErr := e.Prov.Snapshot(doc.PathGlobs)
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
			scopeHash, hashErr := e.Prov.ScopeHash(doc.PathGlobs)
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
			snap, err := e.Prov.Snapshot(doc.PathGlobs)
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

// storeRelDoc renders the document's path relative to the store's
// containing repository root.
func storeRelDoc(storeDir, rel string) string {
	return ".vanguard/docs/" + rel
}
