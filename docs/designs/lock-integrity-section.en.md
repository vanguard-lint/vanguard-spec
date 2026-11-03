## Lock & store integrity — detecting hand-edits and sabotage

> **Proposed location in the spec:** after *Anchoring* (the "Lock loss" part), before *Tests bindings*.
> **Status:** proposed addition for v1. To be ratified together with M3 (lock) and M4 (check).

### Threat

Two kinds of hand-editing, handled differently:

| Kind | Example | Vanguard's goal |
|---|---|---|
| **Accidental** | Hand-editing `vanguard.lock` "to go faster", a merge conflict resolved wrongly, an editor changing line endings, deleting an entry by mistake | **Detect reliably**, report exactly where it diverges and which command fixes it |
| **Deliberate** | An agent or user plants forged `scope_tree_hash` / `content_hash` values so a document shows `fresh`; narrows `watches.paths`; edits the `bindings:` allowlist | **Make it hard and make it visible in review**; never promise to prevent it |

### Honest limit (stated, not hidden)

The lock is a plain text file with no secrets. Anyone who can write the lock and commit it can recompute every hash, including any self-contained checksum. Therefore:

* Vanguard **does not claim** to stop an attacker who can both write and commit. This is the same class of limit as "`observe` without real review" (see *Honest limit — attestation is only as good as the attester*).
* The final protection is **process**: lock changes show up in PR diffs; CODEOWNERS and branch protection on `.vanguard/`; signed commits; CI running `verify` against a base ref.
* What Vanguard can do is **turn hand-edits into something measurable and traceable**: every divergence between the lock and the truth in git is exposed, and every forgery has to pass a human review gate.

Cryptographic signatures (private keys) are **excluded from v1**: they need key management, break the "no daemon, no external service" principle (NG4), and merely move the problem to "who holds the key". Recorded as a post-v1 candidate (see *Alternatives Considered*).

### Detection mechanisms — three tiers, cheap to expensive

#### Tier A — Cheap checks, run in every `check` (advisory)

No history scan is needed. Cost is O(lock size) plus one comparison against HEAD.

| Code | Condition | Meaning |
|---|---|---|
| `lock_noncanonical` | `canonical(parse(file)) ≠ file`, byte for byte | The file was not written by `observe` (id order, field order, indent, newline). **The strongest signal of a manual hand-edit**, since `observe` always writes the canonical form |
| `lock_uncommitted` | Lock in the worktree differs from the lock blob at HEAD (git mode, store inside the repository) | The lock is modified but uncommitted. An attestation not in git cannot be reviewed. Hint: *"commit the lock, or `git checkout -- .vanguard/vanguard.lock` to revert"* |
| `lock_entry_shape` | Entry missing required fields (`observed_at`, `content_hash`), mixes `anchor` with `scope_snapshot`, or `scope_tree_hash` has the wrong kind prefix | The entry is not one of the three valid shapes. Treated as corrupt for that entry only |
| `lock_orphan_entries` | Entry whose id is no longer in discovery | Advisory already exists; unchanged |

Severity: **advisory warning** in the envelope's `violations[]` (code in column 1, with `file` + `hint`). No exit bit by default, so bootstrap is not broken. `--strict` escalates them to **bit 8**.

#### Tier B — Cross-check against git: `vanguard verify` (opt-in, read-only)

A new command that **writes nothing**, like `check`. Use it when something looks off, and run it in CI. For each git-mode entry it checks **invariants that an honest observe always satisfies**:

| Code | Invariant | How it is checked | Why a naive forgery is caught |
|---|---|---|---|
| `anchor_unknown` | `anchor` is a commit present in the ODB, or at least the scope hash matches a commit on the first-parent chain (the existing recovery path) | ODB lookup; otherwise the recovery walk | A made-up OID does not resolve and matches no commit |
| `scope_hash_mismatch` | `scope_tree_hash` **equals** the scope hash computed at the `anchor` commit itself | Recompute with the same strategy (subtree / enumeration) on the `anchor` tree | Editing `scope_tree_hash` without changing `anchor` mismatches immediately |
| `scope_count_mismatch` | `scope_file_count` equals the file count of the scope at `anchor` | Count on the `anchor` tree | A planted number |
| `content_hash_mismatch` | `content_hash` equals the normalized hash of **the document at the commit that put this entry into the lock** | Find the latest commit `L` that changed the entry in the lock's history (bounded, default 200 lock commits); take the document blob at `L`, normalize, compare | Editing `content_hash` to "catch up" with a changed document mismatches the document committed alongside |
| `anchor_not_ancestor_of_lock_commit` | `anchor` is an ancestor of (or equal to) commit `L` | Reachability | An anchor pointing "into the future" relative to the attestation itself |
| `observed_at_implausible` | `observed_at` does not exceed `commit_time(L) + clock_skew_tolerance`, and is not earlier than the entry's previous `observed_at` (monotonic) | Time comparison | Only a **secondary signal**: committer dates are forgeable (see *Clock source*), so never evidence on its own |
| `lock_commit_without_doc_trace` | Commit `L` changes entry X with no earlier document commit and an identical `content_hash` | Informational, not an error: re-attesting an unchanged document is valid (the review act). Reported only so the reviewer knows | — |

