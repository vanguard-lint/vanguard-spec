# Vanguard

* **Author:** vanguard-spec working session
* **Status:** Final design — Step 6 compact assembly (this file is normative)
* **Date:** 2026-11-03
* **Process history:** `design_draft.md` (in `docs/designs/`, next to this file) carries the full drafting log — reversals, review rounds, partially-superseded refinements. This file is the final, single-source version and **wins wherever wording differs**.

--------------------------------------------------------------------------------
## Overview

Vanguard is a standalone command-line tool that keeps specification documents honest. It manages a project's spec documents (markdown, JSON, YAML, and DSL dialects such as C4), verifies that each document follows the structure its owner declared, and answers one question deterministically: **which documents have gone stale, and what exactly made them stale.**

Vanguard is designed around a single state file committed with the repository: `vanguard.lock`. Documents and configuration live in a `.vanguard/` directory — inside the repository by default, but movable outside it. It does not require the git CLI: the engine is written in **Go** and reads a repository's `.git` directory in-process through the embedded **go-git** library when one exists, falling back to content-hash snapshots when it does not. The engine itself ships with no opinions: document layout beneath `docs/`, document schemas, presentation rules, and linters are all user-declared configuration. The engine owns only the participation protocol — document identity, watches, and the lockfile.

Typical consumers are human maintainers and AI coding agents. An agent asks `vanguard check --path src/payments --format json` before working on a module and receives, per affected document, which watch fired, which paths changed, and which commits introduced the change. The single entry point for all freshness questions is `vanguard check`, with modes for one document, one path (reverse lookup), and commit ranges.

## The Problem

Spec documents decay silently. Teams write design docs, invariants, and feature specs; then code evolves and the documents stop describing reality. Nothing detects this. Existing tooling falls short in specific, verifiable ways:

