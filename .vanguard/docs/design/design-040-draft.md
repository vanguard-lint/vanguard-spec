---
id: design-040-draft
schema: none
---

# vanguard v0.4.0 — Decoupled Stores and Multiple Targets

*   **Author:** vanguard-spec working session
*   **Status:** Draft
*   **Date:** 2026-11-05

--------------------------------------------------------------------------------

## Overview

A vanguard store today can attest exactly one repository, and observe only
works when that repository contains both the store and the watched code. This
design lets one store live anywhere — outside every repo it watches — and
attest N named targets, each with or without a `.git`. The engine's single
git provider splits into two roles: a store provider (proves the documents
are committed) and a target provider (computes staleness), and every document
names the target it watches.

This draft records the design for the 0.4.0-rc.1 milestone. It is a design
record only: it binds to no source tree and carries no watch scope.

## The Problem

An experiment on the 0.3.0 engine confirmed two walls, one empirical and one
structural.

**Wall 1 — decoupled observe is refused, forever.** A store placed outside
its watched repo reads fine: `check` resolves `target.path` correctly, and
range attribution runs against the target's object database. But `observe` is
refused with "document not committed" no matter what. The committed-document
precondition evaluates document paths against the **target** repository —
where the store's documents do not exist — so an anchor can never be written,
and staleness can never be established. The design canon already records this
exact gap: "decoupled observe needs its own provider seam — not started". The
read half of decoupled stores shipped; the write half did not.

**Wall 2 — one store, one target.** The config carries a single
`target.path`; every document's watch globs resolve against that one root.
Watching N repositories requires N stores today, each nested in its repo.
That does not fit the central-docs workflow: one team keeping the specs for
many services in one place, attesting each service from that one store.

The proposed solution: split the provider into a store-side and a
target-side role; let the config declare named targets; let each document
bind to one target; record that binding in the lock entry and the envelope.
Stores that use none of this — every 0.3.0 store — behave exactly as before.

## Goals

*   **G1 — decoupled lifecycle.** A store outside the watched repo completes
    the full cycle: observe writes an anchor against the target's history, a
    later target commit turns the document stale, and the stale verdict names
    the changed paths and introducing commits.
*   **G2 — one store, N targets.** The config declares named targets; each
    document binds to exactly one of them; a store may hold documents across
    any mix of targets.
*   **G3 — honest degradation per target.** A target with `.git` gets the
    full axis set (paths staleness, attribution, dirty). A target without
    `.git` gets net-state staleness by re-enumeration — and never pretends to
    have attribution or a dirty/stale distinction.
*   **G4 — zero migration for 0.3.0 stores.** In-repo stores without
    `targets:` behave identically. The old binary reads locks the new binary
    writes (and vice versa): the new lock field is optional and unknown-field
    tolerant on both sides.
*   **G5 — one lock-schema pass.** W1 `verify` (lock integrity, Tier A/B/C,
    exit bit 64) rides the same lock-schema change instead of forcing a
    second migration later.

## Non-Goals

*   **Real-time watching.** Vanguard is a check-time tool. Staleness is
    discovered when someone runs `check`, not pushed by a daemon.
*   **One document, many targets.** A document binds to exactly one target;
    overlapping coverage is expressed by separate documents.
*   **Git-less stores.** The store side requires a repository: attestation
    must anchor the documents' history somewhere. Only targets are free to
    be plain directories.
*   **Network targets.** Targets are local filesystem paths. Remote URLs
    (clone-on-check) are out of scope.
*   **Envelope schema bump.** `Document.target` is optional and additive —
    the envelope schema stays at version 1.

## Open Questions

*   **Q1 — target name grammar.** Proposed: the document-id grammar
    `[a-z0-9][a-z0-9._-]*` (max 128, ASCII), so one validation rule covers
    both. Any reason to differentiate?
*   **Q2 — `check --path` across targets.** When a path is given, does the
    reverse lookup run against every target's root, or only against the
    targets of the store's documents? (Proposed: only targets actually
    referenced by at least one document — a target nobody watches has no
    document to report.)