History-based checks (`content_hash_mismatch`, `observed_at_implausible`) need lock history. A **shallow clone** missing history returns `skipped` with the hint *"unshallow to check fully"*, **never silently counted as a pass** (the loud-never-silent principle).

Provider: GitProvider only. In HashProvider mode there is no history, so `verify` runs Tier A and the `scope_snapshot` checks against current files, and states clearly which areas could not be checked.

**Exit bit:** a **dedicated bit, `64` = `lock_integrity`**, set only by `verify` (and by `check --verify-lock`). Reason for a separate bit: a forged lock is **not** staleness (bit 2) and not a document structure error (bit 1). Same philosophy as bits 16 and 32: one condition per bit, so CI can gate independently.

#### Tier C — Diff against a base ref: `vanguard verify --base <ref>` (CI for PRs)

This tier answers the question human reviewers need: **"are this PR's lock/config changes reasonable?"** It compares HEAD with `<ref>` (usually `origin/main`):

| Code | Rule | Example caught |
|---|---|---|
| `lock_entry_changed_without_cause` | Entry X changed `anchor`/`scope_tree_hash`/`content_hash` but there is **nothing to attest**: the scope is unchanged, the document is unchanged, and it is not a valid review act (only `observed_at` + `anchor` changed) | Bulk hash changes with no accompanying code/doc commit |
| `lock_entry_removed` | An entry is removed while its id is still in discovery | Deleting an entry to turn `stale` into `unobserved` (and slip past default CI) |
| `lock_bulk_change` | Number of entries changed in one PR exceeds a configurable threshold (default 5) | A disguised `observe --all` sweep; the threshold only warns |
| `watch_narrowed` | A document's `watches.paths` is narrower than at base (fewer files match, or a glob was removed) | Narrowing scope to silence staleness (already caught via `content_hash`; this code makes the reason explicit to the reviewer) |
| `config_changed` | `vanguard.yaml` differs from base: `bindings:` allowlist, discovery globs, `clock_skew_tolerance`, `target.path`, schema definitions | Adding a command to the bindings allowlist; loosening `clock_skew_tolerance` to let a forged `observed_at` through |
| `schema_changed` | A file under `schemas/` changed | Loosening a schema so an invalid document passes |

`config_changed` and `schema_changed` are **information for the reviewer**, not errors: changing configuration is legitimate. Their value is making "who changed the rules of the game" appear in a single place. For the `lock_*` codes and `watch_narrowed`, the default severity is bit 64; `config_changed` and `schema_changed` only print, unless `--strict` is given.

### Command surface (additions to *Check command surface*)

| Invocation | Behavior |
|---|---|
| `vanguard verify` | Tiers A + B, read-only, writes nothing. New envelope mode: `verify` |
| `vanguard verify --base <ref>` | Adds Tier C. `<ref>` resolves in the repository that contains the store |
| `vanguard check --verify-lock` | Runs Tiers A + B within the same `check` (convenient for one-command CI) |
| `--max-lock-history <n>` | Bound on how many lock-history commits are walked, default `200` |

General rule: `verify` **never writes**, never opens a socket, never spawns a process. A flag not applicable to the current mode is a usage error (same rule as `--strict` ∘ `--from`).

### Envelope impact

Each finding added to `violations[]`:

```json
{
  "rule": "lock.scope_hash_mismatch",
  "file": ".vanguard/vanguard.lock",
  "line": 7,
  "message": "payments-spec: scope_tree_hash does not match the scope at anchor e4f5a6b7…",
  "hints": ["restore the entry: git checkout -- .vanguard/vanguard.lock, then re-run: vanguard observe payments-spec"]
}
```

* `line` comes from the position in the lock (the lock is canonical, so line numbers are stable).
* The `verify` mode uses the same envelope (`schema: 1`) and adds a store-level field `lock_integrity: {checked, skipped[], findings}` so agents can read results without parsing prose. `skipped[]` lists checks that were not run and why (shallow, HashProvider).
* The bit table gains: `64` = `lock_integrity`, set only by `verify` / `check --verify-lock`. Example: `stale + lock_integrity` → `2 + 64 = 66`.

### Special cases

