---
id: vanguard-engine
schema: none
watches:
  paths:
    - "vanguard/**/*.go"
---

# The vanguard Engine

The Go module under `vanguard/` implements the design in `docs/designs/vanguard.md`
as a single binary. This document is the store-level map of what is implemented;
the design canon stays authoritative.

## Layout

- `cmd/vanguard` — the CLI surface: init, check, observe, wired with cobra.
  Package contracts live in `engine/` beside this document — one per
  subsystem, each bound by `schema: package-contract` in its frontmatter:
  [hashnorm](engine/hashnorm.md), [pathmatch](engine/pathmatch.md),
  [scope](engine/scope.md), [duration](engine/duration.md),
  [config](engine/config.md), [docmodel](engine/docmodel.md),
  [lockfile](engine/lockfile.md), [provider](engine/provider.md),
  [check](engine/check-engine.md), and the [command
  surface](engine/command-surface.md).
- `.vanguard/schemas/` holds the binding contracts: `document.schema.json`
  (the engine fields of every store document), `package-contract.schema.json`
  (structure of the engine/ contracts), `envelope-v1.schema.json` (the output
  envelope), and `criteria-tree.schema.json` (the quality criteria tree).
- The quality artifacts live under `quality/`: the criteria tree plus the
  quality plan itself ([quality-plan](quality-plan.md)); the rollout plan is
  [rollout-plan](rollout-plan.md).
- `internal/hashnorm` — the normalization pipeline: binary detection on the
  first 8 KiB, BOM strip, CR fold, trailing-LF strip, streaming, no Unicode
  canonization. All eight mandatory vectors have tests.
- `internal/pathmatch` — the closed glob syntax: one-level brace expansion,
  normalization, classification into subtree, enumerated, and repository-wide
  strategies, the segment matcher, and the check --path decision procedure
  with all eleven mandatory vectors.
- `internal/scope` — the scope-hash formulas: the sorted entry-list hash, the
  multi-glob combination, and the store-exclusion filters.
- `internal/duration` — the review_after grammar (h, d, w, mo, y; a bare
  integer means days) plus Go-duration tunables.
- `internal/config` — strict vanguard.yaml loading: unknown keys, anchors,
  aliases, and merge keys are fatal; discovery globs cannot escape docs.
- `internal/docmodel` — engine-field extraction for markdown, json, yaml, and
  the dsl leading comment block; lexicographic discovery with fail-closed
  parse and id rules.
- `internal/lockfile` — canonical lockfile serialization (sorted ids, fixed
  field order), forward-compatible unknown fields, compare-and-swap writes.
- `internal/provider` — the git provider over go-git (tree OIDs, reachability,
  bounded recovery, range attribution) and the hash provider over plain
  directories; worktree status comes from the real git CLI because go-git's
  own status reports phantom churn under core.autocrlf.
- `internal/check` — the two-phase check engine, the observe attestation
  engine, bindings, and the output contract.

## Guarantees already guarded by tests

- The net-state rule decides staleness from the scope hash at HEAD; history
  is attribution only.
- Observe is the only lock writer, with per-document refusals, monotonic
  observed_at, the skew guard, and an atomic compare-and-swap write.
- Exit bits compose by OR: 1 fatal, 2 stale, 4 dirty, 8 gaps under strict,
  16 binding failed, 32 affected in range.

## Not yet implemented

Provisioning (install), the markdown structure rule vocabulary, the json
schema enforcement layer, and the full anchor-recovery surface are later
milestones of the plan; this document updates as they land.
