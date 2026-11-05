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
  [structure](engine/structure.md), [check](engine/check-engine.md), and the
  [command surface](engine/command-surface.md).
- `.vanguard/schemas/` holds the binding contracts: `document.schema.json`
  (the engine fields of every store document), `package-contract.schema.json`
  (structure of the engine/ contracts), `envelope-v1.schema.json` (the output
  envelope), `criteria-tree.schema.json` (the quality criteria tree),
  `quality-result.schema.json` (scorecard entries), and
  `design-canon.schema.json` (the design mirror).
- The quality artifacts live under `quality/`: the criteria tree and the
  scorecard; the quality plan itself lives beside this document
  ([quality-plan](quality-plan.md)); the rollout plan is
  [rollout-plan](rollout-plan.md).
- `internal/hashnorm` — the normalization pipeline: binary detection on the
  first 8 KiB, BOM strip, CR fold, trailing-LF strip, streaming, no Unicode
  canonization. All eight mandatory vectors have tests.
- `internal/pathmatch` — the closed glob syntax: one-level brace expansion,
  normalization, classification into subtree, enumerated, and repository-wide
  strategies, the segment matcher, and the check --path decision procedure
  with all eleven mandatory vectors.
- `internal/scope` - the scope-hash formulas: the sorted entry-list hash, the
  multi-glob combination, and the store-exclusion filters.
- `internal/structure` - declared-schema enforcement: required sections,
  order, duplicates, citation-pattern bullets (one claim per bullet across
  its wrapped lines), format mismatch, and the declared max_depth heading
  bound (load-time validation cap 32); contracts load from the top level or
  one combinator branch, code fences are invisible to every rule, and the
  gate is wired fail-closed at load [engine.go].
- `internal/duration` — the review_after grammar (h, d, w, mo, y; a bare
  integer means days) plus Go-duration tunables.
- `internal/config` — strict vanguard.yaml loading: unknown keys, anchors,
  aliases, and merge keys are fatal; discovery globs cannot escape docs;
  named targets (`targets:` map) declare additional watch roots with the
  shared id grammar.
- `internal/docmodel` — engine-field extraction for markdown, json, yaml, and
  the dsl leading comment block; lexicographic discovery with fail-closed
  parse and id rules; the optional `target:` key binds a document to one
  named target.
- `internal/lockfile` — canonical lockfile serialization (sorted ids, fixed
  field order), forward-compatible unknown fields, compare-and-swap writes;
  entries carry an optional `target` field (omitted = primary, zero-migration
  both directions).
- `internal/provider` — the git provider over go-git (tree OIDs, reachability,
  bounded recovery, chronological range attribution that stops at the
  anchor's reachable set) and the hash provider over plain directories;
  dirty detection spawns no process — go-git status supplies stat-mismatch
  candidates only, each confirmed by comparing worktree bytes against the
  HEAD blob over the hashnorm pipeline, so `core.autocrlf` renormalization
  cannot report phantom churn.
- `internal/check` — the two-phase check engine, the observe attestation
  engine, bindings, the text and pretty renderers, and the output contract.
  Since 0.4.0 the engine splits providers by role: a store provider (the
  repository containing `.vanguard`) answers the document-side observe
  preconditions, and one target provider per named target answers every
  staleness question — a store may live outside the repositories it watches
  and attest N targets, each with or without git.

## Guarantees already guarded by tests

- The net-state rule decides staleness from the scope hash at HEAD; history
  is attribution only.
- Observe is the only lock writer, with per-document refusals, monotonic
  observed_at, the skew guard, and an atomic compare-and-swap write.
- Exit bits compose by OR: 1 fatal, 2 stale, 4 dirty, 8 gaps under strict,
  16 binding failed, 32 affected in range.

## Not yet implemented

Provisioning (install), goldmark-AST structure rules, JSON Schema 2020-12
enforcement for json/yaml documents, and the full anchor-recovery surface
are later milestones of the plan; the x-vanguard markdown structure rules
(required sections, order, duplicates, citation patterns) are implemented
today and enforced fail-closed at load. This document updates as they land.
