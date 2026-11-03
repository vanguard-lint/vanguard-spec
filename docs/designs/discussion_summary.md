# Vanguard — Discussion Summary

* **Date:** 2026-11-02
* **Origin:** Interactive brainstorming session (egm-brainstorm workflow)
* **Purpose:** Predecessor context for `/egm-draft`. Captures the anchored concept, decisions made, alternatives rejected, and open questions.

## 1. Original Idea and How It Evolved

The session started with an ambitious concept: *a spec-driven verification harness that locks down architectural invariants before code is written*. Specs would live in `.vanguard/` inside the repo, an engine would enforce spec-first development, AST-based drift detection would block commits that introduce undeclared public functions or external calls, and a 5-step bug-fix loop would tie spec changes to the Git DAG.

Through stress-testing, the scope was deliberately narrowed and re-shaped. The final concept is:

**Vanguard is a standalone CLI that acts as an observations ledger for specification documents.** It lives outside the codebase, does not require the git CLI, and answers one question deterministically: *which spec documents have gone stale, and why?*

### Scope evolution (deliberate cuts)

| Original ambition | Final v1 decision |
|---|---|
| AST analysis of source code, `exposes`/`depends-on` vocabulary, PR gates, fix loop tied to Git DAG | **Cut for v1.** Deferred; may return as later phases. |
| Docs inside the repo (`.vanguard/`), version binding via git tags/commits | **Reversed.** Vanguard sits outside the codebase as a separate ledger; git is an optional data source, not a deployment requirement. |
| Explicit anchors (doc bound to tag/commit, re-anchor operation) | **Replaced by implicit anchoring** via observation comparison. |
| Frozen contract states + amendment flow | **Cut.** Out of v1 scope. |

The reasoning: colocating docs with code and versioning them via git creates a *false sense of match* when no automatic check runs. Rather than rely on convention, vanguard owns a ledger of observations and computes staleness deterministically.

## 2. Core Decisions (Anchored)

### D1 — Empty engine, thin core
Vanguard ships with no opinions about document content. Users freely configure folder structure, file formats, presentation rules, and linters. The engine owns only the participation protocol:
- Stable document `id` (identity survives moves).
- Document references (`refs` to paths/symbols) and watches.
- Format-handler plugin contract (markdown AST via mdast, JSON/YAML via schema).
- The observations ledger.

### D2 — Structure verification via AST, fail-closed
Markdown = plain-text AST (mdast / unified). Verification rules run on AST nodes (required sections, heading order), never regex on raw text. Referential checks (unique ids, globs match real files, refs resolve) fail closed: broken structure counts as verification failure, not warning, because a silently no-op check is the most dangerous failure mode.

### D3 — Staleness via watches (replaces mandatory folder scope)
Not every document maps to a folder. Each document carries zero or more watches; staleness is an OR over them:

```yaml
watches:
  paths: ["src/payments/**"]   # file churn in scope (optional)
  refs: ["PaymentGateway"]     # paths/symbols the doc mentions (optional)
  docs: ["glossary-core"]      # propagation: upstream doc stale → this doc stale
  review_after: 90d            # human clock for docs that cannot be traced to code
```

### D4 — Implicit anchoring, one command
`stale(doc)` ⇔ the watch sources changed since the last observation **and** the document itself did not change in the same window. A document edit re-anchors it implicitly — no re-anchor command, no stored anchor field. Every CLI invocation lazily records an observation first, so `vanguard stale` is the only command users need for the freshness question.

### D5 — Embedded git, no git CLI dependency
Vanguard reads `.git` directly through an embedded library (gitoxide / libgit2 / dulwich — implementation language is an open question). This restores what pure hash snapshots lose: attribution (which commit made a doc stale) and retrospective queries (`vanguard stale --at v1.2`). When the target has no `.git`, vanguard falls back to content-hash observations. osv.dev was evaluated and rejected: its "git analysis" is dependency-commit-to-vulnerability matching, not path-churn analysis; vanilla git plumbing (revwalk + tree diff) covers the actual need.

### D6 — Positioning against existing tools
- **OpenSpec** (73k stars): owns the plan→apply→archive *workflow* for AI assistants; specs are fluid, no frozen state, no continuous verification. Vanguard does not compete here.
- **EGM** (`egm-drift-report`): LLM-based, pull-only (`disable-model-invocation: true`), non-deterministic, no ledger. Catches *semantic* drift that structural checks cannot express — kept as a slow-cadence complementary sensor, not replaced.
- Vanguard is the **sensor + ledger**: deterministic per-invocation checks, machine-readable JSON output (`{doc, watch_type, changed_paths, introducing_commits[]}`) that agents and other tools consume.

## 3. Alternatives Rejected (with reasons)

1. **Docs-in-repo with git-based version binding (OpenSpec-style same-commit co-evolution)** — rejected: spec and code change in the same commit, so verification against HEAD is always green; staleness becomes ritual. Also conflicts with the multi-codebase, ledger-outside requirement.
2. **Explicit anchors (tag/commit binding + re-anchor command)** — rejected as default: stale flags can never clear without a re-anchor operation, adding bookkeeping the user explicitly did not want. Retained as an optional `verified` field if certification semantics are needed later.
3. **Reusing osv.dev / osv-scanner git analysis** — rejected: category mismatch (vulnerability matching, not path churn); plain git plumbing does the job.
4. **Requiring the git CLI** — rejected: user requires a self-contained CLI; embedding a git library achieves the same data access.
5. **Mandatory folder scope for every doc** — rejected: many docs (glossaries, vision, ADRs) have no folder; the watches model with `review_after` covers them honestly (time-based review instead of pretending automatic detection exists).
6. **Pure hash snapshots without git** — kept only as fallback: loses attribution and retrospective queries.

## 4. Open Questions (carried into the draft)

- **Q1 — Engine language:** Rust + gitoxide (single static binary) vs Python + dulwich (easier embedding into agent skills). Not decided.
- **Q2 — Ledger location & cardinality:** user-specified directory (e.g. `~/.vanguard/` or a dedicated store folder)? One ledger per codebase, or one ledger managing many codebases? Not decided.
- **Q3 — Both-changed ambiguity policy:** when scoped code and the doc both changed in one observation window, trust the doc edit (fresh) or mark `unconfirmed` for review? Not decided.
- **Q4 — Default-on watches:** are `docs` propagation and `review_after` enabled by default in the core schema, or purely user-declared? Not decided.

## 5. Non-Goals (v1)

- No AST analysis of source code; no `exposes`/`depends-on` machine vocabulary.
- No CI PR gates, no frozen contract states, no amendment workflow.
- No LLM-based semantic drift detection inside the engine core (stays deterministic; external LLM scanners may consume JSON output).
- No team synchronization story for the ledger in v1 (single-machine file; ledger-in-own-repo is a future step).