*   **Q3 — target identity in the hash.** The lock entry gains a `target`
    name field; the scope hash itself stays target-blind. Is binding identity
    by field enough, or must the hash mix the target name in (forgery across
    two targets reusing one entry)? Proposed: field is enough — a swapped
    name fails the entry-shape check the same way a swapped scheme prefix
    does today.

## Technical Plan (Architecture)

### Component map — what changes, what does not

| Component | Change | Shape |
|---|---|---|
| `internal/config` | Modify | `targets:` map (name → path) beside the legacy `target:`; validation rules |
| `internal/check` (engine) | Modify | two provider roles; per-target provider table |
| `internal/provider` | **Unchanged** | `OpenGit`/`OpenHash` reused as-is, instantiated per role |
| `internal/docmodel` | Modify | optional `target:` frontmatter key on documents |
| `internal/lockfile` | Modify | optional `target` field per entry; canonical serialization |
| `internal/check` (envelope) | Modify | optional `Document.Target`; text renderer annotation |
| `internal/scope` | **Unchanged** | hash formulas stay target-blind |
| `docs/designs/lock-integrity-section.en.md` | Reference | W1 `verify` rides this lock-schema pass (G5) |

### The two provider roles

Today one provider serves two masters. The split assigns each precondition to
the repository it actually belongs to:

