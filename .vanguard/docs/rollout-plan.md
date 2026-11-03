---
id: rollout-plan
watches:
  paths:
    - "docs/designs/**"
  review_after: 14d
---

# Rollout Plan

Where the engine stands and what ships next. The design canon under
`docs/designs/` stays authoritative; when it moves, this plan is re-observed.

## Current state (2026-11-03)

The Go module under `vanguard/` implements the check/observe core end to end,
with the mandatory vectors, a strict config and frontmatter loader, a
canonical compare-and-swap lockfile, both providers, the envelope contract,
and the CLI surface. The engine checks this very store — the dogfood loop is
closed. Build, test, and lint run in CI; a goldfish drift review is archived
under `docs/reviews/`.

## Workstreams

### W1 — Lock integrity (new, from `docs/designs/lock-integrity-section.en.md`)

Ratify the proposed section into the design canon, then implement:

- Tier A in every check: `lock_noncanonical`, `lock_uncommitted`,
  `lock_entry_shape`, `lock_orphan_entries` as advisory violations, escalated
  to bit 8 under strict.
- The `vanguard verify` command: Tier B cross-checks of every git-mode entry
  against the anchor commit, Tier C diff against a base ref, the dedicated
  exit bit 64, and the `lock_integrity` envelope field with a `skipped[]`
  list that is never silent.
- `check --verify-lock` reusing the same engine path.

### W2 — Quality plan execution (from `docs/designs/vanguard-quality-plan.en.md`)

- Export `docs/quality/criteria-tree.yaml` with the node schema from section
  6; run the Decomposer / Attacker / Referee round for L1 to freeze L0 and
  L1.
- Turn each L4 leaf into a named test (`Q-1.1.a-1`, ...) wired into CI per
  the section 8 gate table. The oracle for git facts is the real git CLI,
  never go-git itself.
- Close the spec gaps G1-G4 the plan records, including property or fuzz
  tests for the frontmatter, glob, and lockfile parsers.

### W3 — Drift backlog (from `docs/reviews/2026-11-03-DRIFT-REPORT.md`)

The cheap blockers are closed. Still open, in milestone order:

- The structure-verification layer: markdown AST rules, JSON Schema
  enforcement of the `schema` field, resource caps (M2).
- Provisioning and the install surface (M5).
- Binding contract hardening: checksum verification of config-provided
  commands, process-tree kill on timeout, the output parser field.
- Presentation contracts: the yaml quoting rule and the text renderer
  details.

### W4 — Release engineering

The versioning rule in `.vanguard/rules/versioning.md` is in force: version
numbers come only from `vX.Y.Z` tags, release binaries are built and attached
by the release workflow, and untagged builds report `dev`. First tagged
release lands once W3's structure layer is in.

## Order

W1 ratification and W2's criteria tree can start immediately and gate
everything else. W3 continues in parallel per milestone. W4 is done except
the first tag, which waits for the structure layer.
