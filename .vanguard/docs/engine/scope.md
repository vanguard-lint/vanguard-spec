---
id: scope
watches:
  paths:
    - "vanguard/internal/scope/**/*.go"
  review_after: 30d
---

# scope — Scope-Hash Formulas

The byte-exact formulas that turn a set of watched files into the
`scope_tree_hash` stored in the lockfile: per-entry hashing, per-glob hashes
by strategy, the multi-glob combination, and store exclusion.

Formulas (package doc): `scope_tree_hash = sha256(concat over globs sorted by
glob bytes: glob || 0x00 || per-glob-hash || 0x0A)`; the per-glob hash is a
kind-prefixed git tree OID (subtree) or the entry-list hash
`sha256(concat over entries sorted by path bytes: path || 0x00 || mode ||
0x00 || content-identity || 0x0A)`.

## Interface

- `Entry{Path, Mode, Hash}` — one file's identity; `Hash` is the git blob SHA
  in git mode or the normalized `"sha256:<hex>"` in hash mode
  [scope.go:Entry].
- `HashEntries(entries) string` — the entry-list formula; caller order never
  matters [scope.go:HashEntries].
- `HashEnumerated(glob, entries, storeDir)` — matched entries after store
  exclusion [scope.go:HashEnumerated].
- `HashRepositoryWide(rootEntries, storeDir)` — synthetic level-1 list, store
  excluded by name [scope.go:HashRepositoryWide].
- `Combine(pairs) string` — per-glob results into the document scope hash,
  sorted by canonical glob bytes [scope.go:Combine].
- `FilterStore(entries, storeDir)` — prefix-based exclusion of the store
  directory [scope.go:FilterStore].
- `SameKind(a, b) bool` — kind-prefix comparability; a mismatch means globs
  changed (stale, re-observe hint), never an internal error [scope.go:SameKind].

## Invariants

- Both formulas are byte-exact and sort inputs first — caller order never
  matters [scope.go:HashEntries, scope.go:Combine].
- Outputs always carry the `"sha256:"` kind prefix [scope.go:HashEntries].
- Paths, modes, and glob text all participate: rename, mode change, or glob
  respelling each re-key the hash [scope_test.go:TestHashEntriesPathIsPartOfTheHash].

## Side effects

None — pure functions.

## Verification

Determinism under reversed input, rename/mode/swap re-keying, store
exclusion at enumeration and repository-wide level, combination order
independence, and kind comparability [scope_test.go].
