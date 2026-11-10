---
id: rollout-plan
schema: none
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

### W2 — Quality plan execution (from `.vanguard/docs/quality-plan.md`)

- The criteria tree lives at `.vanguard/docs/quality/criteria-tree.yaml`,
  validated by `.vanguard/schemas/criteria-tree.schema.json`; run the
  Decomposer / Attacker / Referee round for L1 to freeze L0 and L1.
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
by the release workflow, and untagged builds report `dev`. The pre-release
line has started: `v0.3.0-rc.1` shipped the sha256v2 lock scheme and the
release pipeline; `v0.3.0-rc.2` closed the repo-side structure-gate findings
and was verified black-box by the audit attack suite (delta 100/100). The
stable `0.3.0` tag follows the rc line: the feature set is frozen, no
consumers remain to gather migration feedback from, and the goldmark-AST
structure rules move to the next minor.

### W5 — Decoupled stores and multiple targets (0.4.0, new)

The canon section "Decoupled stores and multiple targets (0.4.0)" is
ratified; the design draft lives at `.vanguard/docs/design/design-040-draft.md`
(an unwatched design record). Implementation order: config `targets:` +
validation fatals, the store/target provider seam in the engine, the observe
precondition split, the optional lock entry field and envelope
`Document.target`, then two-repo integration fixtures reproducing the v1
decoupled-refusal case. Zero-migration rule: in-repo stores without
`targets:` behave byte-identically to 0.3.0.

Status: implemented and shipped as `v0.4.0-rc.1` (2026-11-06). The full
0.3.0 suite passes unchanged with eight new tests covering the two-repo
decoupled lifecycle, git and hash targets side by side, primary-target lock
byte-identity, and the fatal vectors. Verified end to end on a real
three-repo setup: one documentation store attesting three service
repositories, with a cross-repo commit detected as stale naming the exact
changed file and introducing commit. The stable `0.4.0` follows the rc line
pending a black-box audit and field feedback.

### W6 — Within-run memoization (0.4.0, performance)

The canon section "Memoization within a run (0.4.0)" is ratified; the git
provider captures HEAD, the head tree, the go-git worktree scan, and
attribution baselines once per target per run instead of once per
document. Motivation, measured: a full check on a real store paid a full
worktree scan per document — 507.5s on 490 documents, 500.4s on a frozen
689-document snapshot with three git targets. After memoization the same
snapshot checks in 20.1s (~25x), and the check envelopes are byte-identical
between the unmemoized and memoized binaries. No schema, lock, exit-bit,
or dependency change; snapshot semantics are documented in the provider
contract and the engine overview.

### W7 — Per-answer history caches (0.4.1, performance)

Field feedback on the 0.4.0 memoization: a real 811-document store with
four git targets still paid ~10 minutes per full check. Instrumented phasing
attributed 74% of the run to `Attribution` (re-diffing the same commits
once per document — documents anchoring ~54 commits deep on a large tree
paid ~0.5s each) and 16% to `AnchorReachable` (re-walking the same history
per document); the 0.4.0 snapshot memoization never covered these two
history questions, and the release benchmark missed them because its
documents shared one anchor. Fix: reachability is cached per anchor (pure
function of anchor + frozen HEAD) and each commit's changed-path set is
diffed at most once per run, store-filtered, before glob matching (pure
function of the commit pair); globs stay per document. Measured on the
same store: 589.8s → 21.1s (~28x) with byte-identical staleness answers
(37 stale documents, same ids). Canon extended first
("History questions are cached per answer, not per document"), then the
code; `TestHistoryCachesReturnIdenticalAnswers` guards repeat-ask
identicality, pre-glob isolation, and suffix walks.

## Order

W1 ratification and W2's criteria tree can start immediately and gate
everything else. W3 continues in parallel per milestone; W5 is the 0.4.0
headline feature and starts from the ratified canon section. W4's pipeline is
proven and the stable `0.3.0` tag lands on the audited rc.2 state.
