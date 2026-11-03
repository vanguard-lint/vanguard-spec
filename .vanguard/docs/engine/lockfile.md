---
id: lockfile
schema: package-contract
watches:
  paths:
    - "vanguard/internal/lockfile/**/*.go"
  review_after: 30d
---

# lockfile â€” Canonical Attestation Record

Reads and writes `vanguard.lock`, the git-committed attestation record.
Defines a canonical serialization (ids sorted lexicographically, fixed field
order, 2-space indent, LF endings, trailing newline, no comments), preserves
unknown forward-compatibility fields across read-modify-write, and performs
compare-and-swap atomic writes so concurrent observes cannot trample each
other.

## Interface

- `Version = 1` â€” lock schema version; an unknown major fails closed
  [lockfile.go:Version].
- `Entry{Anchor, ObservedAt, ScopeTreeHash, ScopeFileCount, ContentHash,
  ScopeSnapshot, unknown}` â€” anchor present = git mode, scope_snapshot
  present = hash mode, review_after-only entries store observed_at +
  content_hash only [lockfile.go:Entry].
- `Load(path) (*Lock, []byte, error)` â€” parse plus the exact on-disk bytes
  for the compare-and-swap [lockfile.go:Load].
- `Lock{Version, Docs}` â€” the parsed lock: schema version and the id â†’
  `Entry` map [lockfile.go:Lock].
- `Parse(raw) (*Lock, error)` â€” decode lock bytes; unknown version fails
  closed [lockfile.go:Parse].
- `Serialize(lock) []byte` â€” the canonical, byte-deterministic form
  [lockfile.go:Serialize].
- `Save(path, lock, captured) error` â€” CAS write: captured nil requires the
  file absent; otherwise on-disk bytes must equal captured; writers serialize
  through an exclusive `.cas` token (stolen after 10s, so a crashed writer
  never wedges the store); temp file + rename [lockfile.go:Save,
  lockfile.go:acquireCAS].

## Invariants

- Canonical serialization is byte-deterministic: sorted ids, fixed field
  order, sorted scope_snapshot and unknown keys, 2-space indent, LF, trailing
  newline [lockfile.go:Serialize, lockfile.go:entryJSON].
- Zero-valued known fields are omitted; observed_at and content_hash always
  emit [lockfile.go:entryJSON].
- Unknown fields ride along sorted after the known ones and survive a
  parse-serialize round trip [lockfile.go:parseEntry].
- Empty docs serialize to the exact init scaffold bytes [lockfile.go:Serialize].
- The check-then-write window is mutual exclusion, not hope: a concurrent
  loser must observe the winner canonical bytes unchanged (0 lost updates)
  [loser_test.go:TestCASLoserLeavesWinnerBytes].
- A CAS conflict aborts with "lock changed during observe â€” re-run"; writes
  are atomic via temp file + rename [lockfile.go:Save, lockfile.go:atomicWrite].

## Side effects

Reads and writes the lock file; atomic writes through os.WriteFile + os.Rename
[lockfile.go:Load, lockfile.go:atomicWrite].

## Verification

Canonical form byte-stability, round trips, empty-lock scaffold bytes,
unknown-field preservation, corrupt-lock failures, CAS pass/abort with no
temp leftovers, and the snapshot shape [lockfile_test.go].