*   **Store provider** — opened from the repository that contains the store
    (`git.PlainOpen` on the store's parent). Serves exactly two
    preconditions: the document-is-committed check and the document-clean
    check at observe. In an in-repo store this is the same repository as
    today; the seam is invisible.
*   **Target provider(s)** — one per target, opened against the target root
    (`OpenGit` when a `.git` exists, `OpenHash` otherwise). Serves every
    staleness computation: scope hash at HEAD, dirty detection,
    attribution/`introducing_commits`, merge/rebase markers, HEAD existence,
    and the `--path` reverse lookup.

```text
                     ┌──────────────────────────────┐
                     │ store: /work/specs/.vanguard │
                     │  docs/  schemas/  vanguard.lock
                     └──────────┬───────────────────┘
                                │ store provider
                                ▼ (documents committed? clean?)
                     ┌──────────────────────────────┐
                     │ store repo: /work/specs/.git │
                     └──────────────────────────────┘

  vanguard.yaml:  targets:
                    payments: ../payments
                    legacy-billing: /srv/billing      (no .git → hash mode)
                    website:   ../website

  doc frontmatter:  target: payments     (omitted → primary target)

       doc A (target: payments) ──► payments provider ──► /work/payments/.git
       doc B (target: legacy-billing) ──► hash provider ──► /srv/billing
       doc C (no target) ──► primary target provider ──► legacy target.path
```

### Config grammar

```yaml
target:                     # legacy, unchanged — resolves to the primary
  path: ../main-repo        # target when a doc names no target
targets:                    # new, optional map — zero, one, or many entries
  payments: ../payments
  billing: /srv/billing
```

Validation at load, all fatal: a target name matches the document-id grammar
(Q1); duplicate names between `targets:` and the primary resolution are
rejected; a declared target path must exist and be a directory. This last
rule is deliberately stricter than today — a declared target that points at
nothing is a config error, not a silent fall into hash mode over an empty
root. The in-repo default (no `target:`, no `targets:`) keeps today's
behavior untouched.

### Document binding

Frontmatter gains one optional engine key: `target: <name>`. Missing means
the primary target. The key is engine-owned (stripped before user-schema
validation, like `id` and `schema:` today). An unknown target name in a
frontmatter is fatal at discovery — a document bound to a target that does
not exist would otherwise attest against nothing.

### Lock entry

Entries gain an optional `target` string. Omitted means the primary target —
so every 0.3.0 lock reads and writes identically. Serialization appends the
field when present; the entry-shape validation accepts it; the sha256v2
scheme is untouched (Q3: identity binds by field, not by hash). A lock entry
whose name matches no configured target is a fatal at load — the lock cannot
outlive a target rename silently.

### Envelope

`Document.Target` (`target,omitempty`) rides alongside `changed_paths` and
`introducing_commits`. Absent for primary-target documents, so 0.3.0
envelopes are byte-identical for in-repo stores. The text renderer appends a
`@<name>` marker to the status line only when the document declares a
non-primary target — single-target stores render exactly as today.

### Observe precondition split (the fix for Wall 1)

| Precondition | Provider | Rationale |
|---|---|---|
| document committed | store | the doc lives in the store repo |
| document clean (no churn on the doc) | store | same file, same repo |
| HEAD exists (anchor basis) | target | the anchor is a target commit |
| no merge/rebase in progress | target | attesting over target instability |
| watch scope dirty-free | target | uncommitted target churn would launder |
| detached HEAD | target | warns, proceeds (unchanged) |

### What an observe records (two histories, one entry)

An observe on a decoupled store writes one lock entry that carries evidence
from both repositories, each answering the question it owns:

| Field | Answered by | Content |
|---|---|---|
| `content_hash` | store repo | hash of the attested document version |
| `observed_at` | store repo (clock) | when the attestation happened |
| `anchor` | target repo | the target's HEAD commit SHA at observe time |
| `scope_snapshot` | target repo | hash of the watch scope **as it exists at the anchor commit** in the target's ODB |

The flow: the store provider proves the document is committed and clean;
the target provider reads the target's HEAD and scope state; the entry lands
in `vanguard.lock`; the lock is committed in the store repo. Later, `check`
re-derives the scope hash **at the anchor inside the target's ODB** and
compares it to `scope_snapshot` — a difference is STALE, and
`introducing_commits` walks the target's history from anchor to HEAD to name
the culprit commits. The store repo never answers questions about code; the
target repo never answers questions about documents. v1's refusal happened
because one provider was asked both kinds of questions.

### Cross-cutting behaviors

*   **`check --path`** resolves the reverse lookup against every target that
    at least one document references (Q2). A target nobody watches has no
    document to report and costs nothing to skip.
*   **Range mode** (`--from/--to`) runs per document against its own target's
    history; `affected_in_range` aggregates across targets, each attribution
    already names its commits — with the document's target in the envelope,
    ambiguity needs no extra field.
*   **W1 `verify`** (G5) reuses the per-target provider table: Tier B
    cross-checks run each entry's anchor against its own target's ODB. The
    detailed Tier A/B/C semantics stay governed by
    `docs/designs/lock-integrity-section.en.md`; this design only commits to
    landing the lock-schema change they need in the same release.

### Non-functional notes

*   **Performance**: providers open once at load. N targets = N `PlainOpen`
    calls, no per-check overhead; hash targets enumerate only within their
    watch scopes, as today.
*   **Portability**: relative target paths resolve against the store's parent
    — moving a store to another machine requires the same sibling layout, the
    same constraint `target.path` already has. Absolute paths carry the
    machine with them; the config grammar allows both, unchanged.
*   **Rollback**: the feature is additive. Removing `targets:` from the
    config returns a store to 0.3.0 behavior; locks written with `target`
    fields still read (the field is ignored data for a single-target store).

## Alternatives Considered

### Alternative A: target path directly in frontmatter

*   **Pros**: no config indirection; each document self-contained.
*   **Cons**: every document leaks machine layout (`../payments`); a repo
    rename touches every document; duplicate declarations drift apart.
*   **Reason for Rejection**: named targets centralize the layout in one
    validated place; documents carry only a name, so moving a checkout is a
    one-line config edit.

### Alternative B: N stores plus an aggregation wrapper

*   **Pros**: zero engine change; today's proven per-repo behavior.
*   **Cons**: N attestation ledgers with no single `check`; no cross-repo
    range query; the central-docs workflow stays impossible — this is
    precisely Wall 2.
*   **Reason for Rejection**: the wrapper would re-implement check's
    envelope, exit bits, and refusal semantics, badly and outside the engine.

### Alternative C: remote targets (clone on check)

*   **Pros**: stores could watch repositories not present on disk.
*   **Cons**: network in the hot path violates the containment pillar (L1.3
    — check runs offline, deterministically); clone latency and
    non-determinism on every check.
*   **Reason for Rejection**: targets are local paths; remote fetching stays
    outside the engine's contract.

### Alternative D: mix the target name into the scope hash

*   **Pros**: identity baked into the hash; no extra field.
*   **Cons**: changes the sha256v2 scheme (a breaking lock migration) to
    solve what an entry field already solves.
*   **Reason for Rejection**: identity binds by the optional `target` field;
    a swapped name fails the entry-shape check the same way a swapped scheme
    prefix does today (Q3).

## Detailed Implementation Plan

### Target Files to Create/Modify

#### 1. `vanguard/internal/config/config.go` + `config_test.go` (Modify)

*   **Type / Scope**: Go library, config loading.
*   **Proposed Changes**: `Targets map[string]string` parsed from a new
    optional `targets:` block; validation — name grammar
    `[a-z0-9][a-z0-9._-]*` (max 128), duplicate names between `targets:` and
    the primary resolution are fatal, each declared path must exist and be a
    directory (fatal), `targets:` combined with a colliding `target.path`
    resolution is fatal.

#### 2. `vanguard/internal/docmodel/docmodel.go` + `docmodel_test.go` (Modify)

*   **Type / Scope**: Go library, document model.
*   **Proposed Changes**: optional `target:` frontmatter key, engine-owned
    (stripped before user-schema validation); grammar validated at discovery;
    surfaced as `Doc.Target`.

#### 3. `vanguard/internal/check/engine.go` (Modify)

*   **Type / Scope**: Go library, engine load.
*   **Proposed Changes**: open the store provider from the store's parent
    repository; build the target provider table (primary + `targets:` names,
    `OpenGit` when `.git` exists, `OpenHash` otherwise); `providerFor(name)`
    lookup; unknown-target fatal on load when a lock entry or a document
    names a target that is not configured.

#### 4. `vanguard/internal/check/observe.go` (Modify)

*   **Type / Scope**: Go library, attestation engine.
*   **Proposed Changes**: precondition split per the Technical Plan table —
    committed/clean via the store provider; HEAD, merge/rebase markers,
    scope dirty-free via the document's target provider; the written entry
    carries `target` when the document is bound to a non-primary target.

#### 5. `vanguard/internal/check/check.go` (Modify)

*   **Type / Scope**: Go library, check engine.
*   **Proposed Changes**: every staleness computation routes through
    `providerFor(doc.Target)` instead of the single `e.Prov`; range mode and
    `--path` reverse lookup iterate referenced targets only.

#### 6. `vanguard/internal/check/envelope.go` + `render.go` (Modify)

*   **Type / Scope**: Go library, output contract.
*   **Proposed Changes**: `Document.Target` (`target,omitempty`); text
    renderer appends `@<name>` on the status line only for non-primary
    bindings.

#### 7. `vanguard/internal/lockfile/lockfile.go` + `lockfile_test.go` (Modify)

*   **Type / Scope**: Go library, lock serialization.
*   **Proposed Changes**: optional `target` entry field; canonical
    serialization appends it when present; entry-shape validation accepts it;
    unknown target names are the engine's concern, not the serializer's.

#### 8. `vanguard/internal/check/*_test.go` (Create test fixtures)

*   **Type / Scope**: integration tests.
*   **Proposed Changes**: two-repo fixture (store repo + target repo,
    decoupled); the lifecycle cycle — observe, target commit, stale with
    attribution (reproduces the v1 refusal case, now passing); one store
    with three targets (git, git, hash); in-repo byte-identical behavior;
    old-lock compatibility (entries without `target` read cleanly); fatals
    (unknown target name, missing target path, duplicate names).

#### 9. `.vanguard/docs/engine/*.md` (Modify, M4)

*   **Type / Scope**: store contract documents.
*   **Proposed Changes**: `config.md` (targets grammar), `provider.md`
    (per-role instantiation), `lockfile.md` (entry field), `check-engine.md`
    (precondition split), `command-surface.md` (render annotation) — one
    commit per document, observe after each.

### Verification Plan

*   **Build/Lint**: `go build ./...`, `golangci-lint run ./...` (CI runs
    gofmt + staticcheck), `gofmt -l` clean.
*   **Unit + Integration**: `go test ./...` green, including the new
    two-repo decoupled fixtures and the compatibility vectors.
*   **Store self-check**: `vanguard check` exit 0 on this repository's own
    store after the doc sync.
*   **Manual reproduction**: the experiment from the 0.4.0 motivation —
    decoupled store, `observe` refused in v1 — must now complete the full
    observe → target-commit → stale cycle.

--------------------------------------------------------------------------------

*Drafted with EGM — complete draft (4/4 sections).*