- **No continuous detection.** Drift scanners (for example EGM's `EGM-drift-report`, part of the internal EGM skill suite) are pull-based: a human must remember to run them. Nothing fails, nothing signals, until someone happens to look.
- **Workflow tools prevent but do not detect.** [OpenSpec](https://github.com/Fission-AI/OpenSpec)'s delta-first workflow makes drift less likely *if followed*, but is only convention: an agent or developer who edits code directly, skipping the change folder, triggers no alarm. Its `validate` command checks artifact structure, not document-versus-code consistency.
- **Colocation creates a false sense of match.** Keeping docs in the same repository as code and versioning them with git makes people assume docs and code stay in sync. Without a deterministic check that runs on demand, the assumption is untested — and usually wrong.
- **LLM-based comparison does not scale as a sensor.** Comparing documents to code with a model each time is expensive, non-deterministic, stateless between runs, and produces reports instead of measurements.

The proposed solution: a small, deterministic engine that (1) verifies each document's declared structure, (2) keeps a single lockfile (`vanguard.lock`, committed like `go.sum`), and (3) computes staleness as a pure function of that lockfile, the embedded git object database, and the working tree — one command, machine-readable output, no daemon, no git installation.

## Goals

* **G1 — One command, many check modes.** `vanguard check` is the single entry point for all freshness questions: check all documents, check one document (`check <doc>`), reverse lookup from a code path (`check --path <path>` → affected documents), and check against a commit range (`check --from <ref> [--to <ref>]`). Zero setup steps between invocations.
* **G2 — Deterministic and cheap.** Every check is a pure computation over files and (optionally) the embedded git object database. Same inputs, same output. No LLM calls in the engine core.
* **G3 — Structure verification that fails closed.** A document is verified when it parses (markdown AST / JSON schema) and matches its user-declared structure schema — unique ids, declared links that resolve to real files. Broken references are failures, not warnings. **Watch globs are not structure**: a glob that matches zero files is a config error at `observe` time and a staleness signal at `check` time — never a document-structure failure (see Edge-case resolutions). The same precision applies layer by layer: **structural errors are always fatal**; **attestation gaps** (`unobserved`) are loud but informational unless `--strict` — with exactly one deliberate default escalation: a **lost lock** (lockfile absent + every document unobserved) sets the stale bit by default, so degradation can never ship green silently (see the lock section).
* **G4 — Zero-dependency CLI, offline by default.** The binary runs without the git CLI installed. The offline guarantee covers the **freshness hot path**: `check` and `observe` never open a socket. Network appears only in explicit, user-initiated places outside that path: user-declared bindings whose commands themselves use the network, and a remote provider (`provider: github-api` / `gitlab-api` / custom) when explicitly configured. When the watched project has a `.git` directory, vanguard reads it in-process for attribution and retrospective queries; otherwise it degrades to content-hash observations.
* **G5 — User-owned configuration.** File formats, structure schemas, and linter rules are declared by the user. Document discovery is rooted at `.vanguard/docs/` (layout beneath it is user-defined; the root itself is fixed), watch globs for code paths are user-declared, and discovery globs for filtering documents resolve relative to `docs/` and cannot escape it. The engine defines only: document identity (`id`), the watch model, the format-handler contract, and the lockfile.
* **G6 — Machine-readable by default.** Every check command emits structured JSON (one envelope for all modes — see the output contract under *Check command surface*: per-document results with `violations[]`, `bindings[]`, and per-document `hints[]`, plus store-level `unwatched`/`unobserved`/`affected_in_range` sections and the advisory `warnings[]`) so agents and CI scripts consume results without parsing prose.
* **G7 — Flexible placement, in-repository by default.** Documents, configuration, schemas, and the state lockfile live in `.vanguard/` at the repository root by default and are committed with the code. The lockfile is committed **like `go.sum`**: its content (content hashes, HashProvider baselines) is machine-independent, so sharing it through git gives every machine — and CI — the same recorded baseline for free. The directory can be relocated outside the repository via configuration for teams that keep the store separate. The watched codebase needs no hooks and no build changes; the only thing `vanguard init` writes into the repository is the `.vanguard/` scaffold.
* **G8 — Self-describing interface: no companion skill.** AI agents learn vanguard from the CLI itself — `--help`, and above all from the **actionable feedback of every check**: each failure names what failed, where (file + line from the AST node), and the exact next command (e.g. "run `vanguard observe` to re-anchor"). Vanguard ships no skills, slash commands, or agent instruction files; the output is the teaching interface.

## Non-Goals

* **NG1 — No source-code analysis in v1.** No AST parsing of programming languages, no `exposes`/`depends-on` machine vocabulary, no detection of new public functions or undeclared external calls. (Deferred; the watch model is the future attach point.)
* **NG2 — No CI gates or enforcement workflows.** No PR blocking, no frozen contract states, no spec-amendment approval flow. Vanguard reports; it does not police.
* **NG3 — No LLM inside the engine.** Semantic drift ("the code uses a different pattern than proposed") is out of scope. External LLM scanners may consume vanguard's JSON output but are not part of the engine.
* **NG4 — No server, no daemon, no cloud sync.** All team synchronization happens **through git itself**: the committed `vanguard.lock` travels with the repository like `go.sum`. Vanguard never phones home and never runs a background process.
* **NG5 — No document content authoring or templates.** Vanguard verifies and watches; it does not generate or edit documents.
* **NG6 — No replacement of OpenSpec or EGM.** Vanguard is complementary: OpenSpec owns plan→apply workflows, EGM owns deep multi-lens review; both can consume vanguard output.
* **NG7 — No prose-linter orchestration.** Vanguard itself never invokes Vale, markdownlint, or any prose/style validator during its own checks, and never depends on external tooling for structural verification. What a team wires into the user-declared `bindings:` allowlist is the team's own pipeline decision — but the engine's mapping rule is absolute: **binding outcomes are never mapped onto staleness**. A failing binding (style, test, or otherwise) surfaces as `binding_failed` (exit bit 16, a dedicated category), never as `stale` — so a style error can never masquerade as "this document no longer matches the code". Teams that want zero style signal in vanguard's exit code simply do not bind Vale.
* **NG8 — No agent-skill packaging.** No skills, slash commands, AGENTS.md, or MCP servers are distributed. Integration for agents is the CLI's self-description: `--help` and actionable check feedback (see G8).


## Open Questions (carried — none blocks M0)

* **Monorepo performance ceiling** — cheap for directory-aligned and repository-wide scopes (O(path depth) / O(level-1 entries)); the residual risks are *many arbitrary globs* (per-doc enumeration) and long first-parent recovery walks — both measured in the M0 spike (tasks 0.7–0.8) before implementation commits.
* **Lock merge ergonomics** — canonical sorted form keeps merges line-clean like `go.sum`; the only semantic conflict is two branches observing the *same* document (recovery: re-run `observe` on the merged tree) — UX definition pending.
* **`content_hash` split (`metadata_hash` + `body_hash`)** — a post-v1 hint-quality candidate ("you edited only frontmatter" vs "the prose changed"); **no semantic change** to when observe is required — every edit still needs re-attestation.
* **Squash-merge acceptance pilot** — v1 default decided: **no automatic remediation**; stale-after-merge is the correct outcome until a human re-attests on the rebased history (the prescribed workflow is a merge queue). The pilot (DIP task 4.7, run after M4) measures the stale-after-merge rate with and without a queue; a measured rate practitioners reject reopens the post-merge-observe-job alternative explicitly with its trust trade-off.
* **`exclude:`** — the first scheduled v1.1 feature (v1 has no way to say "everything except a subtree"; `!` negation stays rejected to keep the scope hash a declarative pure function).

## Resolved decisions (ADR summary)

The body is the **normative specification** and owns every semantic; this table is the one-line index. Full process history stays in `design_draft.md` (same directory).

| # | Decision (one line) | Normative home |
|---|---|---|
| D1 | Engine language: **Go + go-git** — embedded ODB (git object database) reading, no git CLI anywhere in the engine, offline by default (G4); dirty detection works through the same embedded ODB (see dirty detection) | Target resolution and providers |
| D2 | Store: `.vanguard/` committed inside the repository by default; relocation outside it is configuration | Store layout |
| D3 | Command surface: `check` (all / `<doc>` / `--path` / `--from`) + `observe` + `init` | Check command surface |
| D4 | Both-changed policy — the commit is the only version unit; `observe` refuses to anchor against uncommitted churn | Anchoring (both-changed) |
| D5 | Watch model: exactly `paths` + `review_after`, all opt-in; `docs` and `refs` watches removed; staleness never propagates between documents | Document model |
| D6 | Bindings are transient — executed only under `--run-bindings`; outcome never mapped onto staleness (bit 16) | Tests bindings |
| D7 | Check purity: `check` never writes; `observe` is the only lock writer | Anchoring (lock write rules) |
| D8 | **Stored anchor** in the committed lock, advanced only by observe; three-path resolution survives squash-merge | Anchoring (anchor resolution) |
| D9 | Formats: markdown (goldmark + closed rule vocabulary), json/yaml (JSON Schema 2020-12), dsl (leading comment block); `schema:` mandatory for every format — `none` is the explicit opt-out, an omitted field is a load-time fatal (amended: optional bindings left a silent backdoor, violating G3 fail-closed); agents learn via `--help` + actionable output (NG8) | Structure schemas; Document model |
| D10 | Tool provisioning (`vanguard install`, pinned `url`+`sha256` fetch-and-verify): **deferred post-v1** — v1 bindings resolve from the ambient PATH; the `Provisioner` seam records where provisioning returns | Tool provisioning (deferred) |
| D11 | Single committed state file `vanguard.lock` (no `state/`, no `observations.jsonl`), committed like `go.sum` | Anchoring (lock identity) |
| D12 | `observe` restored as the standalone attestation command; per-document status choice **Stale > Dirty > Fresh**; a fatal is a run-level bit, never a status | Anchoring; Output contract |
| D13 | Cosmetic-commit hole closed by observe attestation; review stamps / trailers rejected | Honest limit (preconditions) |
| D14 | Squash-merge survivability via the **net-state rule** — staleness decided by tree-state comparison, revwalk only for attribution | Anchoring (formula) |
| D15 | `unobserved` unified — no implicit anchor in any mode; two-tier exit: per-doc gaps informational unless `--strict`, **a lost lock sets the stale bit by default**, corrupt lock is fatal | Anchoring (lock loss) |
| D16 | `observed_at` = the attestation instant (`--at`-injectable, system-UTC fallback), never the anchor commit's timestamp | Lock entry table |
| D17 | `refs` watch removed — blob reading breaks blobless clones and drifts into NG1; dirty detection's scoped HEAD-blob reads are the one exception (see body) | Anchoring (dirty detection) |
| D18 | `review_after` baseline is `observed_at`; re-observing an unchanged document is first-class; `content_hash` powers `document_changed` (watched or not) | Anchoring (formulas) |
| D19 | Exit bitmask (1/2/4/8/16/32, composed by OR); range mode is lockfile-free, exit 0 by default, bit 32 under `--fail-on-affected` | Output contract |
| D20 | Concurrency: the lock write is a content compare-and-swap; a conflict aborts the run — re-running is the whole recovery | Lock write rules |

## Implementation status (honest ledger)

The body is the v1 **contract** — written in normative present tense — not a claim that every paragraph ships today. What exists in code now: `init` / `check` / `observe` (including standalone `--prune`), `paths` + `review_after` watches, GitProvider + HashProvider, anchoring with three-path recovery, dirty detection over the embedded ODB (two-phase: status candidates confirmed by normalized HEAD-blob comparison), the committed lock with CAS writes, bindings execution (`--run-bindings`, exit-code-only decisions, no shell), structure verification via declared schema profiles, `text` / `pretty` / `json` / `yaml` output (`--no-color` / `NO_COLOR` included), the envelope with store-level `warnings[]`, envelope schema 1, and the exit-bit composition. Every other element the body declares carries its status here — one table, one source of truth; if code and body disagree beyond this table, the bug is in this ledger:

| Body element | Normative home | Status in code |
|---|---|---|
| Structure-verification phase | D9; Structure schemas; M2 | **Ships, deviating from D9's shape** — `internal/structure` loads `.vanguard/schemas/<name>.schema.json` and enforces the declared `x-vanguard` profile by heading-line scan: required sections (in order, no duplicates), citation sections with a per-section citation regex, and `max_depth` (≤ 32, validated at load). It is **not** the goldmark-AST rule vocabulary of D9; `frontmatter.require/forbid`, `require_any`, `pattern`, `require_child_under`, `forbid_empty_sections`, `min_words`, `require_list_under`, `forbid_broken` are **not started**, and JSON-Schema verification for json/yaml documents is **not started** (dsl documents carry no structure verification by design; their leading-comment-block metadata extraction ships) |
| `vanguard verify`, Tier A/B/C integrity checks, exit bit 64, `lock_integrity` envelope field | lock-integrity-section.en.md | **Not started, unratified** — a proposal document with zero code coverage (a test whitelist already anticipates the subcommand; a hint naming `verify` would fail at runtime) |
| `vanguard install` (pinned fetch-and-verify, `.vanguard/bin/` PATH resolution, committed-binary checksum rule) | D10; Tool provisioning | **Deferred post-v1, not started** — bindings resolve from the ambient PATH; nothing downloads or checksum-verifies binaries |
| Committed-document precondition against the **store's own repository** (decoupled store) | Anchoring (observe preconditions) | **Deviates** — the precondition is evaluated against the **target** provider using the document's path relative to the target root: correct when the store lives inside the target repository (root-level **or nested**); a decoupled store's document path does not exist in the target, so `observe` refuses there. Decoupled observe needs its own provider seam — not started |
| `--repository-path <path>` CLI flag | Target resolution and providers | **Not started** — target resolution is config `target.path` → the store's containing repository |
| `cmd:` single-string shorthand (shlex split) for bindings | Binding execution contract | **Not started** — commands must be declared as YAML arrays |
| Timeout kill of the whole process tree (Job Object on Windows) | Binding execution contract | **Not started** — the 600s default timeout kills the direct child only |

## Critical User Journeys (CUJs)

### CUJ 1: An agent checks freshness before implementing

* **Given**: A codebase whose spec documents are discovered by vanguard's configured globs; an AI agent is about to modify payment logic.
* **When**: The agent runs `vanguard check --path src/payments --format json`.
* **Then**: The agent receives a structured list of affected documents — each with per-document churn attribution (changed paths, introducing commits) where the anchor's history provides it, and scope-level detail plus an `observe` hint where it does not (unobserved or recovery-path documents). No git binary is invoked.

### CUJ 2: A maintainer verifies document structure after editing

* **Given**: A maintainer edits a markdown spec; its frontmatter declares `schema: feature-spec`.
* **When**: The maintainer runs `vanguard check <doc>`.
* **Then**: Vanguard applies the `feature-spec` structure rules (user-declared, loaded from `.vanguard/schemas/`) and reports freshness status. Re-anchoring follows the commit-as-version-unit flow: the edit alone does not clear staleness — once the edit is **committed**, `vanguard observe` advances the anchor to that commit and `check` reports fresh. A rule violation fails the command with the fatal bit set, not a warning. **Friction made visible**: *every* document edit — even a typo fix — costs edit → commit → `observe` → commit-lock, because `content_hash` covers the whole file; that is the deliberate price of "attestations are explicit git artifacts".

### CUJ 3: A document with no code churn ages by clock

* **Given**: A glossary document with no `paths` watch, only `review_after: 90d`.
* **When**: `vanguard check` runs 100 days after the glossary's last attestation (`observed_at` in the lock).
* **Then**: The glossary is reported stale with `triggered_watches: [review_after]`. Its staleness clears only when a human re-runs `vanguard observe` (the review act) — editing the document does not reset the clock.

### CUJ 4: Path-hit attribution on a git-backed project

* **Given**: The watched project has a `.git` directory; tag `v1.2` exists.
* **When**: A maintainer runs `vanguard check --from v1.2` (or a commit range `--from <sha> --to <sha>`).
* **Then**: Vanguard reads the changed-file list from the embedded git object database and reports documents whose watch globs intersect it as `affected_in_range`, with per-document introducing commits — computed entirely without the lockfile.

### CUJ 5: A document proves its described behavior through a bound test

* **Given**: `vanguard.yaml` declares a binding `refund-suite: "go test ./payments/... -run TestRefund"`; a document references it via its top-level `bindings: [refund-suite]`; the watched paths churned in the last commit range (so the document is stale by its `paths` watch).
* **When**: A maintainer runs `vanguard check payments-spec --run-bindings` and the binding passes.
* **Then**: The output shows both signals side by side: the watch-derived status (`stale` with the introducing commits) **and** the binding's `pass` proof — direct verification that the described behavior still holds against the current code. Nothing is persisted: the next plain `vanguard check` reports the bindings as `pending` again. A failing run would report `binding_failed` (bit 16) — never `stale`.

--------------------------------------------------------------------------------

## Technical Plan (Architecture)

### Component Overview

Vanguard is a single Go binary. Four internal components sit between the CLI and two providers (the `.vanguard/` store and the watched project's git object database or filesystem).

```text
                        +---------------------------------------------+
                        |                vanguard CLI                 |
                        |        init | observe                       |
                        |   check <doc> | check --path <p>            |
                        |   check --from <ref> [--to <ref>]           |
                        |   flag: --run-bindings                      |
                        +----------------------+----------------------+
                                               |
            +------------------+---------------+----------------+
            |                  |               |                |
            v                  v               v                v
   +----------------+  +-------------+  +-----------+  +----------------+
   | Config Loader  |  | Doc Model   |  | Watch     |  | Check Engine   |
   | vanguard.yaml  |  | id, schema, |  | model     |  | (modes +       |
   | discovery,     |  | watches     |  | paths,    |  | freshness      |
   | bindings       |  | bindings    |  | review_   |  | computation)   |
   |                |  |             |  | after     |  +-------+--------+
            |        +------+------+  +-----+-----+          |
            |               |                |                |
            |               v                v                v
            |        +-------------+  +--------------------------+
            |        | Structure   |  | vanguard.lock            |
            |        | schema      |  | doc -> {anchor,          |
            |        | profiles:   |  |   observed_at,           |
            |        | sections,   |  |   scope_tree_hash,       |
            |        | citations,  |  |   content_hash,          |
            |        | max_depth   |  |   scope_file_count,      |
            |        +-------------+  |   scope_snapshot?}       |
            |                        +-----------+--------------+
            |                                     |
            +------------------+------------------+
                               |
              +----------------+----------------+
              |                                 |
              v                                 v
   +--------------------------+    +---------------------------+
   | GitProvider (go-git)     |    | HashProvider (fallback)   |
   | resolve ref -> commit    |    | content hash of scoped    |
   | first-parent walk from   |    | files; no history, no     |
   | HEAD (stop at anchor's   |    | attribution               |
   | reachable set) -> changed|
   | paths; dirty detection:  |
   | status candidates con-   |
   | firmed vs HEAD blobs     |
   | (normalized; blobless    |
   | clone = dirty-loud +     |
   | hint)                    |
   +--------------------------+
              |                                 |
              v                                 v
   +--------------------------------------------------+
   | Target repository (.git dir, read-only) / plain files  |
   | (same repository, or local sibling via target.path)    |
   +--------------------------------------------------+

   RemoteGitProvider (opt-in, post-v1): hosted API
   (github-api / gitlab-api / custom) -> changed_files[]
   between two refs; zero-disk, network only when configured.
```

### Store layout (`.vanguard/`, committed with the repository by default)

```text
.vanguard/
├── vanguard.yaml          # user-owned config: doc discovery globs (relative
│                          # to docs/, cannot escape it), watch declarations,
│                          # format handlers, schema refs, bindings allowlist,
│                          # clock_skew_tolerance (default 5m),
│                          # state relocation (outside-repository option)  [committed]
├── vanguard.lock          # per-doc state (see Anchoring) — committed like
│                          # go.sum: machine-independent hashes shared
│                          # through git; "lock" means "do not hand-edit"
├── docs/                  # THE ONLY document root — user's spec documents
│                          # (markdown/json/yaml/dsl) in any user-defined
│                          # layout beneath it                      [committed]
└── schemas/               # user-declared structure schemas per format [committed]
```

**Document discovery is rooted at `.vanguard/docs/` and nowhere else.** Discovery globs resolve relative to `docs/` and cannot escape it (`../` is rejected); engine-owned files elsewhere in `.vanguard/` — `vanguard.yaml`, `schemas/`, `vanguard.lock`, `bin/` — are never treated as spec documents, even when their format would match. Watch globs are a separate mechanism: they match **code paths in the target repository**, never files inside `.vanguard/`. Everything in `.vanguard/` is committed with the code — including `vanguard.lock`, whose machine-independent content (anchors, HashProvider baselines) is shared through git exactly like `go.sum`. Nothing else is written outside the scaffold: no hooks, no `.gitignore` edits, no daemon.

**Discovery details, fixed so two implementations agree:** candidate files are processed in **lexicographic path order** (duplicate-id detection is then order-independent and reported deterministically). **Symlinks under `docs/` are not followed** — a symlink is not a document (its content lives outside the tree; treating it as one makes content-addressing meaningless), and it is skipped with an advisory warning. A file matching the discovery globs whose **extension maps to no registered format handler** is skipped with an advisory warning too — a stray scratch file must not red the pipeline; the extension→format mapping lives in `vanguard.yaml` and is validated at load (a configured format referencing no rule schema and no DSL mode is a config error, fatal). Every skip and every discovery warning carries file + reason (G8).

**`id` grammar and resolution — closed rules.** `id` matches **`[a-z0-9][a-z0-9._-]*`**, max 128 chars, ASCII only, compared **case-sensitively** and unique across discovery (case-variant duplicates collide like exact ones). Rationale: the id is a JSON key, a CLI argument, and a lock key simultaneously — `/`, whitespace, and Unicode would make the CLI ambiguous and JSON escaping load-bearing. An `id` failing the grammar is a fatal at discovery.

**Resolving `check <doc-id>` — one rule.** The argument is an **exact document id** (case-sensitive); an unknown id is a usage error with the hint *"no document with id `<id>` — documents live under `.vanguard/docs/`"*. There is no path-argument form in v1: paths belong to `check --path <path>`, and ids are the only per-document selector — a single resolution rule two implementations can never disagree on.

### Decoupled stores and multiple targets (0.4.0)

**Two provider roles, pinned by precondition.** The engine instantiates providers per role, never one provider serving both: the **store provider** — opened against the repository that contains `.vanguard/` — answers exactly two observe preconditions (document committed, document clean); the **target provider** — one per target — answers every staleness computation (scope hash at HEAD, dirty detection, attribution, merge/rebase markers, HEAD existence, `--path` reverse lookup). A question about documents is never asked of a target; a question about code is never asked of the store. An observe on a decoupled store records both sides in one entry: `content_hash`/`observed_at` describe the store-side document, `anchor`/`scope_snapshot` describe the target's state at its HEAD — and the lock is committed in the store repo, whose git history is the attestation ledger.

**Named targets, bound per document.** `vanguard.yaml` may declare a `targets:` map (name → local path) beside the legacy `target:`; a document's optional engine-owned frontmatter key `target:` names one of them. Omitted means the primary target (legacy `target.path`; in-repo default unchanged). One document binds to exactly one target — overlapping coverage is expressed by separate documents. Validation is fatal at load: a target name must match the document-id grammar, duplicate names are rejected, a declared target path must exist and be a directory (a declared target pointing at nothing is a config error, never a silent fall into hash mode), a frontmatter naming an unknown target is fatal at discovery, and a lock entry naming an unconfigured target is fatal at load — the lock cannot outlive a target rename silently.

**Degradation per target is honest and closed.** A target with `.git` runs the full axis set (paths staleness, attribution, dirty). A git-less target runs the hash provider: net-state staleness by re-enumeration — no attribution, no dirty/stale distinction, and the envelope says so rather than inventing history.

**Compatibility is zero-migration.** The lock entry's `target` field is optional: omitted means primary, so 0.3.0 locks read and write identically, and the new field is unknown-data to an older binary. The envelope's `Document.target` is optional and additive — the envelope schema stays at version 1; the text renderer annotates only non-primary bindings. In-repo stores without `targets:` behave byte-identically to 0.3.0.

**Cross-target surface.** `check --path` resolves the reverse lookup against every target at least one document references. Range mode runs per document against its own target's history, aggregating `affected_in_range` across targets. W1 `verify` rides the same lock-schema pass: Tier B cross-checks each entry's anchor against its own target's ODB.

### Memoization within a run (0.4.0)

**One snapshot per run.** A `check` or `observe` invocation evaluates every document against a single snapshot of each target's state: HEAD, the HEAD tree, the worktree status, and the attribution baseline are read once per target per run and reused. Without this, a long run mixes states — document one is judged at second zero and document five hundred at minute ten — and the envelope reports a blend that corresponds to no instant in time. Memoization is therefore a consistency improvement, not merely a speedup: the drift window shrinks from the run's duration to the snapshot instant.

**Only deterministic functions of immutable inputs are memoized.** The head tree and the scope hash at HEAD are pure functions of the commit; the worktree status is captured once per run and filtered per document; the attribution baseline (the reachable set below an anchor) is a pure function of the anchor commit. Everything time-dependent is never memoized across a run boundary — `review_after` is evaluated from the clock at evaluation time — and everything worktree-dependent is never reused across runs: dirty state changes without HEAD moving, so a persistent cache keyed by HEAD would lie. Per-path blob comparisons in dirty confirmation read the live worktree and stay live.

**No new artifact, no boundary change.** In-run memoization lives in memory only: `check` still never writes, `observe` remains the only writer of `vanguard.lock`, and no cache file enters the store. A persistent cross-run cache is a separate decision that would require a complete cache key (every input that determines the value, including config), tamper-evidence equal to the lock's, and a canon amendment before any code.

**Semantics are byte-identical.** Memoization must not change any envelope: the same store, lock, and target state produce the same bytes as an unmemoized run. The existing byte-identity and lifecycle tests guard this; a memoized value that could differ from a fresh computation for the same snapshot is a bug, not an optimization.

### Structure schemas (`.vanguard/schemas/`)

Each format has its own schema language; a document's `schema:` field keys into one of these files.

**JSON documents** use the standard JSON Schema dialect directly — no custom vocabulary. The engine-owned fields (`id`, `watches`, ...) are verified as required properties automatically, on top of whatever the user's schema declares.

**Markdown documents** use a declarative rule vocabulary owned by the engine (v1: closed set). A schema file:

```yaml
# .vanguard/schemas/feature-spec.yaml
id: feature-spec
format: markdown

frontmatter:
  require: [title, author]     # user-declared fields on top of the engine-owned ones
  forbid: [draft_notes]        # fields that must not appear

headings:                      # all structural; subsequence semantics for order
  require_order:               # required h2 sections must appear in this relative
    - Overview                 # order (subsequence matching: extra headings may
    - The Problem              # appear between them)
    - Goals
    - Non-Goals
  require_any:                 # at least one of these must exist (any level)
    - Risks
    - Open Questions
  max_depth: 4                 # deepest allowed heading level
  forbid_empty_sections: true  # a required section with no content is a violation
  pattern:                     # applies ONLY to headings matching the selector,
    - level: 3                 # never to sibling headings en masse
      match: "^Requirement: "
  require_child_under:         # parent pattern must contain >= 1 child pattern
    parent: "^Requirement: "
    child: "^Scenario: "

content:
  min_words: 200               # sanity floor for the whole document

lists:
  require_list_under:          # these sections must contain at least one list
    - Goals
    - Non-Goals

links:
  forbid_broken: true          # relative links must resolve to real files
```

**Every rule is a selector plus an assertion.** A bare selector without a constraint defaults to `require: exists` (at least one match). The `pattern` group above reads: *headings matching `^Requirement: ` must exist at level 3*, and *each of them must contain at least one `^Scenario: ` child* — `match:` is both the selector and the required conformance, so no rule is ever a selector without a check. The full assertion semantics per key are fixed by the closed vocabulary below; a schema that uses an unknown key fails config validation (fatal), never silently no-ops.

**The v1 rule vocabulary (closed set, engine-owned):**

| Rule group | Keys | Runs on |
|---|---|---|
| `frontmatter` | `require`, `forbid` | frontmatter map |
| `headings` | `require_order`, `require_any`, `max_depth`, `forbid_empty_sections`, `pattern`, `require_child_under` | heading AST nodes |
| `content` | `min_words` | text nodes |
| `lists` | `require_list_under` | list AST nodes under a heading |
| `links` | `forbid_broken` | link AST nodes |

Semantics pinned to remove every ambiguity: `require_order` constrains the **ordered sequence of level-2 (h2) headings** — the declarations must appear as h2s, in that order, contiguous (nothing between them unless listed); `require_any` matches headings **at any level**; `min_words` counts **body text nodes only** — frontmatter and fenced code blocks are excluded, so a code-heavy document cannot fake a floor, and a mere schema dump cannot ride over the prose requirement.

The shipped x-vanguard projection (`internal/structure`) pins three further semantics. **Contract lookup**: the block may sit at the schema's top level or inside one combinator branch (`anyOf`/`oneOf`/`allOf`, one level deep — the shape package contracts use); identical copies are one contract, and distinct copies are an ambiguous schema that **fails closed** — the engine never guesses a merge that would silently weaken whichever constraint the author meant. **Quoted material is invisible**: the contents of a code fence (` ``` ` / ` ~~~ `) are neither the document's structure nor its claims — a heading quoted inside an example is not a section, a fenced bullet is not a claim, and a commented heading in a shell block violates no depth bound; a scanner that reads fences false-passes contracts satisfied only by example text and phantom-fails quoted headings. **A bullet is one claim across its wrapped lines**: markdown wraps freely, so the citation may sit on any line of the item — checking only the bullet's first line phantom-fails every wrapped claim.

**Citation matching is deliberately unanchored.** The engine searches the citation pattern anywhere in the bullet's text (a substring match, not anchored to the item's start or end): a claim may speak around its citation, and a bullet that merely mentions a citation in passing satisfies the contract. This is a design choice, not an oversight — anchoring would trade usefulness for purity by failing every mid-sentence citation. Placement strictness belongs to the pattern owner: a schema that demands anchored citations writes `^`/`$` into its own `citation_pattern`, and the engine honors that regex exactly as written.

### Responsibility separation: provisioning is not orchestration

Verification scope ends at the structural layer. Vanguard has **no escape hatch, no plugin execution, no shell-out to prose or style tools** in its own checks: it never invokes Vale, markdownlint, or arbitrary validators during default `check`, and never depends on their presence. The single, explicit execution boundary is the `bindings:` allowlist — user-declared commands, executed only when `--run-bindings` is passed. Binding outcomes never alter freshness state (see below); what a team binds is the team's pipeline decision.

> **Provisioning is not orchestration.** Vanguard v1 does not provision external binaries at all (`vanguard install` is **deferred post-v1** — see Tool provisioning): the core engine never invokes anything during default checks (`check` without `--run-bindings`), and external execution remains strictly opt-in and quarantined within the `--run-bindings` execution boundary.

**Exit code is the only decision input for bindings — and its outcome is never staleness.** The core engine determines binding pass/fail strictly by process exit code (0 = pass, non-zero = fail) — it never interprets third-party tool output for status calculations. The optional `output_parser` attribute (e.g. `vale-json`, `regex-diagnostic`) is an auxiliary **diagnostic adapter**: it transforms external stdout/stderr into a structured, unified diagnostic report (file, line, col, message) for human and agent feedback, without altering state transition logic. Outcome mapping, in both directions:

* **pass** → reported as `binding: pass` — a direct proof that the described behavior holds, displayed **beside** the watch-derived status. It does not override a stale/dirty status; it complements it (an agent can see "stale by paths watch, but the bound test passes").
* **fail** → reported as `binding_failed` with the failing command, exit code, and parsed diagnostics — **a dedicated exit bit (16)**, orthogonal to the freshness chain. A Vale style error and a failing test are both `binding_failed`; neither is ever reported as `stale`.

| Concern | Owner |
|---|---|
| Document outline: required sections and order, heading hierarchy and naming patterns, parent/child heading relations (e.g. `Requirement:` → `Scenario:`), non-empty sections, list presence, link resolution, size floor | **Vanguard** (deterministic, self-contained) |
| Prose quality: heading casing, required terms within a section, style-guide conformance, readability | **The team's own tooling** (e.g. Vale with its Google style package), run as a separate pipeline step beside — never inside — `vanguard check` |
| Semantic drift: tone, "documented reasons", truth of prose against code | External LLM layer (NG3), outside vanguard entirely |

The schema example above deliberately contains no external-command field. Teams that want prose checks run Vale themselves with their own configuration; violations from that step never flow through vanguard's exit codes or reports.

**Expressiveness boundary (honest limit).** Prose-level rules in real standards — active voice, tone, "alternatives must have documented reasons" — are semantic judgments no structural vocabulary can model. They are out of vanguard's responsibility by design (Non-Goal NG3 and NG7).

Every rule evaluation produces a violation with the rule id and the source location taken from the AST node (line number) — never from text matching. Violations feed the `check <doc>` report and exit code 1. Extending the vocabulary (custom rule functions, external handler plugins) is future work outside v1's closed set.

### Document model (engine-owned minimum)

Every discovered document carries exactly these engine fields; everything else is user-defined and opaque to the engine:

```yaml
id: payments-spec            # unique across the store; identity survives moves
schema: feature-spec         # key into user-declared structure schemas
watches:                     # staleness sources; all optional; none = no
                             # watch-driven staleness (document-side drift
                             # still applies once observed)
  paths: ["src/payments/**"] # net change in these paths makes the doc stale
  review_after: 90d          # opt-in human clock
bindings: [refund-suite]     # OPTIONAL, NOT a watch: binding NAMES from the
                             # vanguard.yaml allowlist, executed only via
                             # check --run-bindings (see Tests bindings)
```

Markdown uses YAML frontmatter; JSON and YAML documents place the same fields at the top level; DSL documents (e.g. C4/Structurizr) embed them in a leading comment block. Structure verification depends on the format:

| Format | Parse | Engine metadata location | Structure verification |
|---|---|---|---|
| markdown | goldmark AST | YAML frontmatter | rule vocabulary (`schemas/*.yaml`) |
| json | `encoding/json` | top-level fields | JSON Schema |
| yaml | `yaml.v3` | top-level fields | JSON Schema dialect (YAML is a superset of JSON — no new schema language) |
| dsl (C4/Structurizr, ...) | tolerant line-based scan | leading comment block (`//` or `#` lines containing `key: value`) | **none** — the `schema` field is optional; DSL content is opaque to the engine. Only engine-owned fields (id, watches, bindings) and referential checks apply |

**The `dsl` metadata block, precisely.** After an optional UTF-8 BOM and an optional shebang (`#!`) line, the **leading comment block** is the maximal run of consecutive lines each starting (after optional whitespace) with `//` or `#`; the block ends at the first line matching neither — **blank lines are tolerated inside the block** (skipped, not terminators), and a `#!` line is skipped wherever it appears in the run. Engine fields are extracted only from block lines matching `(//|#)\s*([a-z_][a-z_0-9]*):\s*(value)$` — everything else in a DSL document is opaque. Block comments `/* */` are **not** recognized in v1, and a document whose engine fields cannot be found in the leading block is a malformed-document error at discovery (reported with the field-location hint, G8) — the parser never scans the whole file for fields, because that would make file *content* authoritative over structure.

Violations of any applicable layer fail closed with the fatal bit set.

**Engine-owned frontmatter is stripped before user-schema validation.** The keys `id`, `schema`, `watches`, `bindings` (and any future engine field) are extracted by the document model and **removed from the payload before the user's schema runs** — a user schema with `additionalProperties: false` never sees an engine field, so reserved-name conflicts cannot break user validation in either direction (engine fields are also never filtered out by `forbid:` rules). **`schema:` is mandatory for every discovered document** — an omitted field is a load-time fatal (*"declares no schema — every store document must bind one; declare `schema: none` to opt out explicitly"*), and `none` is the explicit opt-out that runs only the engine-owned checks (parse, id uniqueness, watch/binding reference resolution) with the payload unconstrained. A document cannot ride silently without declaring whether it binds a schema or not — the silent omission was a backdoor that violated G3 fail-closed (the D9 amendment).

**Frontmatter and config parsing are strict — fail-closed.** **Duplicate keys** are a parse failure (fatal): an ambiguous map can never be interpreted, and guessing would pick whichever implementation order loaded last. **YAML anchors, aliases, and merge keys are rejected** in document frontmatter *and* in `vanguard.yaml` alike — identity-hiding constructs make "what did this document declare" unreviewable in a PR diff, and the YAML-bomb hazard (exponential alias expansion) never enters the process. An unparseable frontmatter or config file is a structure/config violation (fatal). A UTF-8 BOM is tolerated and stripped before parsing (the same pipeline rule as content hashing).

### Anchoring — stored in the lock, advanced by `observe`

**The anchor is a stored, explicit attestation.** Each document's `anchor` commit lives in `vanguard.lock` and is advanced **only** by `vanguard observe` — the maintainer's (or agent's) explicit statement: *"this document is accurate as of this commit."* Editing the document alone never moves the anchor. Staleness for a `paths` watch is then a pure net-state computation:

```text
anchor(doc)      = lock entry's `anchor` commit (written by observe)
                 | no lock entry → the document is `unobserved`
                   (reported as such in BOTH provider modes — never
                   guessed fresh, never silently fresh)

scope state(doc) = scope_tree_hash recomputed over the watch scope at
                   HEAD (`.vanguard/` excluded — the three construction
                   points govern how)

stale_conditions(doc) = any of:
    paths:        scope state(doc) ≠ lock's `scope_tree_hash`
    document:     normalized full-document hash ≠ `content_hash` # doc edited after
                                                                 # last observe
    review_after: now − `observed_at` > declared duration

dirty_condition(doc) = worktree differs from HEAD inside the watch glob

status(doc) = stale   if any stale_condition holds (dirty may also hold)
            = dirty   if ONLY the dirty_condition holds
            = fresh   otherwise
```

**Net-state rule — staleness is decided by scope *state*, not by churn events.** Both resolution paths (below) compare the scope's tree hash at HEAD against the lock's `scope_tree_hash`; the revwalk exists only for **attribution** (changed paths, introducing commits). This makes the two paths unable to disagree: an A→B→A churn window leaves the scope state identical, so the document is fresh on the fast path exactly as it is on the degraded path — the intermediate churn is visible through attribution, never through status.

**`.vanguard/` is excluded from every watch scope and every `scope_tree_hash`.** When the store lives inside the target repository, a repository-wide watch would otherwise flag its own lock commits as code churn, and the repository-wide `scope_tree_hash` would include `vanguard.lock` — the file that *contains* the hash (a self-referential cycle that can never match). The exclusion is unconditional and applies to dirty detection as well: vanguard's own bookkeeping is never churn. A corollary for users who watch `**/*.md` while the store holds `.vanguard/docs/*.md`: the documents themselves are **also** excluded — a doc edit can never register as code churn in another doc's watch, which is the deliberate realization of "staleness never propagates between documents" (the docs-watch removal). A document watching its own store directory is a configuration smell the discovery validator flags as a warning.

**Relocation constraint, stated as a hard rule.** The exclusion above is *constructional* only where it is built in: the repository-wide hash is a synthetic **level-1** list, which excludes the store directory **when it sits at the root level**. A store relocated **anywhere deeper** inside a watched tree (`services/payments/.vanguard/`) re-creates the self-referential cycle through the enclosing subtree — so the fatal store-inside-scope validation applies **at every depth**: a relocated store may not lie under any watch glob, *including a repository-wide `paths: ["**"]` watch*. A non-root store therefore cannot coexist with a repository-wide watch — relocate the store to the root (excluded by name) or narrow the scope; config validation fails fatal with that hint.

**How the exclusion actually happens — exactly three construction points, no fourth.** So that no prose reading can make "always excluded" mean something the hash formulas cannot deliver:

1. **repository-wide watch**: the synthetic level-1 list is built **excluding the store's level-1 directory by name** — exclusion is structural, works at any repository shape with a root-level store.
2. **Subtree strategy** (`dir/**` etc.): a tree OID cannot exclude a nested directory, so exclusion is enforced **by validation instead**: config validation is fatal whenever the store directory lies inside the glob's directory at any depth (including relocation and repository-wide globs pointing at non-root stores).
3. **Enumeration strategy** (`dir/**/*.go` etc. — **only**): exclusion is a **path-prefix filter applied before hashing** — entries whose path is inside the store directory are removed from the entry list prior to hashing. The repository-wide watch is **never enumerated**: `**`, `.`, and `./` always take the synthetic level-1 hash, where exclusion is the name filter of point 1 — exactly one computation per repository-wide scope, never two.

Everything else in the spec that says `.vanguard/` is "always excluded" reduces to one of these three mechanisms; where none applies (e.g. a plain subtree glob containing the store), validation blocks the configuration rather than pretending the exclusion happened.

**The subtree strategy has a hard precondition: the store may not lie inside a watch scope.** A git tree OID hashes the subtree *as a whole* — it cannot exclude a directory nested inside (`services/payments/.vanguard` inside a watched `services/payments/**` recreates the self-referential cycle). Config validation therefore **fails fatal** when the store directory (the configured store root, wherever relocated) sits under any of the target's watch scopes, with hint *"relocate the store, or narrow the watch to a filtered glob"*. Related normalization: `.` / `./` / `**` / `./**` in `paths` all normalize to the **repository-wide watch** form — the synthetic level-1 hash (which excludes `.vanguard/` by construction), never a graph-internal subtree.

`vanguard observe` is the **single lock writer** and the single re-anchoring path. For every document in scope it enforces three preconditions, then records state:

1. **Structure verifies** (the document parses and matches its schema).
2. **The watch scope is clean** — never anchor against uncommitted churn in the watched paths.
3. **The document itself is committed** (git mode): a dirty or uncommitted document version would make the attestation unverifiable — the lock records an `anchor` describing code state while the attested text exists nowhere in history. The check evaluates the document's path — relative to the target root — against the **target** provider: correct when the store lives inside the target repository (root-level or nested). A decoupled store is a recorded v1 limitation — the document path does not exist in the target, so `observe` refuses there (see the ledger); the design intent — commit in *the store's repository*, anchor from the *target's* HEAD — awaits the provider seam that can ask a second repository. A dirty document fails observe with *"commit the document in the repository that contains it, then re-run observe"*.

It then records `content_hash`, `anchor = HEAD` (git mode) or `scope_snapshot` (HashProvider mode), `scope_tree_hash`, `scope_file_count`, and `observed_at`. Observe itself commits nothing — the lock change is committed by the user, and because the lock **is committed like `go.sum`**, every observe is visible and reviewable in git history.

**What `content_hash` is for — document-side drift detection, over the *whole* file.** `check` compares the current document's normalized full-file hash against the lock's `content_hash`. The hash deliberately covers **frontmatter too** (engine metadata included): hashing body-only would let an edit to `watches.paths` — e.g. narrowing the scope to silence staleness — pass unnoticed. A mismatch means the document changed *after* the last attestation — the anchor still describes an older text — so the document is reported **stale** with `document_changed: true` and the hint *"re-run `vanguard observe <doc-id>` after committing the edit"*. This works identically for uncommitted edits (no git timestamp needed) and in HashProvider mode; it closes the mirror of the code-side hole: code churn makes docs stale, and doc churn makes attestations stale.

**Re-attestation without change is allowed — that is the review act.** Observing an unchanged document is a first-class operation: it records the *same* `content_hash` and advances `anchor`/`observed_at`. This is exactly the `review_after` workflow — a human re-reads the document, confirms it is still accurate, and runs `observe` to attest. No cosmetic edit is required or honored: editing a document does **not** clear staleness (only observe does), and observing does **not** require editing.

This makes the workflow explicit at every step: code changes and are committed → `check` reports stale with the culprit commits → the document is updated and committed → `observe` attests the new state → `check` reports fresh. Forgetting to commit the lock is safe: CI sees the older anchor and stays biased toward **stale**.

**Anchor resolution — three paths (squash-merge is the *common* case, not the exception).** The standard flow observes on a feature branch and then squash-merges — so the anchor commit is orphaned **routinely**, and resolution cannot treat that as an edge case. A commit SHA is a function of tree, message, author, timestamp, and parents; `C_branch` never exists on main, and `git fetch --unshallow` cannot help. `check` resolves the anchor along three paths:

```text
anchor commit reachable from HEAD (ancestor)?
├─ YES → fast path: first-parent walk from HEAD back to (not
│         including) the anchor — every commit outside the anchor's
│         reachable set is diffed against its first parent, giving
│         (changed_paths, introducing_commits); staleness still
│         decided by the net-state rule (scope tree hash comparison)
└─ NO  → first-parent recovery: walk the first-parent chain from
          HEAD toward the root, computing the scope tree hash per
          commit (subtree-OID lookup; bounded walk,
          default 1000 commits, `--max-recovery-walk` to change)
          ├─ commit R found with scope hash == lock's value
          │    → R becomes the recovered anchor: attribution runs
          │      on R..HEAD, `anchor_recovered: true` (informational,
          │      disappears after the next observe)
          └─ no match within the bound
               → compare the scope tree hash at HEAD directly:
                 ├─ equal   → FRESH + informational `anchor_orphaned`
                 │            (hint: rebase onto the current main,
                 │             then re-run observe to re-anchor)
                 └─ differs → STALE (attribution unavailable — the
                              churn lives in orphaned history; report
                              scope-level detail only)
```

The fast path's stop condition is the anchor's **reachable set** (every commit reachable from the anchor), not a bare SHA equality: a regular merge whose second parent carries the anchor would otherwise send the first-parent descent from HEAD all the way to the root, attributing the whole mainline history as churn. With the reachable set, the walk stops at the branch point — exactly the `anchor..HEAD` commit set, under the merge rule below. Attribution along the walk is **deterministic across machines and runs**: introducing commits are listed oldest to newest by committer date, and when two commits carry the same committer second the tie breaks by object id (OID) ascending — the same history therefore always yields the same `introducing_commits` list, byte for byte, never a map- or filesystem-order artifact.

"Present in the object database" is deliberately **not** the test — an anchor commit can exist in the ODB (fetched from another branch) without being an ancestor of HEAD, which would make `anchor..HEAD` a non-linear range and attribution wrong. Reachability (ancestry) is the only condition for the fast path.

Because staleness is decided by the **net-state rule** on every path, the fast path and the recovery/degraded paths can never disagree on status — only on how much attribution is available. A shallow clone takes the recovery path (the tree hash needs no history); the pure HEAD comparison is the final fallback.

`scope_tree_hash` — **exact definition.** For a given commit (or worktree state), collect every file matching the watch scope (`.vanguard/` excluded — reduces to one of the three construction points above). Two computation strategies, chosen deterministically by the *exact shape* of each glob — the boundary is **filter-free vs filtered**:

* **Subtree strategy — only for filter-free glob forms** (`dir/**`, `dir/`, `dir` — the glob is *exactly* a directory scope, **no suffix filter**): the hash **is the git tree OID of that directory's subtree**. Git's own Merkle tree already content-addresses every name, mode, and nested subtree beneath it, so the lookup costs O(path depth) — no enumeration. The common case for module watches (`src/payments/**`). The repository-wide case (no `paths` at all) uses the same idea: hash the synthetic list of level-1 entries (excluding `.vanguard/`), O(top-level entries) per check.
* **Enumeration strategy — every glob with a filter** (`src/payments/**/*.go`, `src/*/middleware.go`, any extension or name pattern — including single-level `dir/*`, which is not recursive): enumerate matching entries and hash the sorted list.

**Normalization before classification** — equivalent glob spellings must classify identically: a leading `./` is stripped, and a trailing `/` is stripped, *then* the glob is classified (`./src/a/**`, `src/a/**/`, `src/a/**` → all subtree; `src/a/*` → filtered, because one `*` matches a single level, not recursively). Without this rule, two implementations could compute different hashes for the same declared scope.

**Closed glob syntax (v1).** Declared inputs, so two implementations can never diverge on what a glob *means*: supported tokens are `*`, `**`, `?`, character classes (`[abc]`, `[^abc]`), and **brace alternatives `{a,b}` — a single, self-contained expansion that happens at config-validation time**: the validation pipeline is *expand → normalize each expanded glob → classify → hash*, order fixed. Brace rules, exactly: **one level only** (nested braces `{a,{b,c}}` are a fatal), **no empty alternative** (`{a,}` is a fatal), **alternatives may contain `/`** (`src/{payments,api}/**` — expansion precedes segmentation, so segment math never sees a brace), and character classes inside an alternative are fine (`{[ab],c}`). Also supported: a **backslash escape** (`\[` matches a literal `[`, `\\` a literal backslash) — a trailing lone `\` is a fatal. Dotfiles are matched like any file — **no hidden-file semantics**. After expansion, **consecutive `**` segments collapse** (`src/**/**/x` and `src/**/x` canonicalize to the same glob) and classification + hashing see only the canonical form — two spellings of the same scope hash identically. Rejected at validation (fatal), because each would break the segment reasoning or cross an engine principle: `!` negation, `**` fused into a larger token (`a**b`). Matching runs on **git-recorded byte paths, case-sensitively** in both provider modes — even on case-insensitive filesystems — so the scope state is defined by git's canonical form, not by the local FS quirk.

**Known v1 limitation, recorded deliberately: there is no way to say "everything except a subtree."** `!` negation is banned (glob intersection must stay a declarative pure function for hashing), and `exclude:` was deliberately deferred — so a `src/**` scope cannot exclude generated/vendored code except by narrowing globs. For teams that genuinely need subtraction, this is a real gap and it is **post-v1 candidate `exclude:` — the first scheduled v1.1 feature** (scoped to filtration, evaluated at enumeration time), not hidden: it belongs to the Known-limitations list in the Detailed Implementation Plan.

```text
scope_tree_hash = sha256( concat, for each entry, sorted by path bytes:
    path UTF-8 || 0x00 || mode hex || 0x00 || blob-SHA hex || 0x0A )
```

**Why the boundary is exact — the strategies have different semantics.** A subtree OID covers *everything* under the directory; a filtered glob covers only its matches. `src/payments/**` and `src/payments/**/*.go` must not silently map to the same hash: editing a README inside `src/payments/` churns the first scope and not the second, and that difference is the user's declared intent. **Multi-glob scope — the union is fully specified.** Each glob is hashed independently with its own strategy; the per-glob results are then combined:

```text
scope_tree_hash = sha256( concat, for each glob, sorted by glob string bytes:
    glob UTF-8 || 0x00 || per-glob-hash hex || 0x0A )
```

* Globs are sorted **as written** — where "as written" means the **post-expansion canonical list**: brace expansion happens at validation, so the list being hashed is the expanded globs (normalized, classified, `**`-collapsed), sorted by expanded glob bytes. The original `src/{a,b}/**` text never enters a hash — an implementation that hashed the brace form would diverge, and this rule closes that. A single glob is the degenerate one-element list.
* **Overlaps are not deduplicated**: a file matching two globs contributes to both per-glob hashes. `["src/a/**", "src/a/b/**"]` double-counts the intersection — not "wrong", just a property: deterministic across machines and runs, and discourages redundant globs. The advisory config warning (not an error) flags fully-nested glob pairs **on the post-expansion list** — `src/{a,a/b}/**` warns exactly like the nested pair it expands into.
* The glob string itself is part of each entry (its **canonical post-expansion bytes** — glob text is config, always UTF-8) — renaming or respelling a glob re-keys its hash, so glob edits can never alias a content change.
* Test vectors are mandatory in the Detailed Implementation Plan (nested globs, overlap, rename, single-glob degenerate form).

**Identifier and byte precision, stated openly.** OIDs are stored **in full** in the lock and in JSON output (`anchor` is a whole 40/64-hex object id, as are `introducing_commits[]` entries); abbreviations are a **text-rendering** concern only (bold doc id, dimmed short hash). Paths in hashes are the **raw bytes as git stores them** — git paths are arbitrary bytes, not guaranteed UTF-8; filesystem case-insensitivity and macOS NFD normalization sit *upstream* of git, so vanguard never sees them — the hash sees git's recorded bytes only.

**Form-change and hash-kind consequences, stated openly:**

* Changing a glob's *form* (`dir/**` → `dir/**/*.go`) changes the hash formula — the lock value changes even with unchanged code. Harmless: the glob lives in the frontmatter, so the edit flips `content_hash` too, forcing a re-observe.
* The lock stores the hash **kind-prefixed** — `tree:<oid>` (a filter-free directory scope hashes to the subtree's tree OID), `blob:<oid>` (a single-file scope hashes to that file's blob OID), `sha256:<hex>` (legacy combined-scope digest, scheme v1), or `sha256v2:<hex>` (combined-scope digest, scheme v2 — the composition tags its scheme so the kind survives hashing). `check` compares only like-prefixed values; a *kind* mismatch means the **globs changed or the scope-hash scheme changed** → stale with a re-run-observe hint, never an internal error. A repository migrating object formats changes `tree:`/`blob:` values wholesale — same hint. A binary upgrade that bumps the scheme re-keys every lock — also the same hint, which is what keeps a scheme migration from masquerading as ordinary staleness with a misleading "inspect the commits" attribution.

In both strategies:

* **Paths are part of the hash** — renaming a file, or swapping contents between two paths, changes the hash (blob SHAs alone would silently miss both).
* **Mode is part of the hash** — the executable bit and symlinks (`120000`) participate; submodule gitlinks (`160000`) hash their commit SHA. Mode participates exactly *as git records it*: with `core.filemode=false` (Windows checkouts), git itself freezes the recorded mode, so vanguard's dirty detection ignores mode-only differences — matching git's own behavior, never manufacturing phantom churn.
* **Blob SHAs are git's own** — content-addressed and already canonical; no line-ending concern on this field.
* **An empty scope cannot occur at observe time** — observe refuses a zero-match glob as a config error (see Edge-case resolutions).

The strategy is a pure function of the declared globs, so observe and check always compute the same way for the same document.

**Recovery cost, honestly stated.** During first-parent recovery, each visited commit needs its scope hash: a subtree scope is one OID lookup per commit (cheap — 1000 commits is fine), but an **enumerated scope re-enumerates its matches per commit** — O(walk × matches) per document. That is the price of filter precision; a parent-hash cache (a child's scope hash is derivable from its parent's plus the intervening diff) is the known optimization, deferred to the go-git spike and the Detailed Implementation Plan.

**`scope_file_count` — diagnostic, not a staleness input.** The lock stores how many files the scope contained at anchor time. Net state already catches a scope that shrank to nothing (the hash of an empty entry list differs from any non-empty one); the stored count exists purely to make the *hint* precise: *"scope emptied: 14 files → 0 — the code this document was responsible for moved or vanished"*.

**Time sourcing — `observed_at` is the wall-clock instant of the attestation, in both modes.** The anchor commit's timestamp is deliberately **not** used: a HEAD can be months old when a human reviews today (observe would record a stale time and `review_after: 90d` would re-fire immediately — the review act would be unable to clear the clock), and committer dates are freely forgeable (`GIT_COMMITTER_DATE`), so "stamped by git" was a trust claim the repository does not actually make. Instead: `observed_at = --at <RFC3339>` when given (the deterministic path — CI pipelines and agents inject the CI server's clock instead of trusting the container runner), otherwise the machine's UTC clock. Skew risk is mitigated identically in both modes: the committed lock makes every `observed_at` visible in the PR diff. Two different facts, two different fields: `anchor` carries the *code-state identity*, `observed_at` carries the *human review instant*.

Because the lock travels with the clone, `vanguard check` works **immediately on any fresh clone or CI checkout** — no bootstrap, no warm-up observe; orphaned or shallow-missing anchors degrade to the tree-hash comparison instead of failing. A document that has never been observed is a distinct **`unobserved`** state, handled per provider (see below and Edge-case resolutions).

**The lockfile's identity: the shared attestation record, committed like `go.sum`.** It holds what Git cannot know — *when a human last attested the document's accuracy*: `anchor` (git mode), `scope_snapshot` (HashProvider mode), `observed_at` (the attestation timestamp), `content_hash` (the full-document hash at attestation time, used by `check` to detect document-side drift), `scope_tree_hash` (the watch-scope state at anchor time — the staleness decider), and `scope_file_count` (diagnostic — makes the "scope emptied" hint precise). Test-binding results are transient (reported at execution time, never persisted); `review_after` derives from `observed_at` (not from the document's last-change time), so re-running `observe` is the only way to clear a review-clock staleness. **Entry shape varies with the declared watches — fields exist only when something needs them.** A document with **no `paths` watch** records no `anchor`, no `scope_tree_hash`, and no `scope_file_count` (git mode), and no `scope_snapshot` (HashProvider mode): there is no scope to hash, so nothing is written — a `review_after`-only document (CUJ 3's glossary) has exactly the minimal shape `observed_at` + `content_hash`. `content_hash` and `observed_at` are present in every entry. `check` applies the same sparsity rule in reverse: a document with no lock entry *and* no `paths` watch still cannot be fresh — it is `unobserved` on the review-clock axis as long as it declares `review_after`, or `unwatched` otherwise.

**Lock loss — the exit behavior is two-tier, matching what G3 actually promises.** Configuration damage fails closed unconditionally: an **unparseable or corrupt** lock is a fatal (IO/schema bit 1) on the documents it covers. **Attestation gaps degrade loudly, not fatally, by default**: per-document `unobserved` sets no bit (the bootstrap state must not break every fresh clone's CI), and `--strict` is the escalation. But a **totally degraded** store is a different signature: the lockfile **absent** while `vanguard.yaml` exists and **every** discovered document reporting `unobserved` is what a lost or deleted lock looks like — and in that state `check` sets the **stale bit by default**, so lock loss can never ship green. An unwatched document counts toward the signature: it carries no attestation to lose, so with the lock absent it is unobserved too — the condition is `unobserved + unwatched == every discovered document`. A normal bootstrap (lock committed, some documents not yet observed) does not trigger the rule — only full-unobserved-with-no-lockfile does. **An emptied-but-present lock (`{"docs": {}}`) is deliberately NOT lock-lost**: it is byte-identical to a fresh, valid bootstrap (which `init` writes), so the condition cannot distinguish sabotage from onboarding — the refusal path there is the git history: an emptied lock is a visible diff in a PR (it had entries, now it has none), and restore-from-history covers it. Stated, not hidden: this is the one accepted false-negative of the lost-lock detector — and **`--strict` covers it**: locking an emptied lock into CI with `--strict` flips the resulting all-`unobserved` inventory to bit 8 red, so a team that wants the emptied case caught has a working switch today. The lock stays committed, so a deletion is a visible, reviewable git change, restorable with `git checkout -- .vanguard/vanguard.lock`. The lock is load-bearing in both provider modes.

**The `unobserved` state — one rule, both modes.** A document with no lock entry has no attestation, and vanguard does not invent one. There is no implicit "anchor at discovery time": that phrase has no deterministic meaning (every check would re-derive it, silently keeping the document fresh through a thousand commits — a silent hole found in review). An unobserved document is listed in the **`unobserved`** section on every check — both GitProvider and HashProvider — with the hint *"run `vanguard observe <doc-id>` to record the first attestation"*. It is never reported fresh or stale; staleness computation cannot begin without a baseline. The cross-repository rule stays absolute: a docs-repository commit SHA is never used as an anchor in the target's ODB.

| Field | Meaning | Needed when |
|---|---|---|
| `anchor` | commit at which the document was last attested by `observe`; moves **only** via observe | **GitProvider with a `paths` watch** — fast-path attribution baseline; a clock-only document records no anchor |
| `observed_at` | RFC3339 UTC wall-clock instant of the attestation: `--at <RFC3339>` (deterministic, CI-injectable) or system UTC — deliberately **not** the anchor commit's timestamp (stale HEAD breaks the review act; committer dates are forgeable) | always — the `review_after` baseline and audit marker |
| `scope_tree_hash` | the watch-scope state at anchor time — **kind-prefixed**: `tree:<oid>` for filter-free directory globs (git's own subtree Merkle hash), `blob:<oid>` for a single-file scope (that file's blob OID), `sha256v2:<hex>` for the combined digest over the declared globs (scheme v2 — per-glob results fed glob-sorted into one SHA-256; a `sha256:<hex>` value marks a lock written by scheme v1 and fails loud on kind mismatch); paths and modes included, `.vanguard/` excluded | **GitProvider with a `paths` watch** — the staleness decider on every resolution path. Omitted for `review_after`-only documents (nothing to hash). (HashProvider answers the same question differently: `scope_snapshot`) |
| `scope_file_count` | number of files the scope contained at anchor time | **GitProvider with a `paths` watch** — diagnostic only: makes the "scope emptied" hint precise (14 → 0); staleness itself comes from the hash |
| `content_hash` | sha256 of the **entire document over the normalization pipeline** (BOM strip → LF normalize → strip EOF newlines; frontmatter engine metadata included, body alike) — see Hash normalization | always — document-side drift detection |
| `scope_snapshot` | path→content-hash map of watched files at anchor time | **HashProvider with a `paths` watch only** (no HEAD to diff against); omitted for `review_after`-only documents |

**Structure of `vanguard.lock`.** JSON, schema-versioned, human-inspectable (its presence as a mode indicator: `anchor` implies git mode, `scope_snapshot` implies HashProvider mode — no stored `provider` field; the target comes from config, never from the lock):

```json
{
  "version": 1,
  "docs": {
    "payments-spec": {
      "anchor": "e4f5a6b",
      "observed_at": "2026-11-03T09:12:00Z",
      "scope_tree_hash": "tree:7f8a9b",
      "scope_file_count": 14,
      "content_hash": "sha256:9f2a1c"
    },
    "runbook-ops": {
      "observed_at": "2026-11-02T18:40:11Z",
      "content_hash": "sha256:3b71c0",
      "scope_snapshot": {
        "ops/runbook.txt": "sha256:c01d22"
      }
    },
    "glossary-core": {
      "observed_at": "2026-11-02T18:40:11Z",
      "content_hash": "sha256:5be11c"
    }
  }
}
```

(`payments-spec` is a **git-mode** entry — `anchor` present, no `scope_snapshot`; `runbook-ops` is a **HashProvider** entry — `scope_snapshot` present, no `anchor`; `glossary-core` is the **`review_after`-only** shape — no paths watch declared, so there is no anchor, no scope hash, and no snapshot to store: `observed_at` + `content_hash` is the entire entry. The three shapes are mutually exclusive per document, and a document's mode is implied by which shape it has. There is no `vanguard_version` field: per-observe tool versions would conflict in every PR; the schema `version` field is the only compatibility marker. Field meanings are in the table above; the JSON carries no comments — it is exactly what vanguard writes. **OIDs and hashes are abbreviated in this illustration** (`e4f5a6b`, `tree:7f8a9b`) — the lock stores them **in full** (40/64-hex per the object format); the abbreviation exists only to keep the example readable.)

**Canonical serialization — the lock is diff- and merge-friendly like `go.sum`.** Observe writes the lock in one canonical form: document ids sorted lexicographically, fields in a fixed order, 2-space indent, LF endings, trailing newline, no comments. Two observes of *different* documents therefore touch disjoint lines, so parallel PRs merge cleanly; two observes of the *same* document conflict semantically — the recovery is to re-run `observe` on the merged tree (the attestation must describe the merged state anyway).

**Concurrent observes don't trample — compare-and-swap on content.** The atomic-rename write protects against a *torn* file, not against a lost update: two `observe` processes running in parallel would each load, mutate, and overwrite the other's entry. The mechanism, precisely: observe captures the exact on-disk bytes when it loads the lock (`captured`); before writing it acquires a **CAS token** — an exclusive-create `<lock>.cas` sidecar next to the lockfile (a crashed writer's stale token is **stolen after 10 s**, so a dead process can never wedge the store; a live competitor bounds the wait at 2 s and then fails) — and under that mutual exclusion re-reads the file and **aborts fatal** (`"lock changed during observe - re-run"`, the exact string the code emits) when the bytes differ from its captured snapshot. No retry loop, no merge logic — observes are rare, deliberate attestation acts, and a re-run is the whole recovery. The `.cas` sidecar (and the `.tmp-` write target) are transient engine files next to the lock — never committed, never part of the canonical form.

**Hash normalization — machine-independence despite `autocrlf`.** Every content hash in the lock (`content_hash`, `scope_snapshot` values) is computed over a **fixed normalization pipeline**, applied to text files before hashing:

1. **BOM**: a leading UTF-8 BOM (`U+FEFF`) is stripped — an editor that adds or drops it must not read as churn.
2. **Line endings**: `\r\n` and lone `\r` normalize to `\n` — a Windows checkout with `core.autocrlf=true` and a Linux CI produce identical hashes.
3. **Final newline**: **all trailing LF bytes at EOF are stripped** — whether an editor saved a final newline or not has no bearing on content identity.
4. **Everything else is preserved exactly**: whitespace inside lines, indentation, and **no Unicode canonization (NFC/NFD untouched)** — two texts differing only in canonization form hash differently. That is a *deliberate false-positive direction* (louder, never silent): undetected drift is the thing vanguard exists to prevent, and ghost-churn on canonization is a rare, visible editor artifact. Binary files are excluded from the whole pipeline: a file detected as binary (a NUL byte in the first 8 KiB) is hashed raw, unchanged — mutating its bytes would corrupt the hash. **Order is fixed, first rule of the pipeline: binary detection runs on the raw bytes before any transformation** — no BOM strip, no line-ending fold, nothing. **Intended consequence, stated as such:** a UTF-16 document (whose bytes carry NUL, first 8 KiB) hashes raw — its BOM and line endings are never normalized. Vanguard's supported formats are UTF-8 text; a stray UTF-16 file is content-addressed as opaque bytes — the "loud" behavior, never a guess.

**Hash-normalization test vectors — mandatory in the Detailed Implementation Plan, enumerated here:**

1. `"x"`, `"x\n"`, `"x\n\n\n"` → **same hash** (EOF newlines all stripped)
2. `"x \n"` vs `"x\n"` → **different hash** (trailing spaces are content — markdown hard line breaks)
3. `"x\r\ny"` vs `"x\ny"` → **same hash** (CRLF folds)
4. `"\xEF\xBB\xBFx"` (BOM + "x") vs `"x"` → **same hash** (BOM stripped)
5. NFC vs NFD spellings of the same word → **different hash** (no Unicode canonization — deliberate)
6. a file whose first 8 KiB contain NUL, otherwise text-like → **raw** hash (binary detection first, nothing normalized)
7. a file ending in an LF run that **straddles a chunk boundary** (streamed hashing) → **same hash as vector 1** (pending-LF buffer flushes correctly)
8. a `\r\n` **split across a chunk boundary** → **same hash as vector 3** (the fold sees the pair)

(`scope_tree_hash` itself needs no normalization: it hashes git blob SHAs, which are already content-addressed in canonical form. `scope_snapshot` exists only in HashProvider mode, so per-file hash volume is bounded to non-git targets; git-mode locks stay tiny — one anchor + one tree hash + one count per document.)

Write rules: `vanguard observe` is the only writer; it rewrites the file atomically (write temp + rename) so an interrupted observe can never leave a corrupt lock. Unknown fields are **preserved, not dropped** (forward compatibility across minor versions); an unknown `version` major fails closed with an upgrade hint. A missing `scope_snapshot` in git mode is normal, not an error.

`check` **never writes anything, ever** — it reads `vanguard.lock`, computes, and reports. The explicit `--run-bindings` flag adds execution but still writes zero bytes. Only `vanguard observe` updates the lock. This separation prevents, for example, an AI agent's read-only `check --path` call from silently re-anchoring a document — re-anchoring is always a deliberate, git-history-visible act.

Uncommitted churn can never be silenced by editing the document — there is no code version to advance to. The commit is the synchronization point.

**Dirty detection and blob reads — the promise, made precise.** `dirty_condition` = "worktree differs from HEAD inside the watch glob". Operationally: the worktree status pass flags non-clean paths in the scope as candidates; a candidate is confirmed dirty by comparing worktree bytes (over the Hash normalization pipeline) against the **HEAD blob's content** (same pipeline). That means dirty detection *does* read blob contents — but **only for scoped candidate paths, never for history, attribution, or the revwalk**; the "reads no blobs" promise is scoped exactly: the freshness hot path (net state, anchors, recovery) works on commit graph + tree metadata, and blob content is touched solely to verify currently-present worktree files. **A blobless clone that lacks the blob makes the comparison impossible — never silently**: the file is reported **dirty** with the hint *"this clone does not hold the code blob for <path> — confirm in a full checkout"* (louder, never silent; no network fetch happens — G4 offline-by-default holds).

**What ships today — the recipe, as implemented.** The two-phase recipe above **ships**: go-git's worktree status supplies the candidate set only (its raw comparison would report phantom churn under `core.autocrlf` renormalization); every candidate is then **confirmed** by comparing the worktree bytes against the HEAD blob over the Hash normalization pipeline — CRLF folds to LF on both sides, so the phantoms never survive. Untracked-not-ignored files have no HEAD blob and are dirty outright; ignored files never appear (go-git delegates to git's ignore rules). The engine spawns no process for any of this — the git CLI is nowhere in the build (D1 holds unconditionally). A blobless clone that lacks a candidate's blob reports the file **dirty with the hint** above (loud, never silent) — in check mode the warning rides on the document's `hints[]`; in observe it surfaces in the store-level `warnings[]` (where a dirty scope refuses the attestation).

**Honest limit — attestation is only as good as the attester.** With the stored anchor, staleness clears **only** via an explicit `observe` run — a cosmetic doc commit alone no longer silences it (the derived-anchor hole is closed). The residual hole: a maintainer or agent runs `observe` without actually reviewing the content. Vanguard cannot judge semantic accuracy — that is content semantics, excluded by NG3 — and review trailers / frontmatter review stamps stay rejected (commit noise, mechanically gameable). Likewise the `--all` guard: it reduces **accident** risk (an agent sweeping the unobserved set), not **abuse** — an agent with shell access can still run `observe <id>` per document unhindered; the durable protection is procedural: every attestation is a reviewable lock diff in a PR. What vanguard offers: the attestation is **visible in git history** (the committed lock changes on every observe, reviewable post-hoc like any file); structure verification still applies to every edit; PR reviewers see doc churn and code churn side by side; and `check --run-bindings` provides proof-grade verification for teams that want it.

**Watch scope semantics.** Staleness is the comparison of the **scope state** (tree hash) between the anchor and HEAD — never a count of churn events:

- A watch **with `paths`** compares the scope's tree hash at HEAD against the value recorded at anchor time: a document anchored at an old commit is *not* stale if the watched paths' net state is unchanged — even if commits between the anchor and HEAD touched them and reverted (A→B→A is fresh). Intermediate churn is visible through attribution (`introducing_commits`, `changed_paths`), never through status.
- A watch declared **without `paths`** declares **no path scope**: the paths axis never fires, and the document ages only by `review_after` (if declared) or sits in `unwatched` (if nothing is declared at all). An implicit repository-wide scope was rejected after review — read literally, it would make a `review_after`-only document stale on *any* repository change (directly contradicting CUJ 3 and the `check --path` rule). A document that genuinely tracks the whole codebase declares it **explicitly**: `paths: ["**"]`.
- A document with **no watch at all** is never stale *by watch* and is listed under `unwatched` — distinct from a declared watch with empty scope. **Precision: `document_changed` applies to every observed document, watched or not.** An unwatched document that has been observed carries a `content_hash` baseline; editing it afterwards reports `document_changed: true` (stale, document axis) exactly like any other. The `unwatched` label describes the **absence of watches** — watch-driven staleness — not the absence of drift detection.

- **`review_after`** fires when `now − observed_at` exceeds the declared duration. Grammar of the duration: an integer plus **one unit** — `h` (hours), `d` (days, the default unit for a bare integer), `w` (weeks = 7d), `mo` (months = 30d), `y` (years = 365d); months and years are fixed-length conversions (no calendar precision — determinism beats calendar exactness), and anything else (bare seconds, floats, multiple units) is a config error. The baseline is the **attestation time** (the lock's `observed_at`), not the document's last-change time — so the only way to clear a `review_after` staleness is to re-run `observe` (the review act). A cosmetic edit does not reset the clock; an unobserved document has no `observed_at` and is reported `unobserved` instead.
- **No `.git` present**: the GitProvider is replaced by the HashProvider. Without git there is no HEAD and therefore **no dirty status** — statuses are `fresh`, `stale`, or **`unobserved`** (no baseline recorded yet; informational section with an `observe` hint, never guessed). Freshness is computed by comparing current content hashes of scoped files against the `scope_snapshot` recorded at anchor time. Attribution and `--from/--to` modes are unavailable and reported as such.

### Tests bindings — opt-in execution, transient results, never staleness

A document may reference test bindings (top-level `bindings:` field, by name — deliberately **not** under `watches`, because bindings execute, they do not watch). Tests are an **on-demand verification** option, not a staleness source — commit/path watches remain the only staleness mechanism:

- Binding commands live in `vanguard.yaml` under a `bindings:` allowlist; documents reference them **by name only**. Raw commands in document frontmatter are rejected — any change to the allowlist is a visible, reviewable config change (trust model, see Non-Functional Requirements).
- `vanguard check` (default) **never executes** anything; referenced bindings are reported as `bindings: [{name, status: "pending"}]` — informational, not a status.
- `vanguard check --run-bindings` executes the allowlisted commands for the documents in scope, at the current code state, and reports results **immediately**:
  - **pass** → `binding: pass` reported beside the status: direct proof that the described behavior still holds, verified by real execution against current code. It does not silently flip a stale status to fresh — both signals stay visible.
  - **fail** → `binding_failed` (exit bit 16) with the failing command, its exit code, and parsed diagnostics. Never the stale bit — a broken test or a style error is not "document no longer matches code".
- **Nothing is persisted.** Like worktree status, a binding result is true at the moment of execution and is never written to `vanguard.lock`; the next `check` without the flag reports the bindings as `pending` again. Re-running is the only way to re-prove.
- Determinism caveat: test flakiness breaks same-input-same-output for this option only. That is the declarer's trade-off; commit/path staleness stays fully deterministic.

**Binding execution contract** (every binding runs under exactly these rules — no shell interpolation, ever):

| Concern | Rule |
|---|---|
| Process | Spawned via **direct argv exec** (`exec.Command` with an argument array) — never through a shell; no interpolation, no injection surface. Commands are declared as **YAML arrays** (`cmd: [go, test, ./payments/...]`); a single-string shorthand (`cmd: "go test ./payments/..."`) is split by the POSIX shlex rules (whitespace separates; single/double quotes group, embedded whitespace preserved) — the split is pure text parsing, never shell evaluation |
| Working directory | The **target root** by default (bindings usually test code); `cwd: store` per binding opts into the store's location |
| Timeout | Default **600s** per binding, overridable per binding via `timeout:`; on timeout the process tree is killed via the platform mechanism (process group on POSIX, Job Object on Windows) and the binding reports `binding_failed` (timeout) |
| Environment | Parent environment is passed through, plus injected `VANGUARD_TARGET`, `VANGUARD_DOC`, `VANGUARD_STORE`; secrets stay the CI config's responsibility (vanguard adds nothing, scrubs nothing — what the environment contains is the runner's policy) |
| Binary resolution | The **ambient PATH** (`.vanguard/bin/` provisioning is **deferred post-v1** — v1 has no shim directory and no checksum machinery; see Tool provisioning). A command that resolves to nothing fails the binding (`binding_failed`, exit code −1) without executing anything |

### Tool provisioning — deferred post-v1

Bindings reference external verification tools (Vale, vacuum, structurizr-CLI). Provisioning them is an **environment-bootstrapping** concern, deliberately separated from the check engine — and **deferred post-v1** (D10, per the YAGNI audit): v1 ships no `vanguard install`, no `.vanguard/bin/` management, and no checksum machinery.

* **v1 posture**: bindings resolve their commands from the **ambient PATH** — the same trust class as running the project's test suite. Teams pin tools in CI (container images, setup steps); the pin lives in the team's own pipeline, not in vanguard.
* **The recorded post-v1 shape** (kept so the deferral does not lose the design): a pinned `url` + `sha256` fetch-and-verify placing binaries in `.vanguard/bin/` (project-isolated), every provisioner call behind an internal `Provisioner` interface, committed binaries executed only after their `sha256` matches the config declaration, and the aqua registry integration as a later `Provisioner` adapter — **not** a package-manager engine, and never on the `check` hot path.
* **System prerequisites**: complex runtime-dependent tools (e.g. structurizr-CLI needing Java) are documented prerequisites — the user maps the tool path in the binding.

### Edge-case resolutions

| Question | Resolution |
|---|---|
| Untracked and gitignored files in dirty detection | **Git mode** follows git's own classification: a file inside the watch scope that is **untracked but not ignored** is churn — it sets the dirty bit and blocks observe's scope-clean precondition (real, unrecorded content exists). A file matched by **`.gitignore` is never counted** — vanguard delegates the ignore question to git's rules and never re-implements them; ignored files are invisible to every watch computation. **HashProvider mode has no ignore concept** (no git to ask): every file matching the declared globs belongs to the scope, ignored or not — teams needing exclusions narrow the globs themselves. A per-watch `exclude:` in config is the **first scheduled v1.1 feature**, deliberately not in v1 to keep the hash scope meaning simple: *declared globs are the entire truth* |
| "Worktree clean" scope | Evaluated **only within the union of the document's watch scope** (`.vanguard/` excluded per the three construction points) — an unrelated dirty file never blocks re-anchoring. The **document itself** must also be committed, evaluated against the **target** provider (see the observe preconditions — a decoupled store's own repository is a recorded v1 limitation, not silently skipped; a store nested inside the target is resolved by its full relative path). **Target not a git repository** (HashProvider mode): the committed-document precondition is **skipped** (there is no commit to require — attestation durability is the team's process there) |
| **Submodule inside a watch scope** | The submodule contributes exactly **as git records it**: the gitlink (`160000`) hashes the recorded submodule commit SHA, so a submodule pointer bump churns the scope; content changes *inside* the submodule belong to the submodule's own repository — they never register in the parent's worktree status (matching git's semantics). Dirty detection evaluates the parent worktree only |
| Blobless clone + dirty detection + `observe` | The loud-dirty clause has a **downstream consequence, stated for blobless users**: a candidate file whose blob is missing reports dirty → the scope is not clean → **`observe` refuses that document**. In practice this is narrow — only files whose worktree content differs from the index (candidates), and a blob is missing only for never-checked-out or filtered paths — but a blobless clone **cannot bootstrap a document whose scope contains such a file**; the hint says *"confirm in a full checkout"* (or narrow the scope). Observe never fetches blobs (G4 offline) |
| Index oddities: sparse-checkout, `assume-unchanged`, `skip-worktree`, racy-git | **Spike items, pre-committed**: (1) an `assume-unchanged`/`skip-worktree` file is treated as **not in the worktree** (never a dirty candidate — git itself promised it clean); a **sparse-checkout** path that is not materialized is likewise no-candidate; (2) **racy-git** (index written in the same second as the file) can make stat-equality lie — the standard mitigation (re-hash files whose mtime ≥ the index's racy threshold) is evaluated in the spike; if go-git does not expose it, dirty detection re-hashes all scope candidates below the racy threshold |
| **Symlinks** | Participate exactly as git records them: a symlink is a blob of its target path text with mode `120000` — both participate in the entry hash. A retargeted or dangling symlink churns when its **recorded text** changes; what the link points *at* on disk is never followed by scope hashing (no dereferencing at all — the hash sees git's bytes) |
| **Linked worktree / `.git` as a file** | Supported: the provider's ODB discovery follows the gitdir pointer inside the `.git` **file** (linked worktrees, submodules). Same-store-different-checkout scenarios work because all state is committed (anchors, hashes) — the lock never records worktree-local data. Detailed behavior is a spike item in Open Questions |
| HashProvider anchoring rule | No HEAD, no clean concept, no commit anchor: the anchor is the stored `scope_snapshot` (path→content-hash map recorded by `observe`); a document is stale whenever any scoped file's current hash differs from the snapshot. Statuses: `fresh`/`stale`/`unobserved` (no baseline yet) |
| `check --path` structure phase | Phase-1 structure verification is **limited to documents in the result set** — an unrelated document's schema error does not set the fatal bit for a targeted agent query |
| `check --path` and a document with no `paths` watch | A `review_after`-only document has **no path scope to intersect** — `check --path` never matches it; it surfaces in full-document checks (and `check <doc>`) only. Path reverse lookup is a code-path question; a clock-only document answers to the clock, not to paths |
| Path argument is a directory (`--path src/payments`) | A path argument **P** matches a document iff **some file under P (P included if it is a file) could match one of the document's watch globs**. So `--path src/payments` matches both narrower globs (`src/payments/api/**`) and broader ones (`src/**`): the question is "which documents claim any territory under here", not "which globs equal this path". A file argument matches only globs that match that exact file |
| `unwatched` vs `unobserved` classification | The categories are mutually exclusive and decided in order: a document with **no watches** is `unwatched` (no *watch-driven* staleness exists; document-side drift still applies after an observe — see `document_changed`). Observe on an unwatched doc is **allowed and useful-lite**: it records `content_hash` + `observed_at`, so document-side drift detection and a review-clock baseline exist; there is simply no watch axis to fire. A document **with declared watches but no lock entry** is `unobserved` (it has a staleness clock but no baseline). `check --path` returns matching documents that are **stale or unobserved** — a fresh match is omitted, an unobserved match is included (it is exactly the gap an agent must see before editing) |
| Observe produces a second commit per review | **Acknowledged friction**: commit the doc, run `observe`, commit the lock — two commits per attestation. The lock commit is the reviewable attestation artifact, so v1 keeps it explicit; teams may fold both commits into one PR. Anything that hides the lock change (auto-commit, `--amend`) is rejected: it would make attestations invisible in history |
| `vanguard observe --all` — partial failures | **Per-document independence**: `--all` walks the `unobserved` set; each document is attested or **refused individually**, and the exit code reflects refusals **differentiated by kind**: a document refused for a **structure/schema error, an unreadable document, or IO failure** sets the fatal bit (`1`); a document refused because its **watch scope is dirty** sets the dirty bit (`4`) — that refusal is exactly the churn condition, not an engine failure. No all-or-nothing rollback: a partial bootstrap is still progress, and the refused documents stay visible in `unobserved` for a follow-up |
| Document edited after last observe (content_hash mismatch) | Reported **stale** with `document_changed: true` and hint *"re-run `vanguard observe <doc-id>` after committing the edit"* — the anchor still describes an older text. Works for uncommitted edits and in HashProvider mode alike; no mtime fallback exists or is needed |
| Re-running `observe` on an unchanged document | **Allowed and first-class** — the review act: same `content_hash`, new `anchor`/`observed_at`. This is the only way to clear a `review_after` staleness; no cosmetic edit is required, and an edit alone clears nothing |
| Time-dependence of `review_after` vs G2 | Time is an explicit input: `--now <RFC3339>` overrides the wall clock, keeping `review_after` tests deterministic |
| Clock source for `observed_at` | **`--at <RFC3339>`** (CI/agent injects the server clock) or system UTC — in **both** modes. The anchor commit's `Committer.When` is rejected: a months-old HEAD would make the review act unable to clear `review_after`, and committer dates are forgeable (`GIT_COMMITTER_DATE`). The committed lock makes skew visible in PR review. **Future-time guard**: `observed_at` more than the configured **`clock_skew_tolerance`** (default 5m — configurable, because CI runners drift more than that) in the future of `--now` is an integrity violation — the document reports **fatal** with hint *"re-run `observe --at` with the correct clock"* (a future attestation would make `review_after` un-fireable forever). Like the structure phase, the guard applies **only to documents in the result set** — one corrupt entry cannot fatal an unrelated path query. **Monotonicity**: observe refuses any candidate `observed_at` earlier than the document's existing `observed_at` — an attestation cannot un-happen, **regardless of the source** (explicit `--at` or system clock). The refusal carries the hint *"pass a later `--at`, or resolve the clock source"* — the merged-lock case (two branches observed the same doc, skewed clocks) recovers exactly this way. **`--now` is check-only; `observe` validates against system UTC at run time** (there is deliberately no `--now` in observe): the future-time guard and monotonicity both use the system clock as their reference, and `--at` is the CI's correction channel when skew is known |
| Anchor commit missing from history (squash-merge, rebase, cherry-pick, shallow clone) | **Three-path resolution, never a hard failure**: (1) fast path — anchor **reachable from HEAD**; (2) **first-parent recovery** — walk the first-parent chain from HEAD (bounded, default 1000 commits) until a commit's scope tree hash equals the lock's → recovered anchor, full attribution, informational `anchor_recovered`; (3) fallback — compare the scope tree hash at HEAD: equal → fresh + `anchor_orphaned` (hint: **rebase onto the current main, then** re-run `observe`), differs → stale with scope-level detail only. Status is decided by the net-state rule on every path, so paths cannot disagree. Until the interleaving open question (merge queue vs post-merge observe) is resolved, hot scopes may go stale immediately after every squash-merge — the hints steer toward re-observing on rebased history |
| role of `vanguard.lock` | The **shared attestation record, committed like `go.sum`** — anchors and HashProvider baselines travel with every clone. Deleting it resets every document to `unobserved` — with the **two-tier exit rule**: per-document gaps stay informational unless `--strict`, but a **lost lock** (lockfile absent + every document unobserved) sets the stale bit **by default**, and a corrupt lock is fatal — nothing is silently fresh either way; and the lock is committed, so deletion is a visible git change restorable with `git checkout -- .vanguard/vanguard.lock` |
| Unobserved document (no lock entry) | **One rule, both modes**: listed in the `unobserved` section on every check, hint *"run `vanguard observe <doc-id>`"*, never reported fresh or stale. There is **no** implicit "anchor at discovery time" — that phrase re-derives on every check and would keep an unobserved doc silently fresh through a thousand commits. A docs-repository commit SHA is never used as an anchor in the target's ODB (it cannot resolve) |
| Why the anchor **is** stored (reversing the earlier derive-only rule) | The original objection to storage was a gitignored lock — absent exactly on fresh clones, defeating CI. With the lock **committed**, the stored anchor travels with every clone and CI works with no bootstrap. Storage additionally makes re-anchoring an explicit, git-history-visible attestation (closing the cosmetic-commit hole). Drift risk (rebase/amend/squash orphaning the anchor commit) is handled by the three-path resolution — first-parent recovery, then the net-state tree-hash comparison; never silently mis-reported |
| Why binding proofs are **not committed to git** | A test run is not a git event — no commit can record it, and sharing proofs via committed files or git notes adds commit noise and conflicts. Proofs are **ephemeral by design and per-environment**: a proof from another machine describes a different worktree/code state, so it is untrustworthy; CI must run `check --run-bindings` at its own HEAD to produce its own proof. Without the flag, bindings surface as informational `pending` entries, never as staleness |
| Cosmetic commit clears staleness | **Closed by the stored anchor**: a doc commit alone does not move the anchor — only `observe` re-attests. Residual hole: observe run without real review (visible in the lock's git history); see the honest-limit note in Anchoring |
| Files in `.vanguard/` outside `docs/` | **Never documents.** The document root is fixed at `.vanguard/docs/` — `vanguard.yaml`, `schemas/`, `vanguard.lock`, `bin/` are engine-owned and excluded from discovery even when their format would match. Discovery globs resolve relative to `docs/` and cannot escape it (`../` rejected). Watch globs match code paths in the target repository only |
| `bindings` trust model | Bindings are declared **only in `vanguard.yaml`** (an allowlist reviewed like any config change); documents reference them by name in a top-level `bindings:` field. Raw commands in document frontmatter are rejected — a PR cannot smuggle an executable command through a document. **Fork-PR warning**: a PR from an untrusted fork can modify `vanguard.yaml` to run arbitrary commands inside CI's environment (secrets included) — the same trust class as running the project's test suite. Never enable `--run-bindings` on untrusted PRs. **Minimal mechanism, in v1**: every `--run-bindings` execution prints a provenance line — *"bindings from vanguard.yaml @ <HEAD short sha of the repository that contains the store> (<n> commands)"*, or *"bindings from vanguard.yaml @ no-git (<n> commands)"* when the store sits in a non-git directory — so a CI log shows exactly which allowlist revision executed, in any placement. A deeper mechanism (config-diff vs a base ref, allowlist digest in the lock) is recorded as a post-v1 open question; the guard reduces accident risk, not abuse (same honest limit as `--all`) |
| Watch glob matches zero files at check time | **Distinguish config error from drift**: at `observe` time, a watch glob matching zero files is a fatal config error (refuses to attest an empty scope — likely a typo). At `check` time, a scope that shrank to nothing is **stale** by the net-state rule (the hash of an empty entry list differs from the recorded one); the stored `scope_file_count` makes the hint precise: *"scope emptied: 14 → 0 — the code this document was responsible for moved or vanished; update the watch paths or re-anchor"*. Never fatal at check: that would conflate configuration with staleness |
| Lock entry shape vs current target mode | The provider is a property of the **target**, but each lock entry's shape (`anchor` vs `scope_snapshot`) records the provider's mode at observe time. If the target flips — a plain directory gains a `.git`, or a git repository loses one — previously written entries no longer match. Rule: a **shape mismatch is not an internal error**; entry shapes that don't fit the current mode are reclassified as `unobserved` with the hint *"target mode changed — run `observe` to record baselines in the current mode"*. The stale data is simply ignored, never mis-read |
| Document id renamed or colliding | `id` collisions between two discovered documents are **fatal at discovery** (ids are the unique namespace). A rename leaves the old lock entry orphaned; **observe prunes nothing by default** — orphaned entries are inert data that stay in the lock. Reclaiming them is an explicit **`vanguard observe --prune`** — **a standalone invocation, not a modifier**: it runs the gc pass alone and exits accordingly; combining `--prune` with `<doc-id>` or `--all` is a usage error (keep `observe` single-purpose, whether it attests or it collects). Prune drops entries whose id no longer exists in discovery, as dedicated lines in the lock diff (kept out of default observe so per-document attestations touch only their own entry — the merge-clean property holds). Branch merges: canonical sorted serialization keeps the lock line-clean; the only semantic conflict is two branches observing the *same* id, resolved by re-running `observe` on the merged tree. Existing orphans flagged as an advisory warning on every check |
| Discovery glob case-sensitivity | **Same rule as watch globs: case-sensitive**, evaluated against git-recorded byte paths in store-repository mode. On a case-insensitive filesystem (Windows/macOS) only one of `Spec.md`/`spec.md` physically exists — the working tree is what vanguard verifies, so only the existing name is discovered (the sibling existing only in git history is invisible, correctly). Discovery never re-implements platform collation |
| HEAD is unborn (repository with zero commits), detached, or mid-merge/mid-rebase | **Unborn**: there is no HEAD, so nothing can be anchored — `observe` refuses (refusal reason `document not committed`-family, bit 1) with hint *"make the first commit, then observe"*; `check` on an unborn repository has no lock entries possible and reports everything `unobserved`. **Detached HEAD**: allowed — it is a real commit; `observe` anchors to it and a store-level warning *"HEAD detached — anchor describes this commit, re-attest on the intended branch"* appears in `warnings[]`. **Merge/rebase in progress** (the `MERGE_HEAD` / `rebase-merge` / `rebase-apply` markers exist): `observe` refuses the whole run (refusal reason `merge or rebase in progress`, bit 1) with hint *"finalize the merge/rebase, then observe"* — a half-finished merge is not a stable code state to attest |
| Config `version` field in `vanguard.yaml` | **Missing** → assumed `1`, with an advisory hint *"add `version: 1` to vanguard.yaml"* (a fresh scaffold is pre-filled, so absence means hand-editing). **Unknown major** → fatal with an upgrade hint, matching the lock's version rule. Minor compatibility within major v1 is additive by definition |
| Provisioned-binary checksums vs the PR author | **Deferred with the feature**: the pinned-`url`+`sha256` trust chain (catching out-of-band tampering, bit-rot, and a swapped committed file — but not a PR author who changes both) belongs to `vanguard install`, which is **deferred post-v1**. In v1 the ambient-PATH trust model applies — the same trust any repository extends to running its test suite; the fork-PR rule **above** (bindings trust model) is the boundary that matters |
| Hash-mode repository-wide watch | `scope_snapshot` enumerates per-file hashes — a repository-wide watch in HashProvider mode can bloat the lock. **Hashing is streamed** (each file is copied into the hasher in bounded chunks — constant memory, no whole-file reads) with **one subtlety stated**: the EOF-newline rule needs a small **pending-LF buffer** — trailing `\n` bytes are held back and only forwarded when the next non-LF byte (or EOF) arrives, so "strip all trailing LF" survives chunk boundaries; binary detection reads the first chunk before anything else. Observe emits an advisory warning when a hash-mode scope exceeds **10,000 files** and suggests narrowing the watch. A **hash-of-hashes compaction** (attributions fold into one digest — trade away the per-file difference list, which HashProvider never had) is a recorded post-v1 optimization for this case. Git mode is unaffected (one tree hash per document). **Chunk-boundary test vectors**: 7–8, listed with the Hash normalization suite above — one source of truth for the DIP |

### Target resolution and providers — one store, many targets

`.vanguard/` does not have to sit inside the watched repository. The target is resolved in priority order: `target.path` in `vanguard.yaml` (relative paths resolve against the store's location, absolute allowed) → the store's containing directory.

```yaml
# .vanguard/vanguard.yaml — store lives in a separate repository
version: 1
target:
  path: ../backend-payments        # local sibling repository, or:
  # provider: github-api           # remote, zero-clone (opt-in network mode)
  # endpoint: "https://api.github.com/repos/org/payment-service"
  # auth_token_env: "GITHUB_TOKEN"
```

```text
/workspace/
├── architecture-specs/          <-- vanguard store (its own repository)
│   └── .vanguard/{vanguard.yaml, vanguard.lock, docs/}
└── backend-payments/            <-- target repository (.git) — state stays in the store
    └── src/payments/
```

All watch globs resolve relative to the **target root**, never the store's location. Three interchangeable providers sit behind one interface:

| Provider | When | Mechanism | Network |
|---|---|---|---|
| **GitProvider** (default) | target has a local `.git` | go-git ODB: resolve refs, first-parent walk, tree diff, worktree status (dirty candidates). Also works on **blobless/partial clones** (`clone --filter=blob:none`): the freshness hot path needs only commit graph + tree metadata — changed *paths*, never blob contents. **For dirty detection the story is sharper**: a candidate path whose blob is missing is reported **dirty with a hint** (loud, scoped rule — see "Dirty detection and blob reads"); staleness itself needs no blobs ever. (This is why the `refs` text-search watch was removed: reading blob contents everywhere would break blobless clones wholesale) | none |
| **HashProvider** | no `.git` (or target is a plain directory) | content hashing + `scope_snapshot` in the lockfile; no attribution, no `--from/--to` | none |
| **RemoteGitProvider** (opt-in, post-v1) | git protocol forbidden; only a hosted API token exists | provider API (`github-api`, `gitlab-api`, custom): latest commit + `compare/{base}...{head}` returning `changed_files[]`, matched against watch globs. Zero-disk: not a single byte of code is cloned | **yes — only when explicitly configured** |

Vanguard v1 ships GitProvider and HashProvider; RemoteGitProvider is a defined extension point behind the same interface.

### Check command surface

| Invocation | Behavior |
|---|---|
| `vanguard init` | scaffold `.vanguard/` (config skeleton, empty schemas, **`vanguard.lock` containing exactly `{"version": 1, "docs": {}}`** — an *existing, well-formed* lockfile, so the lost-lock condition can never fire on a fresh bootstrap) — nothing else: no hooks, no `.gitignore` edits (the lock is committed). **Bootstrap guidance is in the init output**: *"commit `.vanguard/` immediately — the lock is the shared attestation record; un-committed, a fresh clone reports lock-lost"* |
| `vanguard observe <doc-id>` | the only lockfile-mutating command and the only re-anchoring path. **The document id is required** — bare `observe` is a usage error (mass-attestation is exactly the "unreviewed attestation" hole). **`--all` is a guarded bootstrap hatch**: it attests **only documents currently in the `unobserved` section** — never stale ones; a stale document must be attested individually, after its reviewer has actually looked at what made it stale. Preconditions per document: structure verifies, the **watch scope is clean**, and the **document itself is committed** in the repository that contains it. Then records `content_hash` + `anchor = HEAD` (git) or `scope_snapshot` (HashProvider) + `scope_tree_hash` + `scope_file_count` + `observed_at` (`--at <RFC3339>` or system UTC) into `vanguard.lock`, and prints an observation report to stdout. Re-observing an unchanged document is allowed (the review act). Observe commits nothing — the lock change is committed by the user, making every attestation reviewable in git history |
| `vanguard check` | two-phase, read-only: **phase 1** structure verification of every discovered document against its declared schema profile (fail-closed, sets the fatal bit); **phase 2** freshness computation for all documents, plus the `unwatched` and `unobserved` sections and the store-level advisory `warnings[]` |
| `vanguard check <doc-id>` | single document: structure verification + freshness; the argument is an exact id (see the resolution rule above) |
| `vanguard check --path <path>` | reverse lookup: documents whose watch globs match the path. Returns matching documents that are **stale or `unobserved`** — both are actionable for an agent; fully fresh documents are omitted. Structure verification is limited to the result set. Relative paths resolve against the **target root**; an absolute path is accepted only if it resolves **inside the target root** (else usage error); `..` segments are rejected (same rule as discovery globs) |
| `vanguard check --from <ref> [--to <ref>]` | **Path-Hit Attribution, lockfile-free**: reads the changed-file list between the refs from the embedded git object database, matches each discovered document's watch globs against that list, and reports documents as `affected_in_range` with the per-document introducing commits. Fully independent of `vanguard.lock` — no anchor state is read or written. **`--to` defaults to `HEAD` when absent**; refs resolve in the target repository and an annotated tag is **peeled to its commit** before any walk; an unresolvable ref is a usage error with the hint. Exit code: **0 by default** — the range report is informational, because `affected ≠ stale` (a document attested *after* the range still appears). The `--fail-on-affected` flag promotes a non-empty `affected_in_range` to a **dedicated exit bit (32)**, so CI can gate on range churn without conflating it with staleness; the dirty and gap bits (`4`, `8`) do not apply in this mode. **Semantics note:** `affected_in_range` means "churn touched this document's scope in the range" — the mode answers "what churned here", not "what is stale" |
| `--run-bindings` | check-only flag: execute the allowlisted bindings for the documents in scope. **pass** → `binding: pass` proof beside the status; **fail** → `binding_failed` (bit 16), never stale. Results are transient — never persisted, never run without this flag |
| `--now <RFC3339>` | check-only: override the wall clock for `review_after` (determinism); `observe` always validates `observed_at` against the system clock |
| `--strict` | check-only: elevate informational gap categories (`unwatched`, `unobserved`) to exit bit 8; default runs exit 0 with the gaps listed. **A flag inapplicable to the selected mode is a usage error (fatal, G8 hint), never silently ignored** — specifically `--strict` combined with `--from/--to` is rejected (gap categories are meaningless in range mode), and so is `--run-bindings` (bindings never run in range mode) |
| `--max-recovery-walk <n>` | check-only: bound for the first-parent recovery walk (default 1000 commits) |
| `--fail-on-affected` | `check --from/--to` only: sets exit bit `32` when `affected_in_range` is non-empty. Without the flag the range mode always exits `0` (informational report) — `affected` is never conflated with `stale` |
| `--at <RFC3339>` | observe-only: explicit attestation timestamp (deterministic path for CI/agents) |
| `--format text&#124;pretty&#124;json&#124;yaml` | output format for every mode (default: `text`; `pretty` is the terminal-first styled rendering — see Output formats) |
| `--output <file>` | check-only: write the result to a file instead of stdout |
| `--no-color` | disable ANSI styling in the `pretty` format (also disabled automatically when piped, or when `NO_COLOR` is set — the no-color side of the `CLICOLOR_FORCE` escape hatch) |

**Defaults and fixed constants — every tunable and constant in the engine, in one table:**

| Setting (tunable) | Default | Where it binds |
|---|---|---|
| `--max-recovery-walk <n>` | `1000` commits | first-parent recovery walk bound |
| per-binding `timeout:` | `600s` | binding execution; process-tree killed on overrun |
| `clock_skew_tolerance` | `5m` | future-`observed_at` integrity guard |
| hash-mode snapshot warning | `10,000` files | advisory when a HashProvider scope exceeds it (suggest narrowing) |

| Constant (fixed by design) | Value | Where it binds |
|---|---|---|
| frontmatter size cap | `256 KiB` | structure parse (larger → fatal, hint: split the document) |
| `max_depth` validation cap | declared heading depth ≤ `32` | structure-schema validation |
| lock canonical form | ids sorted, fixed field order, 2-space, LF, trailing newline | every observe write |
| binary detection window | first `8 KiB` | hash normalization pipeline |
| scope-empty at observe | fatal config error | zero-file glob refusal |
| OID rendering | full in machine formats; abbreviated in `text` only | output contract |
| envelope `schema` | `1` | output contract version marker |

### The `check --path` decision procedure (exact)

"`some file under P could match glob G`" is a **decision procedure on glob shape**, not a filesystem walk — it answers *which documents claim territory under here*, deliberately independent of what currently exists on disk (`--path src/payments` matches `src/payments/api/**` even while `api/` is empty, because the document claims that territory). For a **file argument**, the decision is the direct glob match. For a **directory argument P** against glob G, both split into path segments; the question is whether the *future set* of paths with prefix P intersects G's match language. Memoized segment reasoning decides it:

```text
decide(gi, pj):                       # gi = G's remaining segments, pj = P's
  if gi is empty:  return false       # G exhausted, P continues → no recursion
  if pj is empty:  return true        # P consumed, G can still name a deeper file
  if gi[0] = "**": return true        # `**` absorbs any run of P's segments
  g := gi[0]
  if p := pj[0] matches g (literal equal, or `*`/`?`/class match)):
                      return decide(gi[1:], pj[1:])
  else:               return false
match(P, G) = decide(split(G), split(P))
```

Termination is structural (each recursion consumes at least one segment), cost is O(len(G) · len(P)) — no directory listings, no full-tree scan; a document's glob union matches if **any** glob does. The previously stated normalization applies first (`./` prefix, trailing `/` stripped; `dir/` and `dir` are `dir/**`).

**Test vectors — mandatory in the Detailed Implementation Plan, enumerated here:**

1. P=`src/payments` (dir), G=`src/payments/api/**` → **match** (P consumed, G continues)
2. P=`src/payments` (dir), G=`src/**` → **match** (`**` absorbs P's remainder)
3. P=`src/payments` (dir), G=`docs/**` → **no match** (first literal segment prunes)
4. P=`src/payments/refund.go` (file), G=`src/**/*.go` → **match** (direct glob test)
5. P=`src/local` (dir), G=`src/*/middleware.go` → **match** — potential: `*`=local, then the final segment could name a file there, present or not
6. P=`src/a/b` (dir), G=`src/*.go` → **no match** (G exhausted while P continues — no `**` recursion)
7. G=`**` (explicit repository-wide) → **match for every P**
8. A `review_after`-only document → **never matches any P** (no path scope exists to ask)
9. Mid-glob `**`: P=`src/a` (dir), G=`src/**/test/*.go` → **match** — potential: `**` may absorb zero segments, so `src/a/test/<x>.go` qualifies, at any depth deeper or none
10. Mid-glob `**` elsewhere: P=`docs` (dir), G=`src/**/test/*.go` → **no match** (first literal segment prunes; a later `**` never rescues an earlier mismatch)
11. G=`src/a**b/x` → **not a match question at all**: fused `**` is rejected at config validation (closed glob syntax) — the decision procedure never sees it

### The output contract (G6 + G8 together)

All modes emit one envelope, identical in `json` and `yaml` — structured enough for agents, and carrying the G8 "what next" hints inside the machine-readable payload, not just in text rendering:

```json
{
  "mode": "all",
  "schema": 1,
  "exit_bits": 2,
  "documents": [
    {
      "doc": "payments-spec",
      "status": "stale",
      "triggered_watches": ["paths"],
      "changed_paths": ["src/payments/refund.go"],
      "introducing_commits": ["a1b2c3d", "9f8e7d6"],
      "document_changed": false,
      "anchor_orphaned": false,
      "anchor_recovered": false,
      "violations": [],
      "bindings": [{"name": "refund-suite", "status": "pending"}],
      "hints": ["inspect the commits, update the document, commit, then run: vanguard observe payments-spec"]
    }
  ],
  "unwatched": ["glossary-core"],
  "unobserved": ["new-spec"],
  "warnings": ["orphaned lock entry \"ghost\" — the id no longer exists in discovery; reclaim with: vanguard observe --prune"],
  "affected_in_range": []
}
```

- `triggered_watches[]` holds **declared watch sources that fired** (`paths`, `review_after`) — bindings never appear here; document-side drift is the boolean `document_changed`. A missing anchor commit sets `anchor_orphaned` (HEAD fallback comparison) or `anchor_recovered` (first-parent recovery succeeded) — both informational, both cleared by the next `observe`.
- `violations[]` (structure phase) carries `{rule, file, line, message}` from the AST node — the *where* of G8; `hints[]` carries the exact next command — the *what next* of G8. Both are present in JSON, so an agent gets actionable failures without parsing prose.
- `bindings[]` appears when bindings are referenced or executed; `bindings[].status` is one of `pending | pass | failed` — `pending` is the **only** notation for referenced-but-not-executed (one name, no alias notation anywhere); `failed` carries the command's exit code. `affected_in_range[]` is populated only in `--from/--to` mode (replacing the stale/dirty axis there); `unwatched`/`unobserved` list ids in the store-level sections.
- **Closed value sets**: `status` is exactly one of `fresh | stale | dirty | unobserved` — a **fatal condition is not a status**: it sets the exit bit and surfaces through `violations[]`/`hints[]` while per-document statuses stay well-formed. `mode` is exactly one of `all | doc | path | range | observe`. `unwatched` appears only in the store-level section (not a per-document status). `introducing_commits[]` OIDs are stored **in full** in machine formats; the `text` renderer may abbreviate (all examples in this document abbreviate for readability only).
- **YAML shape parity (`--format yaml`)**: a closed quoting rule guarantees the YAML envelope carries exactly the JSON envelope's values — every string that YAML 1.1 could *not* resolve as a string (numeric-looking OIDs and hashes, `no/on/yes` bool-like ids, null-like values) is **double-quoted**; only unambiguous strings stay unquoted. Same shape, always parseable to the same types.
- **Determinism contracts on the envelope**: `documents[]` is sorted **by `doc` id bytes**; so are the store-level arrays `unwatched[]` and `unobserved[]`; within a document, `introducing_commits[]` is sorted **oldest → newest** (chronological reading order; equal-position ties break by OID lexicographically); **merge commits attribute against their first parent only** (consistent with the recovery walk — a merge's second parent is a branch, whose churn the merge commit itself introduces); `--from`/`--to` refs resolve **in the target repository** (never the store's). And the envelope carries **`"schema": 1`** — a version marker for the output contract itself, so vanguard can evolve the envelope without breaking agents that pin the shape.
- Store-level fields: `unwatched[]`, `unobserved[]`, `affected_in_range[]` (range mode), `warnings[]` (advisory findings, composed per mode: on **`check`** — discovery skips carrying file + reason, and orphaned lock entries with the reclaim hint; on **`observe`** — hash-mode scopes past the 10,000-file bound, blobless-clone confirmations from a dirty refusal, and the detached-HEAD note; **never an exit bit, never a status**; stable per run — orphan entries sorted), and **`lock_lost: true`** — set when the lockfile is absent while every discovered document reports `unobserved` (unwatched documents count toward the signature: with no lock they hold no attestation either), with the hint *restore from git (`git checkout -- .vanguard/vanguard.lock`) or bootstrap (`vanguard init`, then `observe --all`)*. It is what explains a lost-lock exit of bit `2` whose per-document statuses are all `unobserved`. **Scope of evaluation**: the lost-lock condition is judged over the **full discovery set** and the field (plus its default stale bit) appears **only in `all` mode** — narrower modes (`doc`, `path`) cannot judge whole-store state, so they report per-document `unobserved` with the hint *"run a full `vanguard check` to see whether the whole lock is missing"*.

When multiple conditions apply, the **exit bits compose by OR**; two different things were historically conflated in prose — the chain and the field — so both are stated separately, and the per-document `status` field (defined above in the envelope) is always exactly one of the closed values:

```text
exit-bit composition (run level):
    fatal(1) dominates the run exit; the remaining bits compose by OR.

per-document status selection (freshness axis only — Stale > Dirty > Fresh):
    stale   if any staleness condition holds (dirty may also hold)
    dirty   if only the uncommitted-churn condition holds
    fresh   otherwise
```

A **fatal is never a status value**: it is a run-level bit (1) that surfaces through `violations[]` / `hints[]` — the chain marker `Fatal >` means the fatal bit dominates the *exit code*, not the field. The anchoring formula settles stale > dirty: when both uncommitted churn and committed range churn exist, the document is stale.

`unobserved` **is** a legal per-document status (declared watches, no baseline) — what sits outside the chain is the *exit-bit consequence*: the gap categories set **no exit bit by default** (a repository with one such document still exits 0, so naive `|| fail` CI is not broken); `--strict` elevates both categories to bit 8 for teams that want zero-gap enforcement. `unwatched` is **not a status** — it is a store-level section only (there is nothing to watch: a property of the document set, not a report line about freshness).

`affected_in_range` (Path-Hit Attribution mode only) replaces the stale/dirty axis in that mode's output; at command level it sets **no bit by default** (`affected ≠ stale`); `--fail-on-affected` promotes it to a **dedicated bit (32)** so CI can gate on range churn without masking it as staleness. **Semantics: `affected` ≠ `stale`** — the mode never reads `vanguard.lock`, so a document attested *after* the range still appears; it answers "what churned here", not "what is stale".

Exit codes use a **bitmask**, so flags compose and CI can gate on any subset independently:

| Bit | Value | Meaning |
|---|---|---|
| — | `0` | success / all fresh and valid (informational sections present → still 0) |
| 0 | `1` | fatal error: schema validation failed, broken link/id reference, IO error |
| 1 | `2` | stale detected |
| 2 | `4` | dirty detected (uncommitted changes in a watch scope) |
| 3 | `8` | gap categories (`unwatched` / `unobserved`) — **only when `--strict` is passed** |
| 4 | `16` | `binding_failed` — only under `--run-bindings`; never set by staleness |
| 5 | `32` | `affected_in_range` non-empty — **only in `--from/--to` mode under `--fail-on-affected`**; never set by staleness |

Example: stale and dirty together → exit code `6` (`2 + 4`). CI blocks on staleness alone with `exit_code & 2 != 0`.

### The observe envelope — same contract, observe's shape

`observe` emits the same envelope philosophy with an observe-specific shape (identical in `text`/`json`/`yaml`):

```json
{
  "mode": "observe",
  "schema": 1,
  "exit_bits": 0,
  "lock": {"changed": true, "path": ".vanguard/vanguard.lock"},
  "documents": [
    {
      "doc": "new-spec",
      "observed_at": "2026-11-03T09:12:00Z",
      "anchor": "e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3",
      "hints": ["commit the lock: git add .vanguard/vanguard.lock && git commit"]
    }
  ],
  "refused": [
    {"doc": "api-contract", "reason": "watch scope dirty", "offending": ["src/api/router.go"]}
  ]
}
```

- **`documents[]` here carries no `status` field** — the attestation itself is the event, and computing a post-observe status would just re-run `check`; `observed_at` and (git mode) the new `anchor` are what observe writes. `lock.changed` is true when the write succeeded, and a store-level hint *"commit the lock: git add .vanguard/vanguard.lock && git commit"* rides on every changed observe — the lock diff is the reviewable attestation artifact. `refused[]` lists per-document refusals from guarded `--all` (or a single refused `<doc-id>`) with the precondition that failed — the reason strings are a closed set matching the code exactly: `structure violations`, `watch scope dirty`, `document not committed`, `future or earlier observed_at`, `IO failure`, `config error`, `merge or rebase in progress` — each carrying the G8 hint. **Exit bits, per refusal kind**: structural/IO/integrity refusals (structure, **and the time-integrity refusals `future or earlier observed_at`**) → bit `1`; `watch scope dirty` → bit `4`; a CAS conflict (`lock changed during observe`) aborts the whole run with bit `1` and **no write** — nothing is partially applied. `--prune` reports dropped ids in a `pruned[]` list when `changed` is true; a prune with nothing to drop exits `0` with an empty diff.

### Output formats and terminal presentation

**Self-describing feedback contract (G8).** Every failure output carries three things: *what* failed (rule id / watch type), *where* (file + line from the AST node), and *what next* (the exact command that resolves it). This is what lets an agent use vanguard with no prior training:

```text
error   payments-spec.md:42   heading rule violated: require_order expected "Non-Goals" after "Goals"
        → fix the heading order in the document, or adjust schemas/feature-spec.yaml

stale   payments-spec         paths watch: net scope change since anchor a1b2c3d
        → inspect the commits, update the document, commit, then run: vanguard observe payments-spec
```

Default `text` output is a compact, uncolored report — plain lines agents and humans parse identically; the terminal-first color presentation is a separate format, `pretty` (below). **Encoding is pinned for stability** — the thing agents parse: stdout is **UTF-8 always** (on Windows the terminal must be configured for UTF-8 — the engine does not switch console modes); if the underlying stream cannot encode a glyph, `text` and `pretty` degrade to ASCII-safe rendering (`json`/`yaml` are never degraded — they are byte-stable UTF-8 with non-ASCII escaped where the format requires). ANSI styling belongs to `pretty`; `text` itself ships no styling, so piped text is byte-identical to TTY text.

**`--format pretty` - the terminal-first presentation.** `pretty` renders the same envelope data as a centered, color-coded ledger: a brand title, a tally line (fresh/stale/dirty/unobserved counts), one row per document (`Document`, `Watches`, `Attested`, `Verdict`) with a one-line reason under each verdict (structural violations lead, then document change, review overdue, commit attribution, or scope/scheme change), store-level footers (`unwatched`, `lock lost`) left-aligned to the table margin. **Presentation only** - the envelope contract for `json`/`yaml` is untouched, and `pretty` adds no fields and changes no exit bits. The palette is fixed (no theme configuration in v1); colors are text-color only, never backgrounds. Width adapts to the terminal (`COLUMNS` overrides, else the TTY size, else unlimited): on narrow terminals the `Attested` column drops first, then `Watches`; `Document` and `Verdict` always remain. When stdout is not a terminal, `pretty` degrades to `text` rendering (`CLICOLOR_FORCE=1` forces the styled output for demos and CI snapshots); `NO_COLOR`/`--no-color` strip ANSI from `pretty` the same way.

```text
Vanguard check — 14 documents, 3 stale, 1 dirty, 1 unobserved

 STALE   payments-spec            paths      src/payments/refund.go, src/payments/gateway.go
                                  introduced by a1b2c3d "add partial refund flow", 9f8e7d6 "rename Gateway"
 DIRTY   api-contract             paths      src/api/router.go (uncommitted)
 FRESH   adr-001                  —
 UNOBSERVED  new-spec             never attested → run: vanguard observe new-spec
 UNWATCHED  glossary-core         no watches declared
```

With `--run-bindings`, binding outcomes render inline:

```text
 BINDING FAILED  payments-spec    refund-suite  exit 1
                 → inspect the binding's diagnostics; this is not staleness
```

- `pretty` verdict colors: `stale` red, `dirty` yellow, `fresh` green, `unobserved`/`unwatched` gray-purple; the document id is bold, watch types and reason lines dimmed.
- Colors apply only when stdout is a TTY and `NO_COLOR`/`--no-color` are absent — CI logs and piped output stay clean (`CLICOLOR_FORCE=1` forces them back on for demos and snapshots).
- `--format yaml` follows the YAML shape-parity quoting rule (above); YAML is preferred for human-diffable CI artifacts, JSON for agents.

### Go dependencies

| Package | Purpose | Status |
|---|---|---|
| `github.com/go-git/go-git/v5` | embedded git: ref resolve, revwalk, tree diff, worktree status (dirty candidates) | ships |
| `gopkg.in/yaml.v3` | frontmatter and `vanguard.yaml` parsing | ships |
| `github.com/spf13/cobra` | CLI surface | ships |
| `charm.land/lipgloss/v2` | terminal styling: the `pretty` format's colors and centered layout | ships |
| `github.com/charmbracelet/x/term` | terminal width detection for the responsive `pretty` layout | ships |
| `github.com/yuin/goldmark` | markdown AST (the D9 rule vocabulary's planned substrate, beyond the shipped heading-line scan) | Not started (see ledger: structure phase) |
| `github.com/santhosh-tekuri/jsonschema/v6` | JSON document verification | Not started (see ledger: structure phase) |

No glob library: `**` semantics are the closed in-house syntax of `internal/pathmatch` (glob syntax v1), so two implementations can never diverge on what a glob means. No git-CLI dependency exists anywhere — dirty detection works through the same embedded go-git ODB (see dirty detection).

### Module layout (new Go module `vanguard/`)

```text
vanguard/
├── cmd/vanguard/main.go        # wiring only
├── internal/config/            # vanguard.yaml load + validation (strict parse)
├── internal/docmodel/          # frontmatter/metadata extraction (md/json/yaml/dsl),
│                               # id grammar, discovery, engine fields
├── internal/duration/          # review_after duration grammar (h/d/w/mo/y)
├── internal/hashnorm/          # the Hash normalization pipeline (streaming)
├── internal/structure/         # declared schema profiles (x-vanguard): sections,
│                               # citations, max_depth — heading-line scan
├── internal/lockfile/          # vanguard.lock read/write (write path: observe
│                               # only; CAS token + atomic rename)
├── internal/provider/          # provider interface: GitProvider (go-git, incl.
│                               # blobless clones), HashProvider; RemoteGitProvider post-v1
├── internal/pathmatch/         # glob compilation, `--path` decision procedure
│                               # (segment reasoning + test vectors from the body)
├── internal/scope/             # scope hash strategies (subtree/blob/enumerated)
│                               # + the scheme-tagged combination
└── internal/check/             # engine: check modes, anchoring, observe, bindings,
                                # envelope + rendering (text, pretty, json, yaml), exit bitmask
```

## Alternatives Considered

*Each alternative is stated with the reason it was rejected; the body carries the accepted design.*

| Alternative | Outcome and reason |
|---|---|
| Workflow-only prevention (OpenSpec delta-first) | *Rejected as the core mechanism* — convention, not detection; nothing alarms when the workflow is skipped. Kept as the NG6 complement, never the sensor |
| LLM-based semantic comparison ("does the doc still match the code?") | *Rejected* (NG3) — expensive per run, non-deterministic, stateless between runs, reports prose instead of measurements. External LLM scanners may consume vanguard's JSON; the engine stays a deterministic measuring device |
| Prose linter (Vale) inside the engine | *Rejected* (NG7) — style is not staleness; binding outcomes are never mapped onto staleness at all (`binding_failed`, bit 16 — never `stale`). Teams wanting zero style signal simply do not bind Vale |
| `docs` watch (document-to-document staleness propagation) | *Removed* — every doc became a sensor for every other; the model collapses to "one document vs the code it names". Cross-references stay informational |
| `refs` watch (literal text / symbol search) | *Removed* — requires blob contents (breaks blobless clones and the no-blob-reads guarantee); symbol semantics drift into NG1. Teams run their own tools and cite files via `paths` |
| Implicit repository-wide scope for watches without `paths` | *Rejected* — made `review_after`-only documents stale on any repository change (contradicts CUJ 3 and `--path`). repository-wide is explicit: `paths: ["**"]` |
| Derived anchor (document's add-commit or last-touching commit) | *Rejected* — decoupled stores break fatally (a docs-repository SHA cannot resolve in the target's ODB); last-commit deriving re-runs per check with no attestation semantics. **Stored anchor** accepted: travels with the committed lock, advanced only by explicit observe |
| Anchor = commit SHA only / tree hash only | Both rejected — SHA-only dies on squash/rebase (orphaned) and shallow clones; hash-only loses attribution and cannot distinguish "never anchored" from churn. **Hybrid accepted**: full OID + `scope_tree_hash` + bounded first-parent recovery (squash-merge treated as the common case) |
| Anchor derived at discovery for unobserved documents | *Rejected* — non-deterministic; keeps a new document silently fresh through a thousand commits. `unobserved` (both modes) is the honest state |
| `last_reviewed` frontmatter stamp / git notes for attestations | *Rejected* — mechanically gameable (nobody can tell a stamp was earned), commit noise, and git notes are invisible in ordinary PR review. The committed lock diff is the record reviewers actually see |
| Post-merge CI observe job | *Rejected for v1* — automated attestation is the "observe without review" hole wearing a CI badge; merge-queue-less teams re-observe manually, and DIP 4.7's pilot measures whether that is acceptable |
| Gitignored, per-machine lock | *Rejected* — absent exactly where CI needs it (fresh clones). Committed + machine-independent hashes shares state like `go.sum` |
| `observations.jsonl` ledger / per-document state files | *Rejected* — write-only with no reader (git history is the audit trail) and multi-file merge chaos. Single locked file, canonical sorted serialization, `observe --prune` as the only garbage collector |
| Config (provider, target path) inside the lock | *Rejected* — the entry shape implies the mode and the target belongs to config; less in the lock means fewer senseless conflicts |
| `check --record` (check writes state) | *Rejected* — a read-only `check` silently re-anchoring lets an agent sweep attestations as a side effect; re-anchoring stays a deliberate, git-history-visible act |
| Single-commit observe (auto-commit, `--amend`, staged-doc) | *Rejected* — hides the lock change from review; the two-commit friction (edit → commit → observe → commit lock) is acknowledged and made visible (CUJ 2), not papered over |
| `observe --all` without guards | *Kept only guarded* — the sweep walks the `unobserved` set (never `stale`), attesting or refusing each document individually with differentiated exit bits; mass attestation of stale documents is exactly the hole it protects against |
| External git CLI | *Rejected* (G4) — embedded go-git: one static binary, no installation, machine-independent; the cost (incomplete partial-clone / clean-filter coverage) is named in the M0 spike, not hidden |
| aqua CLI subprocess / aqua Go packages | Both *rejected for v1* — the CLI adds a process and machine-local drift; the library is a heavy dependency with an unstable internal API that pulls registry semantics into scope. With provisioning itself **deferred post-v1** (D10), the aqua adapter stays a recorded post-v1 `Provisioner` extension |
| Persisted binding proofs (committed files, git notes) | *Rejected* — a test run is not a git event, and a proof from another machine describes a different worktree; CI produces its own proof at its own HEAD |
| Subtree OID everywhere / enumeration everywhere | Both rejected — over-approximation destroys filter precision vs O(scope) waste where O(1) lookups suffice. **Mixed by exact glob shape** + normalization (`./`, trailing `/`; `dir/*` is filtered), fully specified with mandated test vectors |
| CODEOWNERS-style default path coverage | *Rejected* — implies default-on coverage inherited from a format owned by another tool. Per-document declared watches: opt-in, reviewable, free-form (one doc may watch disjoint scopes) |
| 20-line git script (`git diff <anchor> -- <paths>` in CI) | *Outlined, then rejected* — no attestation state (`unobserved`/`unwatched` invisible), no squash-merge survival, no `content_hash` (silently fresh after a doc edit), no structure/id checks or agent envelope (`exit 1` + prose), and every platform trap listed above (`autocrlf`, self-referential store, blobless clones, case-sensitivity, glob-shape semantics). It solves the aging-fresh slice and **fails silently on the rest** — a prototype, not a peer |

## Non-Functional Requirements

* **Supply-chain posture.** In v1 there is no provisioned-binary chain (the `vanguard install` feature is **deferred post-v1** — see Tool provisioning). When it returns, its trust links stay as recorded there: (1) the pinned `url` declared in config, (2) the recorded `sha256` — a failure at either aborts the install with a non-zero exit; nothing is placed into `.vanguard/bin/` unverified; no plaintext HTTP; `sha256` mismatch is a fatal, not a warning.
* **Pinned, no auto-update.** Tool versions are pinned by the team's own pipeline (container images, setup steps) in v1; there is no automatic update path in vanguard. Updates are explicit config changes — reviewable, visible in git history.
* **Structure-layer resource caps (DoS guard).** Parsing is adversarial-safe: **frontmatter ≤ 256 KiB** (larger → fatal with hint *split the document*); declared `max_depth` ≤ `32` (config validation); goldmark runs with bounded parsing (its default limits). A cap violation is never a crash or an unbounded hang — it is a fatal with a G8 hint. Document JSON Schema verification uses **draft 2020-12**: a user schema whose `$schema` declares another draft is a config validation error (one dialect, deterministic behavior).
* **Execution quarantine.** External tools execute only inside the `--run-bindings` boundary, resolved from the ambient PATH (no shim directory exists in v1). The default `check` hot-path never spawns a process, opens a network connection, or **reads git blob contents from history** (commit graph + tree metadata only — blobless-clone safe). The one scoped exception is dirty detection, which reads **HEAD blob contents for scoped status-candidate paths only** — never for history, attribution, or the walk (the precise rule lives in "Dirty detection and blob reads" above); a blobless clone missing a blob reports the file **dirty with a hint**, loud, never silent.
* **Performance.** Two costs, stated separately. **Committed-state comparison** (anchor/HEAD vs lock) is O(1)-ish per document: directory-aligned scopes resolve to a git **subtree OID lookup** (O(path depth)), repository-wide scopes hash only level-1 entries; only filtered globs enumerate their matches. Attribution is a first-parent walk bounded by the anchor's reachable set — O(changed commits in the range) plus one ancestry walk to build the set; first-parent recovery is bounded (default 1000 commits) and only runs when the anchor is unreachable — with filter-free glob scopes it is one OID lookup per commit, with filtered scopes O(walk × matches). **Dirty detection** is inherently O(scope files): the worktree status pass visits every file, and each candidate is hashed (worktree bytes + HEAD blob) over the normalization pipeline — the spike's performance budget (≤ 2.0 s for a 2,000-file scope) governs this. Structure verification covers requested documents only. Zero network in the default path. **State travels with the clone** — CI reads the committed lock, no cache restore step exists in the correctness path, and binding proofs are always produced by the CI job itself via `check --run-bindings`.
* **Determinism & testability.** Identical store + repository + `--now` inputs produce identical exit codes and JSON output; `review_after` is clock-injected; `observed_at` is injectable in **both** modes via `observe --at <RFC3339>`; the first-parent recovery walk is bounded and deterministic; binding flakiness is confined to the `--run-bindings` boundary by declaration.

## Detailed Implementation Plan

*The plan is milestone-ordered; every milestone ends in a reviewable, mergeable state and carries an explicit **Definition of done**. **M0 has a go/no-go exit — nothing else may start before it passes.** Fixtures for every test vector carry full OIDs/hashes (the body's examples are abbreviated for readability only). Estimates are order-of-magnitude (one week ≈ one engineer). **Release planning (cross-platform builds, binary signing, distribution) is deliberately out of DIP scope** — a separate deliverable.*

### M0 — go-git capability spike (go/no-go) — *time-box: 5 working days*

**Priority ordering inside the time-box: 0.2–0.5 are blocking** (the spike cannot answer its question without them); **0.6–0.8 and 0.10 are deferrable** — they carry already-speced fallbacks, so an incomplete answer there becomes a named follow-up in the memo, never a silent skip.

| # | Task | Priority | Acceptance criterion |
|---|---|---|---|
| 0.1 | Setup: repository matrix generator (Windows + Linux runners; `autocrlf` ∈ {true, false, input}; `.gitattributes` variants: text, eol=crlf, LFS) | blocking | matrix script reproducible in CI |
| 0.2 | Partial/blobless clone: `clone --filter=blob:none` via go-git — refs/revwalk/tree-diff offline? | blocking | walk + tree metadata succeeds without network |
| 0.3 | Phantom-churn probe: go-git `Worktree.Status()` vs expected-clean files across the matrix | blocking | **clean status ⟹ candidate go (direct)**; any phantom file = the status path is rejected — this alone does **not** decide no-go, see the matrix in 0.9 |
| 0.4 | Normalized dirty-detection prototype: worktree bytes vs HEAD blob over the shared pipeline (this code becomes `internal/hashnorm`) | blocking | **performance budget, absolute**: ≤ 2.0 s for a 2,000-file scope on the reference runner (mid-range laptop) — provisional, re-measured against the chosen baseline; behavioral exactness vs the normalization vector stubs |
| 0.5 | Blobless + missing-blob behavior | blocking | missing blob → "dirty-loud" branch reachable and hint-carrying |
| 0.6 | `.git`-as-file discovery (linked worktree, submodule): ODB resolution via gitdir pointer | deferrable | resolves on both layouts (else: documented workaround) |
| 0.7 | Index oddities: sparse-checkout, `assume-unchanged`, `skip-worktree`, racy-git exposure | deferrable | behavior recorded; fallback strategy named per-gap |
| 0.8 | Scope-restricted dirty detection cost | deferrable | measured; ≤ O(scope files) |
| 0.9 | **Go/no-go memo with the decision matrix** (below) | blocking | explicit decision; **no engine code proceeds before this memo exists** |
| 0.10 | `object-format=sha256` repository support in go-git (tree reads, revwalk, status) | deferrable | verdict recorded — decides whether the sha256 test leg survives into the CI matrix (M1) |

**Decision matrix for the memo (0.9)**:

| 0.3 (status path) | 0.4 (normalized prototype) | Decision |
|---|---|---|
| clean | passes | **GO — direct path** (normalized detection implemented; status path unused for dirty) |
| phantom churn | passes | **GO — with mitigation**: the status path is bypassed; dirty detection is implemented *only* on the normalized comparison (0.4's code becomes production code in M1.5/1.6) |
| clean | fails budget | **GO — restricted**: status path used for dirty detection with its known filter limits; Windows `autocrlf=true` worktrees get a documented caveat — accepted only if the inaccuracy direction is proven **loud** (phantom-dirty, never phantom-fresh) |
| phantom churn | fails | **NO-GO for go-git dirty detection** → fallback: scope-restricted manual worktree comparison; if metadata walking is also broken, GitProvider degrades to HashProvider for affected operations — every spec amendment listed in the memo |

### M1 — pure computational core: glob/hash/pathmatch/hashnorm (+ providers) — *est. 2 weeks*

**Definition of done: the entire test-vector suite (scope hash, pathmatch 1–11, normalization 1–8) passes on Windows and Linux; the determinism harness proves byte-identical hash values for identical trees across both runners; the sha256-repository leg is included iff M0 0.10 approved it.** This milestone performs **no document discovery** — documents, frontmatter, and formats are the next milestone's business; M1 is the deterministic mathematical heart everything else consumes. **It does include the production GitProvider/HashProvider machinery**, because M3's observe preconditions depend on it — no prototype leakage is allowed past this milestone.

| # | Task | Notes |
|---|---|---|
| 1.1 | `internal/pathmatch`: compile → normalize (`./`, trailing `/`, `**` collapse, brace expansion with the closed rules) → classify (filter-free vs filtered) | config-validation errors covered: nested `{}`, empty alternative, fused `**`, `!`, lone `\` |
| 1.2 | Subtree-OID strategy (git tree of the directory) + repository-wide synthetic level-1 strategy | both read tree metadata only |
| 1.3 | Enumeration strategy + path-prefix `.vanguard/` filter | streamed; sorted entry list |
| 1.4 | Multi-glob union hash + `scope_file_count` | post-expansion canonical list |
| 1.5 | `internal/hashnorm` **production**: the pipeline promoted from prototype 0.4 (BOM → LF fold → EOF-newline strip; binary-first detection; pending-LF streaming buffer) | **one code path, used by content_hash, scope_snapshot, and dirty detection** |
| 1.6 | Production **GitProvider dirty detection**: normalized worktree-vs-HEAD-blob comparison, scoped to stat-mismatched candidates; index oddities honored per the spike verdicts (sparse/assume-unchanged/skip-worktree/racy); blobless missing-blob → dirty-loud branch | reuses 0.4/1.5 — no second implementation |
| 1.7 | **HashProvider `scope_snapshot` engine**: per-file streamed hashes, path→hash map assembly | the only provider alternative to 1.2/1.6 |
| 1.8 | `decide()` — the `check --path` decision procedure | O(len(G)·len(P)); no FS |
| 1.9 | Test-corpus infrastructure: table-driven fixtures in `testdata/`, determinism harness, CI matrix wiring | each vector = one table-driven test |
| 1.10 | Corpus pass: scope-hash vectors (nested globs, overlap-not-dedup, rename re-keys, single-glob degenerate, kind-prefix `tree:` vs `sha256:`, object-format sha256 repos *iff M0 0.10 approved*), pathmatch vectors 1–11, normalization vectors 1–8 | full-OID fixtures only |

### M2 — document model, discovery, structure verification (+ `init`) — *est. 2 weeks*

**Definition of done: the discovery matrix (lexicographic order, symlink skip, unknown-format skip) is green; every format handler named in the body is implemented and tested (markdown rule vocabulary — closed set, json/yaml via JSON Schema draft 2020-12, dsl metadata block, `schema:` mandatory with `none` as the explicit opt-out); engine-field stripping is proven (a user schema with `additionalProperties: false` validates documents carrying engine fields); the resource caps (frontmatter 256 KiB, `max_depth` 32) are enforced with fatal hints; `vanguard init` writes the full scaffold including `{"version": 1, "docs": {}}` with the bootstrap guidance in its output.**

| # | Task | Notes |
|---|---|---|
| 2.1 | `internal/config`: `vanguard.yaml` load + validation (strict parsing — duplicate keys fatal, anchors rejected; `version` rule; format-handler mapping) | |
| 2.2 | `internal/docmodel`: frontmatter extraction, engine-owned-field stripping, `id` grammar + collision fatal | supplies document state to M3 (observe) and M4 (check) |
| 2.3 | Discovery engine: discovery globs, lexicographic order, symlink skip, unknown-format skip with advisory | warning carries file + reason (G8) |
| 2.4 | `internal/format`: markdown (goldmark + closed rule vocabulary), json/yaml (`santhosh-tekuri/jsonschema/v6`, draft 2020-12), dsl metadata block per body spec | unknown `$schema` draft = config error |
| 2.5 | Structure-verification layer: rule evaluation, id uniqueness, reference resolution (`forbid_broken`), resource caps | violation → `{rule, file, line, message}` |
| 2.6 | `vanguard init`: scaffold + empty well-formed lock + bootstrap guidance | unblocks bootstrap ahead of observe (M3) — moved out of provisioning deliberately |

### M3 — lock + observe — *est. 1.5 weeks*

**Definition of done: canonical round-trip byte-identical; concurrent-observe CAS race test green; the full refusal matrix (structure/IO → bit 1, scope dirty → bit 4, time-integrity → bit 1) green; lost-lock detector green incl. the documented emptied-lock false-negative (`--strict` path); entry-shape matrix (git / HashProvider / review_after-only) green; no prototype leakage (task 3.5).**

| # | Task | Notes |
|---|---|---|
| 3.1 | `internal/lockfile`: canonical serialization (sorted ids, fixed field order, LF, trailing newline), load/validate (`version` major gate, unknown-field preservation), three entry shapes incl. the `review_after`-only minimal entry | round-trip property test: canonical(file) == file, byte-identical |
| 3.2 | CAS write path (temp + rename + captured-bytes re-read) | race test: second writer aborts fatal, file unchanged |
| 3.3 | Observe engine: preconditions (structure verify — consumes M2; scope clean — consumes M1.6/M1.7; doc committed / store-not-git exemption), time rules (future-guard vs system UTC, monotonicity, `--at`) | refusal → exit-bit mapping per the observe envelope spec |
| 3.4 | `--all` guarded sweep + per-document refusal independence; `--prune` standalone | partial success allowed; pruned ids listed |
| 3.5 | **Dependency check**: every observe precondition has a production implementation from earlier milestones — prototype blockage surfaces here | milestone contract enforced |
| 3.6 | Lost-lock detector (`all`-mode only): lock absent + full unobserved → stale bit + `lock_lost` | emptied-lock case: accepted false-negative, tested once |

### M4 — check modes + envelope + exit bits — *est. 1.5 weeks*

**Definition of done: envelope golden tests for all modes; shape parity (JSON vs YAML same values) green; the flags usage matrix green (mode-inapplicable flags = usage error); exit-bit composition incl. bits 16/32; the squash-merge scenario suite green (rebase, squash with intervening PR, shallow clone, A→B→A fresh-with-attribution); `documents[]` and store-level determinism sorts green; `document_changed` applies to observed unwatched docs (test included).**

| # | Task | Notes |
|---|---|---|
| 4.1 | Two-phase check engine (structure fail-closed; freshness) with per-mode scope (`all`, `<doc>` with id-beats-path precedence, `--path` with its resolution rules, `--from/--to`) | consumes M1 paths + M3 lock |
| 4.2 | Anchor resolution: fast path → bounded first-parent recovery → net-state HEAD fallback; `anchor_orphaned`/`anchor_recovered` | the squash-merge scenario suite lives here |
| 4.3 | `review_after` + `--now` determinism; duration grammar | |
| 4.4 | Envelope (`schema: 1`) + text/json/yaml renderers; YAML quoting rule; `text` encoding stability | envelope golden tests |
| 4.5 | Exit-bit composition + range mode (`--to` defaults to HEAD, tag peeling, target-repository refs, `affected ≠ stale`, bit 32 under `--fail-on-affected`) | |
| 4.6 | Flags + determinism matrix tests (`--strict` ∘ `--from` usage error; absolute/`..` path rules; `documents[]`/`unwatched[]`/`unobserved[]` sorts) | |
| 4.7 | **Pilot preparation — repository selection**: pick the repository(s) for the post-sign-off squash-merge measurement | schedules the pilot; measurement runs after this milestone |

### M5 — bindings + provisioning — *est. 1 week*

**Definition of done: binding lifecycle tests (pass/fail/timeout/missing command → bit 16, never staleness) green; the provenance line covers git and no-git stores; `--run-bindings` executes and still writes nothing.** (Bindings **ship**; provisioning tasks 5.2 are **deferred post-v1** per D10 — the executor resolves from the ambient PATH until the `Provisioner` seam returns.)

| # | Task | Notes |
|---|---|---|
| 5.1 | Bindings allowlist parsing (YAML array; the shlex single-string shorthand is a recorded extension) + quarantine executor: argv exec, cwd rules, `VANGUARD_*` env injection, timeout | exit-code-only outcome |
| 5.2 | *Deferred post-v1* — pinned provisioning (`url` + `sha256`) behind `Provisioner`; `.vanguard/bin/` placement; execution-time checksum verification | fail-closed; ambient-PATH binaries outside the guarantee |
| 5.3 | Provenance line (`@ HEAD-or-no-git`); fork-PR warning rendering | |
| 5.4 | Binding lifecycle + pending-notation tests | flaky-test caveat installed as documentation |

### Cross-cutting DIP commitments

* **Testing strategy**: table-driven fixtures (full-OID only); CI matrix = {windows, linux} × {sha1, sha256 iff M0-approved} × {autocrlf on/off}; a milestone's tests are part of its merge, never a follow-up.
* **Pilot measurement**: runs after M4, before v1 sign-off — stale-after-merge rate with/without merge queue, per the Open Questions acceptance criterion; repository selection is task 4.7, so the schedule is committed rather than implied.
* **Known-limitations list shipped with v1** (from the body): no subtree exclusion (`exclude:` = the first scheduled v1.1 feature), two-commit friction, no `refs` watch, blobless bootstrap constraint on dirty scopes.
* **Out of scope**: release engineering (builds, signing, distribution), registry integration (post-v1 `Provisioner` adapter), `--journal`/`vanguard log` forensics, `RemoteGitProvider`.
* **Final assembly (Step 6 after this plan)**: ADR split of the decision log, Glossary, single-source pass (one normative home per assertion), edge-table row splitting for readability.

## Glossary

| Term | Meaning |
|---|---|
| **Anchor** | The commit at which a document was last attested by `observe`; stored in the lock, advanced only by observe |
| **Attestation** | The observe act — a human (or an `--at`-timestamped CI run) asserting the document is accurate as of that moment |
| **Baseline** | The lock entry's recorded state (anchor, scope hash, content hash) that `check` compares against |
| **Binding** | A user-declared verification command in the `vanguard.yaml` allowlist; executed only under `check --run-bindings` |
| **Churn** | Change inside a watch scope — uncommitted (dirty axis) or committed range churn (stale axis); a binding outcome is never churn |
| **Dirty** | A watch scope whose worktree differs from HEAD — blocks re-anchoring; exit bit 4 |
| **Envelope** | The structured output (`schema`, `mode`, `exit_bits`, `documents[]`, …) identical in `json` and `yaml` shape |
| **Exit bit** | One OR-composable condition: 1 fatal, 2 stale, 4 dirty, 8 gaps (`--strict`), 16 binding_failed, 32 affected_in_range |
| **Filter-free / filtered glob** | `dir/**` (resolved via subtree OID) vs any glob with a filename part (resolved via enumeration) — the hash-strategy boundary |
| **Gap category** | `unwatched` (no declared watches, store-level section only) or `unobserved` (declared watches, no lock entry, legal per-doc status) |
| **Gitlink** | A submodule entry in the parent tree (mode `160000`) — hashes the recorded submodule commit SHA |
| **Lost lock** | lockfile absent while every discovered document reports `unobserved` — sets the stale bit by default (all mode) |
| **Net-state rule** | Staleness is decided by comparing scope tree state; history (revwalk) exists only for attribution — an A→B→A window is fresh |
| **Scope** | The union of a document's watch globs; `.vanguard/` never belongs to any scope |
| **Scope snapshot** | HashProvider's per-file `path → content-hash` map of a scope at anchor time |
| **Subtree OID** | Git's own Merkle tree hash of a directory — the filter-free scope hash, O(1) per lookup |
| **Synthetic level-1 list** | The repository-wide hash's construction: hashes top-level entries with the store directory excluded by name |
| **Two-tier exit** | Structural errors are fatal; attestation gaps degrade loudly, not fatally — with the one default escalation for a lost lock |

--------------------------------------------------------------------------------

*Assembled at Step 6 of the EGM drafting workflow. The body above is single-source and normative; contradiction passes, ADR trimming, and this glossary complete the final assembly. Handoff: `EGM-review`.*
