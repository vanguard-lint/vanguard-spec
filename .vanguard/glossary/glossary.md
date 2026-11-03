# Vanguard Glossary

Single source of shared vocabulary for every vanguard document: the design
doc (`docs/designs/vanguard.md`), the README, the website, and agent-facing
rules. Use the exact form from the **Term** column — Vale enforces this table
in continuous integration. When a term is missing here, add it here first,
then use it.

| Term | Definition |
|---|---|
| **anchor** | The commit at which a document was last attested by `vanguard observe`; stored in the lock, advanced only by observe. An orphaned anchor is recovered by the documented procedure, never silently. |
| **attestation** | The observe act: a human (or an `--at`-timestamped CI run) asserts that a document is accurate as of that moment. Attestation is always explicit and recorded in git. |
| **baseline** | The lock entry's recorded state — anchor, scope hash, content hash — that `vanguard check` compares against. |
| **binding** | A user-declared verification command in the `vanguard.yaml` allowlist. Executed only under `check --run-bindings`; its exit code is evidence, its result is never a staleness signal. |
| **churn** | Change inside a watch scope. Uncommitted churn is the dirty axis; committed range churn is the stale axis. A binding outcome is never churn. |
| **dirty** | A watch scope whose worktree differs from HEAD. Blocks re-anchoring; reported through exit bit 4. |
| **document** | A specification file discovered by the discovery globs, carrying frontmatter with an engine-owned `id` and optional watches. |
| **envelope** | The structured output of `check` and `observe` — `schema`, `mode`, `exit_bits`, `documents[]`, and friends — identical in shape across `json` and `yaml`. |
| **exit bitmask** | The OR-composable set of per-condition bits: 1 fatal, 2 stale, 4 dirty, 8 strict gaps, 16 binding failed, 32 affected in range. CI consumes the mask without parsing prose. |
| **fresh** | A document whose current scope state matches its attested baseline. The only state that clears without human action is one where nothing watched changed. |
| **gap** | A category with no attestation coverage: `unwatched` (no declared watches, store-level listing only) or `unobserved` (declared watches, no lock entry — a legal per-document status). |
| **lock** | The committed file `vanguard.lock`: canonical, byte-stable JSON recording every observed document's baseline. Absent lock plus full unobserved coverage is a lost lock and degrades to stale by default. |
| **net-state rule** | Staleness is decided by comparing scope tree state, not by reading history. History (revwalk) exists only for attribution; an A→B→A window is fresh. |
| **observe** | The command that attests a document: verifies structure and a clean scope, then records the new baseline in the lock. |
| **phantom churn** | A false dirty report caused by line-ending or filter artifacts in git status. vanguard's dirty detection is built so phantom churn can only be loud (false dirty), never silent (false fresh). |
| **revwalk** | Git revision walking over commit history, used solely to attribute which commit churned a watched file or orphaned an anchor. |
| **scope** | The union of a document's watch globs, hashed as a whole. The `.vanguard/` store directory never belongs to any scope. |
| **scope tree hash** | The Merkle-style hash of a scope: a subtree OID for filter-free globs, an enumerated hash otherwise. Deterministic across platforms by construction. |
| **stale** | A document whose scope changed after its anchor, or whose `review_after` deadline passed. The core alarm; exit bit 2. |
| **store** | The `.vanguard/` directory holding configuration, schemas, and the lock; committed with the repository by default. |
| **target** | The repository vanguard runs against, resolved through providers so the store may live in a separate repo. |
| **unobserved** | A document with declared watches but no lock entry: legal, reported loudly as a gap, never silently fresh. |
| **verdict** | The per-document outcome of `check`: exactly one of fresh, stale, dirty, unobserved, or a fatal structural error — never a vague warning. |
| **watch** | A declared path glob (plus optional `review_after`) binding a document to the code it describes. |

## Word list quick reference

| Use | Not |
|---|---|
| vanguard, vanguard.lock, vanguard.yaml, vanguard check | `Vanguard-lock`, `the lockfile` alone when naming the file |
| lockfile (generic, code-level) | `lock-file`, `lock file` |
| watch, watches | `watcher`, `watched file list` |
| scope, scope tree hash | `watched scope-tree`, `scopehash` |
| verdict | `result state`, `status code` (for the per-document outcome) |
| exit bitmask, exit bit | `exit code bits`, `return-code mask` |
| GitHub | `Github`, `git hub` |
| repository | `repo` |
| CLI | `C.L.I.`, `command line interface` (spelled out mid-sentence) |
