---
id: quality-plan
schema: document
watches:
  paths:
    - "docs/designs/**"
  review_after: 30d
---

# Vanguard — Quality Assurance Plan (Epistemic Quality Decomposition)

* **Subject:** `vanguard` spec (Step 6, final design, 2026-11-03)
* **Method:** skill `epistemic-quality-decomposition` — L0 → L4, Decomposer / Attacker / Referee loop
* **Status of the whole tree:** `DRAFT`. **No adversarial round has been run yet**, so no node is `FROZEN`. Section 9 lists the attacks pre-registered for the Attacker.

---

## 1. Operating principles

1. **One layer at a time, no skipping.** Each layer is a filter: L0 → L1 (external outcomes) → L2 (failure mechanisms) → L3 (triggering contexts) → L4 (`[Metric] + [Oracle] + [Threshold]`).
2. **No solutions mixed into outcome layers.** L1 contains only external states; module names (`internal/lockfile`, ...) appear only in the traceability table (section 8).
3. **The oracle is independent of the engine.** The engine uses go-git for
   history and trees; worktree status comes from the real `git` CLI (go-git
   only as a fallback). The test oracle is likewise the **real `git` CLI**
   (and hand-computed fixtures) — using go-git to verify go-git is circular.
4. **Permitted error direction: loud only.** Vanguard may be wrong in the false-stale / phantom-dirty direction (with a hint), **never** in the false-fresh direction. This is the backbone of every L4 threshold below.
5. **Each L4 leaf ↔ exactly one test or harness.** A test that cannot be traced up to L0 is suspected junk (Necessity applied at the test layer).

---

## 2. L0 — Value Invariant

```yaml
node_id: "L0_VANGUARD_INVARIANT"
level: 0
statement: >
  For every spec document, the status Vanguard reports (fresh | stale | dirty | unobserved)
  is a deterministic and correct function of {vanguard.lock, the git tree, the worktree}.
  Vanguard never reports `fresh` when the code scope or the document itself has changed
  without being attested.
consequence_if_violated: >
  Teams and AI agents trust a spec that is out of date and write code against it — exactly
  the silent drift this project exists to prevent.
status: "DRAFT"
```

**Counter-factual test:** a `false-fresh` lasting 1 hour ⇒ an agent edits `src/payments` against a wrong spec ⇒ real damage (wrong code, wrong review). **L0 is kept.**

---

## 3. L1 — Essential Pillars

| Node | Pillar | Statement (external state) |
|---|---|---|
| **L1.1** | Semantic Correctness | Status, attribution and structure-verification results are correct. Errors are allowed only in the loud direction. |
| **L1.2** | State Integrity & Durability | `vanguard.lock`, store configuration and every hash are not distorted, do not lose updates, cannot be silently tampered with, and are identical on every machine. |
| **L1.3** | Operational Timeliness | `check`/`observe` run offline, deterministically, fast enough, and work immediately on a fresh clone / CI. |
| **L1.4** | Auditability & Actionability | Every attestation is visible in git history; every error says what / where / which command next; the envelope is stable for agents. |
| **L1.5** | Execution Containment | Only `--run-bindings` and `install` may run processes or open the network; binding results never become staleness. |

**Pre-registered attacks (L1):**

* *Sufficiency:* "Suppose all 5 pillars hit 100% and `fresh` is still wrong." Candidate: **the honesty of the attester** (observe without real review). This is out of scope per NG3 and is the spec's stated honest limit ⇒ **recorded in the Risk Register (section 10)**, not added as a pillar.
* *Necessity:* L1.5 may be a safety property rather than a pillar of L0 (remove it and `fresh/stale` is still correct). But a `binding_failed` mixed into `stale` breaks L1.1 directly, and executing external processes breaks L1.3 (offline). **Proposed: keep**, pending the Referee.

---

## 4. L2 → L3 → L4 per pillar

Column legend: **N** = Nominal, **E** = Edge/Skewed, **Ad** = Adverse/Degraded, **Adv** = Adversarial. A `—` cell means that corner does not apply, and a reason must be given if the Attacker asks.

### 4.1 L1.1 — Semantic Correctness

#### L2.1.a — Wrong scope hash

