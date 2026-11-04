---
id: provider
schema: package-contract
watches:
  paths:
    - "vanguard/internal/provider/**/*.go"
  review_after: 30d
---

# provider — Target Resolution: Git and Hash

Answers scope-state and worktree questions for one target. `GitProvider`
(over go-git, embedded object database) handles the commit-graph and
tree-metadata hot path; `HashProvider` serves a plain directory via
normalized content hashing. Blob contents are touched only for dirty
detection. Dirty detection is two-phase and spawns no process: go-git's
worktree status supplies candidates only, each confirmed by normalized
byte-compare against the HEAD blob — the git CLI is never consulted.

## Interface

- `Provider` - GitMode, HEAD, ScopeHash, ScopeHashAt, Snapshot, Dirty,
  AnchorReachable, RecoveredAnchor, Attribution, ChangedPathsBetween,
  FileStatus, HeadState [provider.go:Provider].
- `OpenGit(root, storeDir)` — roots exactly at root, no upward detection; a
  parent's unrelated repository must never serve as the target
  [provider.go:OpenGit].
- `ScopeHash(globs)` — per-glob by strategy (tree OID / enumerated /
  repository-wide), combined via `scope.Combine` [provider.go:GitProvider.ScopeHash].
- `Dirty(globs)` — uncommitted churn inside the globs plus advisory
  warnings, sorted, store paths excluded, computed fresh on every call;
  two-phase: go-git status supplies the candidate set, each candidate is
  confirmed by comparing worktree bytes against the HEAD blob over the
  hashnorm pipeline, so `core.autocrlf` renormalization cannot report
  phantom churn [provider.go:GitProvider.Dirty].
- `HeadState()` — HEAD detachment and whether a merge or rebase is in
  progress, from the `.git` markers, no process spawn
  [provider.go:GitProvider.HeadState].
- `AnchorReachable(anchor)` — ancestor-of-HEAD walk with a visited set
  [provider.go:AnchorReachable].
- `RecoveredAnchor(globs, want, maxWalk)` — first-parent walk bounded by
  maxWalk, first commit whose scope hash equals want [provider.go:RecoveredAnchor].
- `Attribution(from, to, globs)` — changed paths plus introducing commits
  read chronologically (committer date, equal-position ties break by OID);
  merges diff against first parent only; the walk stops at the anchor's
  reachable set, so post-merge ranges stop at the branch point instead of
  collecting the baseline's whole history
  [provider.go:GitProvider.Attribution].
- `OpenHash(root, storeDir)` — hash-mode provider; `Dirty` is always empty,
  `Attribution` is an explicit error [provider.go:OpenHash].
- `GitProvider` / `HashProvider` — the two `Provider` implementations over a
  git object database and a plain directory respectively
  [provider.go:GitProvider, provider.go:HashProvider].

## Invariants

- The engine spawns no process: dirty detection is two-phase over the
  embedded object database — go-git status is the candidate generator only,
  and every candidate is confirmed by a normalized compare against the HEAD
  blob [provider.go:GitProvider.Dirty]; `internal/check` enforces the same
  rule with a containment test.
- Untracked files have no HEAD blob and are dirty outright; a blobless
  clone that lacks the blob is the loud case — the file reports dirty and a
  warning carries the hint, never silence
  [provider.go:worktreeMatchesHead].
- The store directory is filtered out of the enumerated and repository-wide
  hash strategies, the walk, dirty detection, and attribution
  [provider.go:walkTreeInto]; the Subtree strategy is the exception — it
  hashes the target directory's own tree OID with no store exclusion
  [provider.go:subtreeOID].
- Unborn repositories report HEAD as `""`, not an error [provider.go:HEAD].
- A missing subtree falls back to the deterministic empty-tree OID
  [provider.go:subtreeOID].
- Unreadable commits (shallow history) read as not-reachable, not errors
  [provider.go:AnchorReachable].
- Unresolvable refs in range mode are explicit usage errors
  [provider.go:resolveRef].

## Side effects

Reads the repository's object database and `.git` markers (read-only, no
files written, no processes spawned); reads files from disk in hash mode
[provider.go:HashProvider.Snapshot].

## Verification

HEAD resolution including unborn, per-strategy scope hashing (subtree vs
enumerated) with cross-scope difference and determinism, hash reaction to
change, dirty detection, reachability, attribution, and
hash-mode snapshot behavior [provider_test.go].