| Situation | Handling |
|---|---|
| Lock hand-edited into a correct canonical form, hashes match the anchor, clean commit | **Not detectable by machine**; only Tier C (diff vs base) and human review see it. This is the honest limit above |
| Merge conflict in the lock resolved by hand | Usually lands in `lock_noncanonical` or `scope_hash_mismatch`. Recommendation: resolve by re-running `observe` on the merged tree (already stated in *Lock merge ergonomics*) |
| `vanguard.lock` deleted or emptied | Dedicated mechanism already exists (*lost lock*, emptied-lock). Tier C adds `lock_entry_removed` so the emptied-lock case becomes visible at the PR level |
| Agent edits `docs/` instead of the lock | Already caught by `content_hash` → `document_changed`; no new mechanism needed |
| Agent edits `schemas/` or `vanguard.yaml` | Visible only through Tier C (`schema_changed`, `config_changed`); the main protection is CODEOWNERS |
| Store outside the repository (decoupled) | `lock_uncommitted` and Tier C use the repository **containing the store**, not the target. Anchor/scope checks still use the target's ODB |
| Store lock is not in a git repository | Tier A runs; Tiers B/C are `skipped` with a hint (no history to cross-check) |

### Operational recommendations (not engine features)

1. **CODEOWNERS** for `.vanguard/vanguard.lock`, `.vanguard/vanguard.yaml`, `.vanguard/schemas/`: a reviewer other than the PR author.
2. **Branch protection + signed commits** on the main branch: turns "who committed this lock" into a cryptographic fact at the git layer, where key management already has an owner.
3. **Two CI commands**: `vanguard check --strict --verify-lock` and `vanguard verify --base origin/main`; gate with `exit_code & 64`.
4. **Never** give agents direct write access to protected branches; agents only open PRs.
5. Never enable `--run-bindings` on PRs from forks (already in the *bindings trust model*).

### Non-goals of this section

* **No key-based signing of the lock** (excluded from v1, recorded in *Alternatives Considered*).
* **No automatic lock repair** when a mismatch is found: `verify` only reports and suggests a command. Repair is a deliberate act, consistent with D7 (check never writes).
* **No self-check field in the lock** (e.g. `entry_digest`): a hand-editor can recompute it, so it gives false comfort and adds merge conflicts. The canonical form already gives an equivalent signal at no extra field cost.

### Additions to *Alternatives Considered*

| Alternative | Outcome and reason |
|---|---|
| Cryptographic signature (GPG/sigstore) for the lock | *Deferred post-v1* — needs key management and breaks "offline, no external service"; the equivalent solution lives at the git layer (signed commits + branch protection) |
| Self-contained per-entry checksum (`entry_digest`) | *Rejected* — does not stop a deliberate attacker (recomputable), only adds conflicts. Canonical form + cross-checking against git gives a stronger signal |
| `check` automatically running the full history cross-check | *Rejected* — violates the hot-path budget (revwalk over lock history). Cheap Tier A runs by default; Tiers B/C are opt-in |
| Letting `check` repair the lock when it sees a mismatch | *Rejected* — violates D7; `verify` only reports |

### Mapping into the quality criteria tree (Epistemic Decomposition skill)

This branch belongs under **L1.2 State Integrity & Durability**, adding the following L2:

| L2 (failure mechanism) | L3 (triggering context) | L4 `[Metric] + [Oracle] + [Threshold]` |
|---|---|---|
| Lock hand-edited accidentally (format, missing entry) | **Edge:** hand-edit mid-file, id order changed, CRLF, comma deleted | Share of simulated hand-edits caught by `lock_noncanonical` or `lock_entry_shape` · oracle: lock mutation fixture scripts · **100%** |
| Lock hashes forged without changing the anchor | **Adversarial:** edit `scope_tree_hash`, `scope_file_count`, `content_hash` field by field | Share of forgeries caught by Tier B · oracle: `git` CLI computing hashes independently at the anchor · **100% of cases that do not simultaneously edit `anchor` + doc + hashes consistently** |
| Lock forged consistently (anchor edited + matching hashes) | **Adversarial:** attacker computes every hash correctly | Tier C catches it when there is no matching code/doc change · oracle: diff against base ref · **100% of `lock_entry_changed_without_cause` cases**; the remaining cases are recorded as a limit, not measured |
| Shallow clone silently skips history checks | **Adverse:** shallow depth=1 | Number of `skipped` checks without a hint · **= 0** |
| `verify` writes or opens the network by accident | **Nominal:** every mode | Bytes written, socket syscalls · oracle: network-less sandbox, read-only directory · **= 0** |

Every node above must pass the Orthogonality Check (L2) and the Reproducibility Test (L4) in the Decomposer–Attacker–Referee loop before it is FROZEN.