| L3 | Context |
|---|---|
| N | `dir/**` (subtree OID), a glob with a filter (enumeration), repository-wide (level-1) |
| E | File rename; exec-bit change; symlink retarget; submodule bump (gitlink); brace expansion; consecutive `**`; scope empty at check time |
| Ad | `object-format=sha256` repository; shallow clone; non-ASCII / non-UTF-8 path |
| Adv | Two overlapping globs trying to hide churn; changing a glob's form to change the hash formula |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 1.1.a-1 | Number of cases where `scope_tree_hash` is unchanged although the `(path, mode, blob)` set changed | Fixture suite + independently computed `git ls-tree` | **= 0** |
| 1.1.a-2 | Number of wrong scope-hash vectors (nested, overlap-not-deduped, rename re-key, single glob, `tree:`/`sha256:` prefix) | Hand-built vectors with full OIDs | **0 wrong / all vectors** |
| 1.1.a-3 | Hash of the same tree on Windows vs Linux | Compare the two runners | **Byte-identical** |

#### L2.1.b — Wrong anchor resolution when history is rewritten

| L3 | Context |
|---|---|
| N | Anchor is an ancestor of HEAD (fast path) |
| E | **A→B→A** (scope changed then reverted) |
| Ad | Squash-merge (orphaned anchor — *the common case*), rebase, cherry-pick, shallow clone, walk exceeding the 1000 bound |
| Adv | Anchor points at a commit present in the ODB but **not an ancestor** (fetched from another branch) |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 1.1.b-1 | Cases where the three paths (fast / recovery / fallback) yield **different statuses** on the same input | Net-state computed directly via `git rev-parse <commit>:<path>` | **= 0** |
| 1.1.b-2 | A→B→A reports `fresh` with attribution | Hand-built scenario | **100%** |
| 1.1.b-3 | Cases where an "in ODB but not an ancestor" anchor enters the fast path | `git merge-base --is-ancestor` | **= 0** |

#### L2.1.c — Wrong dirty detection

