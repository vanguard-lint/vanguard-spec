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
detection. Worktree status is authoritative from the real git CLI — go-git's
own status reports phantom churn under `core.autocrlf` and is only a
documented fallback.

## Interface

- `Provider` — GitMode, HEAD, ScopeHash, Snapshot, Dirty, AnchorReachable,
  RecoveredAnchor, Attribution, ChangedPathsBetween, FileStatus
  [provider.go:Provider].
- `OpenGit(root, storeDir)` — roots exactly at root, no upward detection; a
  parent's unrelated repository must never serve as the target
  [provider.go:OpenGit].
- `ScopeHash(globs)` — per-glob by strategy (tree OID / enumerated /
  repository-wide), combined via `scope.Combine` [provider.go:GitProvider.ScopeHash].
- `Dirty(globs)` — uncommitted churn inside the globs, sorted, store paths
  excluded, computed fresh on every call [provider.go:GitProvider.Dirty].
- `AnchorReachable(anchor)` — ancestor-of-HEAD walk with a visited set
  [provider.go:AnchorReachable].
- `RecoveredAnchor(globs, want, maxWalk)` — first-parent walk bounded by
  maxWalk, first commit whose scope hash equals want [provider.go:RecoveredAnchor].
- `Attribution(from, to, globs)` — changed paths plus introducing commits;
  merges diff against first parent only; `from` is baseline, never churn
  [provider.go:GitProvider.Attribution].
- `OpenHash(root, storeDir)` — hash-mode provider; `Dirty` is always empty,
  `Attribution` is an explicit error [provider.go:OpenHash].
- `GitProvider` / `HashProvider` — the two `Provider` implementations over a
  git object database and a plain directory respectively
  [provider.go:GitProvider, provider.go:HashProvider].

## Invariants

- Worktree status comes from `git status --porcelain -z
  --untracked-files=all` [provider.go:porcelainStatus]; go-git status is the
  fallback only [provider.go:worktreeStatus].
- The store directory is filtered out of every hash, walk, dirty, and
  attribution computation [provider.go:walkTreeInto].
- Unborn repositories report HEAD as `""`, not an error [provider.go:HEAD].
- A missing subtree falls back to the deterministic empty-tree OID
  [provider.go:subtreeOID].
- Unreadable commits (shallow history) read as not-reachable, not errors
  [provider.go:AnchorReachable].
- Unresolvable refs in range mode are explicit usage errors
  [provider.go:resolveRef].

## Side effects

Executes the external git binary for status (read-only, no files written)
[provider.go:porcelainStatus]; reads files from disk in hash mode
[provider.go:HashProvider.Snapshot].

## Verification

HEAD resolution including unborn, per-strategy scope hashing (subtree vs
enumerated) with cross-scope difference and determinism, hash reaction to
change, dirty detection, reachability, attribution, and
hash-mode snapshot behavior [provider_test.go].