| L3 | Context |
|---|---|
| N | Edited file in scope, untracked file, `.gitignore`d file |
| E | CRLF/LF with `autocrlf` ∈ {true, false, input}; `.gitattributes` (text, eol, LFS); mode-only change with `core.filemode=false` |
| Ad | Blobless clone with missing blob; sparse-checkout; `assume-unchanged`/`skip-worktree`; **racy-git** |
| Adv | Edit a file, then restore its mtime/stat to the old value |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 1.1.c-1 | **Phantom-fresh** (worktree differs from HEAD but reported clean) | Normalized worktree bytes vs HEAD blob, computed independently | **= 0** |
| 1.1.c-2 | Phantom-dirty without a hint | `git status` as reference | **= 0 hint-less cases** (phantom-dirty with a hint is allowed) |
| 1.1.c-3 | Missing-blob loud handling (premise under review: dirty detection now delegates to `git status --porcelain`, so the canon's stat-and-byte-compare mechanism no longer applies — re-anchor this leaf or amend the canon before M0, see G5) | Blobless fixture | **100%** |

#### L2.1.d — Document axis and clock axis miss changes

| L3 | Context |
|---|---|
| N | Edit content after observe ⇒ `document_changed` |
| E | Edit frontmatter to narrow `watches.paths`; edit an **observed unwatched** document |
| Ad | BOM / line-ending / final-newline changes (must not count as churn); chunk boundaries in streamed hashing |
| Adv | Hand-edit `observed_at`; `review_after` with a skewed clock |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 1.1.d-1 | Document edits where `document_changed` does not fire | Mutation test on fixtures | **= 0** |
| 1.1.d-2 | The 8 normalization vectors | Vectors in the spec | **8/8** |
| 1.1.d-3 | `review_after`: result with a fixed `--now` | Hand-built time table (all of `h/d/w/mo/y`) | **100% correct, byte-identical on repeat** |

#### L2.1.e — Wrong `--path` reverse lookup and attribution

| L3 | Context |
|---|---|
| N | File vs directory, literal glob |
| E | Mid-glob `**`; empty directory; `src/*.go` vs `src/a/b`; a `review_after`-only document |
| Ad | Absolute path outside the target root; `..` |
| Adv | A valid glob crafted so `decide()` does not terminate |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 1.1.e-1 | The 11 `decide()` vectors | Vectors in the spec | **11/11** |
| 1.1.e-2 | `introducing_commits` wrong/missing/mis-ordered (oldest→newest, merges via first parent) | `git log --first-parent --reverse <range> -- <paths>` | **= 0 mismatches** |
| 1.1.e-3 | `--from/--to`: `affected_in_range` vs the actual changed-file set | `git diff --name-only` | **100% equal** |

#### L2.1.f — Structure verification false-pass / false-fail

| L3 | Context |
|---|---|
| N | Document conforming to its schema |
| E | Duplicate frontmatter keys; YAML anchors/aliases; missing `schema:`; ids violating the grammar / case-variant duplicates |
| Ad | Frontmatter > 256 KiB; `max_depth` > 32; DSL without a leading comment block |
| Adv | `additionalProperties:false` with engine fields; relative link escaping the repository |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 1.1.f-1 | Violating documents that pass (false-pass) | Labeled violation corpus, ≥ 1 case per closed-vocabulary rule | **= 0** |
| 1.1.f-2 | Valid documents rejected (false-fail) | Valid corpus | **= 0** |
| 1.1.f-3 | Each violation carries `{rule,file,line,message}` with `line` from the AST | Golden test | **100%** |
| 1.1.f-4 | Cap-exceeding cases that crash/hang | Fuzz + cap cases | **= 0; always fatal with a hint** |

### 4.2 L1.2 — State Integrity & Durability

#### L2.2.a — Lost updates when writing the lock

| L3 | Context |
|---|---|
| N | A single `observe` |
| E | Two `observe` runs on different documents in parallel |
| Ad | Process killed midway (temp + rename) |
| Adv | Two `observe` runs on the same document |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 2.a-1 | Lost updates per 100 race runs | Final lock content vs expected set | **= 0** |
| 2.a-2 | Losing side: exit bit 1, file **unchanged** | Byte compare before/after | **100%** |
| 2.a-3 | Lock truncated after a kill | Kill-injection test | **= 0 (always before-or-after, never in between)** |

#### L2.2.b — Non-portable serialization / hashes

| L3 | Context |
|---|---|
| N | Round-trip |
| E | Merge two branches that observed different documents |
| Ad | Windows `autocrlf`; NFC/NFD; UTF-16 (hashed raw) |
| Adv | Two branches observing the same document |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 2.b-1 | `canonical(file) == file` | Property test | **100% byte-identical** |
| 2.b-2 | Two observes of different documents produce **disjoint** diffs | Real `git merge` | **0 conflicts** |
| 2.b-3 | Same-document case: conflicts correctly and recovers by re-running `observe` | Scenario | **100%** |

#### L2.2.c — Lost / corrupt / emptied lock goes undetected

| L3 | Context |
|---|---|
| N | Valid bootstrap (`init` + nothing observed yet) is **not** reported as lost-lock |
| E | Valid lock with some documents not yet observed |
| Ad | Deleted lock; truncated JSON lock; unknown `version` major |
| Adv | Lock emptied to `{"docs":{}}` (an **accepted** false-negative) |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 2.c-1 | Lock absent + all unobserved ⇒ `lock_lost`, bit 2 (`all` mode) | Fixture | **100%** |
| 2.c-2 | Corrupt lock ⇒ fatal bit 1 | Fixture | **100%** |
| 2.c-3 | Emptied-lock case + `--strict` ⇒ bit 8 | Fixture (tested **exactly once**, documented as an accepted false-negative) | **100%** |
| 2.c-4 | Valid bootstrap wrongly reported as lost-lock | `init` fixture | **= 0** |

#### L2.2.d — Lock hand-edited / forged *(new branch, needs ratification)*

> Detailed mechanism: `lock-integrity-section.en.md` (Tier A advisory, Tier B `verify`, Tier C `verify --base`, exit bit `64`).

| L3 | Context |
|---|---|
| N | Lock written by `observe`, clean commit |
| E | Hand-edit that breaks format (id order, indent, CRLF); missing fields; mixed `anchor` and `scope_snapshot` |
| Ad | Shallow clone (lock history missing); HashProvider (no history); merge conflict resolved wrongly |
| Adv | Forge `scope_tree_hash`/`content_hash`/`anchor` field by field; **consistent** forgery of every hash; delete an entry to turn `stale` into `unobserved` |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 2.d-1 | Format-breaking hand-edits caught by `lock_noncanonical`/`lock_entry_shape` | Lock mutation scripts | **100%** |
| 2.d-2 | Forged hashes (anchor/doc unchanged) caught by Tier B | Hash computed independently via `git` CLI at the anchor | **100%** |
| 2.d-3 | Entry changed with no matching code/doc change caught by Tier C | `git diff <base>` | **100% `lock_entry_changed_without_cause`** |
| 2.d-4 | Entry removed while its id is still in discovery is caught | Fixture | **100%** |
| 2.d-5 | History checks skipped due to a shallow clone without a `skipped` hint | Shallow fixture | **= 0** |
| 2.d-6 | Fully consistent forgery (attacker computes everything correctly) | — | **Not measured; recorded as Risk Register R1** |

#### L2.2.e — Entry shape / mode / version mismatch

| L3 | Context |
|---|---|
| N | The three valid shapes (git, HashProvider, review_after-only) |
| E | Unknown field (must be **preserved**, not dropped) |
| Ad | Target mode changes (`.git` added/removed); object-format changes; unknown `version` major |
| Adv | `tree:`/`sha256:` prefixes swapped |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 2.e-1 | Shape mismatch ⇒ `unobserved` + hint (no internal error) | Fixture | **100%** |
| 2.e-2 | Unknown field lost after an observe | Field compare before/after | **= 0** |

#### L2.2.f — Store configuration tampered with (`vanguard.yaml`, `schemas/`)

| L3 | Context |
|---|---|
| N | Valid configuration |
| E | Missing `version`; YAML anchors / duplicate keys |
| Ad | Store relocated inside a watch scope (self-referential cycle) |
| Adv | Loosen `clock_skew_tolerance`; add a command to the allowlist; loosen a schema |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 2.f-1 | Configs with anchors / duplicate keys / unknown rule keys that slip through | Bad-config corpus | **= 0 (always fatal)** |
| 2.f-2 | Store paths at **any depth** not excluded from every hash and dirty computation (prefix filter, never counted as churn) | Multi-depth fixture | **= 0** |
| 2.f-3 | `config_changed`/`schema_changed` vs base ref shown correctly | `git diff <base> -- .vanguard/` | **100%** |

### 4.3 L1.3 — Operational Timeliness

#### L2.3.a — Network or processes leak into the hot path

| L3 | Context |
|---|---|
| N | Ordinary `check`, `observe` |
| E | `--path`, `--from/--to` |
| Ad | Blobless clone missing a blob (must not fetch) |
| Adv | A document/glob trying to trigger execution |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 3.a-1 | Number of socket/connect syscalls | Network-namespace-blocked sandbox + `strace` | **= 0** |
| 3.a-2 | Child processes without `--run-bindings` | `strace -f` | **= 0** |

#### L2.3.b — Performance budget exceeded

| L3 | Context |
|---|---|
| N | 2,000-file scope |
| E | Many arbitrary globs; large scope (monorepo) |
| Ad | 1000-commit recovery walk with a filtered glob (O(walk × matches)) |
| Adv | Globs crafted to blow up (braces, `**`) |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 3.b-1 | Dirty-detection time, 2,000 files | Clock on the reference runner | **≤ 2.0 s** (provisional, re-measured after M0) |
| 3.b-2 | Recovery-walk time, subtree scope, 1000 commits | Clock | **Threshold set in M0** (currently *missing* — see section 10 G3) |
| 3.b-3 | Malicious brace/glob config causing a hang | Fuzz | **= 0 (fatal with a hint within bounded time)** |

#### L2.3.c — Non-determinism

| L3 | Context |
|---|---|
| N | Run twice |
| E | Discovery order, store-level array order |
| Ad | Change OS, change `GOMAXPROCS` |
| Adv | Different `--now` across runs |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 3.c-1 | JSON across 2 runs with the same `store + repository + --now` | Byte `diff` | **Byte-identical** |
| 3.c-2 | `documents[]`, `unwatched[]`, `unobserved[]` sorted by id bytes | Golden | **100%** |

#### L2.3.d — Fresh clone / CI not usable immediately

| L3 | Context |
|---|---|
| N | Full clone |
| E | Linked worktree, `.git` as a file, submodule |
| Ad | Shallow, blobless, partial clone |
| Adv | — (not applicable: security lives in 2.d) |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 3.d-1 | `check` runs with no bootstrap on every clone kind | Clone matrix | **No internal errors; every degradation carries a hint** |
| 3.d-2 | Orphaned/shallow anchor ⇒ no hard failure | Fixture | **100%** |

### 4.4 L1.4 — Auditability & Actionability

#### L2.4.a — Attestation invisible or unverifiable

| L3 | Context |
|---|---|
| N | `observe` on a committed document with a clean scope |
| E | Uncommitted document; decoupled store (outside the repository) |
| Ad | Unborn HEAD; detached HEAD; merge/rebase in progress |
| Adv | `observe` with a dirty scope; `--at` in the future; `--at` going backwards |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 4.a-1 | `observe` refused in the right way (refusal table) | Refusal matrix | **100%**, correct bit (1 vs 4) |
| 4.a-2 | `observe` auto-commits or edits files other than the lock | Tree diff before/after | **= 0** |

#### L2.4.b — Feedback that is not actionable

| L3 | Context |
|---|---|
| N | Violation, stale, unobserved |
| E | `anchor_orphaned`/`anchor_recovered`; scope emptied (14 → 0) |
| Ad | Missing blob; shallow skips a check |
| Adv | — |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 4.b-1 | Share of error fixtures carrying all of (*what, where, next-command*) | Envelope linter | **100%** |
| 4.b-2 | `hints[]` contain a parseable, runnable command | Script that runs the hints | **100%** |

#### L2.4.c — Unstable envelope and exit bits

| L3 | Context |
|---|---|
| N | Each mode (`all/doc/path/range/observe`) |
| E | Bit combinations (2+4 = 6; 2+64 = 66) |
| Ad | Flags not applicable to the mode (must be a usage error) |
| Adv | Strings YAML misreads as other types (`no`, `on`, numeric OIDs) |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 4.c-1 | JSON vs YAML yield the same values/types | Parse both, compare | **100% equal** |
| 4.c-2 | Golden envelope per mode; `"schema": 1` | Golden | **100%** |
| 4.c-3 | Inapplicable flags silently ignored | Flag × mode matrix | **= 0** |

#### L2.4.d — Abuse of `observe` (attesting without review)

| L3 | Context |
|---|---|
| N | Deliberate `observe <id>` |
| E | Bare `observe` (no id) |
| Ad | `--all` hitting a partial failure |
| Adv | Agent running `observe <id>` document by document |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 4.d-1 | Bare `observe` accepted | Fixture | **= 0 (usage error)** |
| 4.d-2 | `--all` touching a `stale` document | Fixture | **= 0 (`unobserved` only)** |
| 4.d-3 | Deliberate abuse | — | **Not machine-measurable; Risk Register R2, mitigated by `verify --base` (lock_bulk_change) and PR review** |

### 4.5 L1.5 — Execution Containment

#### L2.5.a — Binding results leak into staleness

| L3 | Context |
|---|---|
| N | Binding pass / fail |
| E | Timeout; command not found |
| Ad | Flaky binding |
| Adv | Binding prints output that fakes a status |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 5.a-1 | Cases where `binding_failed` raises bit 2 | pass/fail/timeout/missing matrix | **= 0** |
| 5.a-2 | Pass/fail decided by exit code only | Binding with fake output | **100%** |
| 5.a-3 | Flaky: re-run N times before counting as fail | Policy | **Own threshold, an exception to the "0% deviation" rule (see section 7)** |

#### L2.5.b — Arbitrary execution via documents or config

| L3 | Context |
|---|---|
| N | Binding declared in the allowlist |
| E | Shorthand string with quotes |
| Ad | — |
| Adv | Raw command in frontmatter; shell metacharacters in `cmd`; fork PR editing the allowlist |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 5.b-1 | Raw command in frontmatter gets executed | Fixture | **= 0 (rejected)** |
| 5.b-2 | Anything goes through a shell (`sh -c`) | `strace -f` | **= 0 (pure argv exec)** |
| 5.b-3 | Provenance line printed on every `--run-bindings` (git and no-git) | Golden | **100%** |

#### L2.5.c — Binary supply chain

| L3 | Context |
|---|---|
| N | `install` with a correct `url` + `sha256` |
| E | Binary already committed in `bin/` |
| Ad | Download cut midway |
| Adv | Wrong `sha256`; plain HTTP; swapped binary |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 5.c-1 | Wrong-`sha256` file still lands in `bin/` | Fixture | **= 0 (and exit ≠ 0)** |
| 5.c-2 | Plain HTTP accepted | Fixture | **= 0** |
| 5.c-3 | Swapped committed binary still executed | Fixture | **= 0 (refused at execution time)** |

#### L2.5.d — Writes or side effects outside the boundary

| L3 | Context |
|---|---|
| N | Ordinary `check` |
| E | `check --run-bindings` |
| Ad | `.vanguard/` is read-only |
| Adv | Agent uses `check` as a re-anchoring path |

| L4 | Metric | Oracle | Threshold |
|---|---|---|---|
| 5.d-1 | Bytes written by `check` (including `--run-bindings`) | Read-only FS + tree diff | **= 0** |
| 5.d-2 | A `check --record` or equivalent exists | CLI audit | **= 0** |

---

## 5. Stopping rules (per the skill)

**Horizontal stop (freezing a layer):** the Attacker fails **K = 3** consecutive rounds to build a valid counter-example, **and** each child set has 3–6 nodes. Current tree: L1 has 5 nodes; L2 has 6 / 6 / 4 / 4 / 4 nodes ⇒ within range. If the Attacker finds a sixth pillar, check whether layers are being mixed (more than 7 is an alarm).

**Vertical stop (when to make a leaf):** an L4 node is a leaf when it simultaneously has (1) an Oracle + binary threshold, (2) two independent agents measuring the same result, (3) splitting further would not change the corrective action. A node missing any of the three must be decomposed further.

---

## 6. The Decomposer / Attacker / Referee loop

```
[Decomposer] ── child set L_k ──> [Attacker] ── counter-example ──> [Referee]
      ^                                                                │
      └──────────── valid: fix, redo ─────────────┬────────────────────┘
                                                  └─ unrealistic: reject Attacker, +1 clean round
```

Each round is recorded in the node's `verification_protocol`: `method`, `attacker_rounds_passed`, `referee_verdict`. Node status: `DRAFT → UNDER_ATTACK → FROZEN | LEAF`.

**Node schema (required):**

```yaml
node_id: "L2.1.b_ANCHOR_RESOLUTION"
level: 2
parent_id: "L1.1_SEMANTIC_CORRECTNESS"
dimension: "Failure mechanism / history rewrite"
statement: "An orphaned or history-shifted anchor makes status differ between resolution paths."
justification:
  sufficiency: "Same net-state ⇒ same status on all three paths."
  necessity: "Squash-merge is the common case; without this branch, status is wrong right after every squash."
status: "DRAFT"
verification_protocol:
  method: "Orthogonality Check"
  attacker_rounds_passed: 0
  referee_verdict: "PENDING"
children: ["L3.1.b_SQUASH", "L3.1.b_ABA", "L3.1.b_NON_ANCESTOR"]
```

---

## 7. Two known exceptions to the "0% deviation" rule

1. **The binding leaf (L2.5.a):** the spec itself declares that flaky tests break determinism *only at the `--run-bindings` boundary*. This leaf needs its own threshold (a re-run count) instead of 0%.
2. **The consistent-forgery leaf (2.d-6):** if the attacker computes every hash correctly, it cannot be detected by machine. No binary Oracle can exist ⇒ **not a measurable leaf**; moved to the Risk Register.

---

## 8. Traceability to DIP milestones

| Milestone | L4 leaves that must be green | Gate |
|---|---|---|
| **Before M0** | Run L0 → L2 (no code needed) | L0, L1 `FROZEN` |
| **M0** | 1.1.c-1/-3, 3.b-1, 3.d-1; 1.1.a-1 on a `sha256` repo (0.10) | Go/no-go memo cites L4 nodes |
| **M1** | 1.1.a-*, 1.1.d-2, 1.1.e-1, 2.b-1; scope-hash/pathmatch/normalization vectors | 100% of vectors on Windows + Linux |
| **M2** | 1.1.f-*, 2.f-1, 2.f-2 | Discovery matrix + resource caps green |
| **M3** | 2.a-*, 2.c-*, 2.e-*, 4.a-*, 2.d-1 (Tier A) | Refusal matrix + CAS race green |
| **M4** | 1.1.b-*, 1.1.d-3, 1.1.e-2/-3, 4.b-*, 4.c-*, 3.c-*, 2.d-2..5 (`verify`) | Squash-merge suite + determinism harness green |
| **M5** | 5.a-* … 5.d-* | `binding_failed` never mixes with `stale` |
| **After M4** | Squash-merge pilot (task 4.7) | Stale-after-merge rate measured |

---

## 9. Pre-registered attacks for the Attacker (round 1)

| Layer | Attack | Question for the Referee |
|---|---|---|
| L1 | Sufficiency | Is `fresh` still wrong when all 5 pillars pass? (Candidate: observe without review) |
| L1 | Necessity | If L1.5 is removed, does L0 still hold? |
| L2 | Orthogonality | Do 2.1.a (scope hash) and 2.1.c (dirty) share a root cause in "content normalization"? Can 2.2.c (lost lock) and 2.2.d (forged lock) be merged? |
| L2 | Orthogonality | Is 2.2.f (config tampering) truly distinct from 2.2.d (forged lock)? |
| L3 | Boundary shift | Repo 10× larger; disk latency +200 ms; 500 concurrent `observe`; empty commit vs complex merge |
| L3 | Boundary shift | Non-UTF-8 paths; upper/lower-case file names on a case-insensitive FS |
| L4 | Reproducibility | Two agents running the same oracle: on which leaf is the deviation > 0? (Suspect: 3.b-1, hardware-dependent ⇒ needs a standardized reference runner) |
| L4 | Actionability | When 2.d-2 FAILs, does the engineer know which entry to fix? |

---

## 10. Risk Register and gaps in the spec itself (found while applying the skill)

| # | Description | Handling |
|---|---|---|
| **R1** | A fully consistent lock forgery (correct hashes + a commit) cannot be detected by machine | Stated honest limit; mitigated by CODEOWNERS, branch protection, signed commits, `verify --base`. Cryptographic signing is a post-v1 candidate |
| **R2** | `observe` without review (including by an agent with a shell) | Already recorded in the spec; mitigated by PR review and `lock_bulk_change` |
| **R3** | Emptied-lock is an accepted false-negative | `--strict` catches it; tested exactly once |
| **R4** | `observed_at` is forgeable (committer date, `--at`) | Secondary signal only; never used as sole evidence |
| **G1** | The spec has **no** `verify` command and no bit 64; hand-edit detection is only scattered advisories (canonical form, future guard) | Ratify `lock-integrity-section.en.md` into the spec before M3 |
| **G2** | The spec does not require **fuzz/property testing** for parsers (frontmatter, glob, lock) despite the DoS guard | Add to DIP M1/M2 |
| **G3** | No numeric budget for recovery walk or large enumeration (only the "provisional" 2.0 s for dirty detection) | Fix in M0 (0.8), then feed into L4 3.b-2 |
| **G4** | The spec does not say how **adversarial-round results are recorded** (where the artifact lives) | Store `.vanguard/docs/quality/criteria-tree.yaml` (validated by `.vanguard/schemas/criteria-tree.schema.json`) + round log; the Referee signs `referee_verdict` |
| **G5** | The canon specifies stat-mismatch + byte-compare dirty detection; the implementation delegates to `git status --porcelain` (phantom-dirty under autocrlf) | Amend the canon or re-anchor leaf 1.1.c-3 before M0 |

---

## 11. Next steps

1. Run the Decomposer–Attacker–Referee loop for **L1** (table in section 9). Freeze L0 and L1.
2. Export `.vanguard/docs/quality/criteria-tree.yaml` using the node schema in section 6, from the tables in section 4.
3. Ratify gaps G1–G4 into the spec, then start M0.
4. Turn each L4 leaf into a test/harness with an ID (`Q-1.1.a-1`, ...) and wire it into CI per section 8.
