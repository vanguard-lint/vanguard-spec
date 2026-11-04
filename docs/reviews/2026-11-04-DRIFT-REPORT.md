# Drift Report — 2026-11-04

Target: `docs/designs/` vs `vanguard/` (Go engine). Code state: working tree (uncommitted session changes included — design doc L449/L478 scheme-tag edits match the code's `sha256v2:` scope digests; no drift reported for those).

## vanguard.md

- **Missing Feature — `--no-color` flag and `NO_COLOR` env** (L679, L862). Design: "disable ANSI colors (also disabled automatically when piped, or when `NO_COLOR` is set)". Code: no `--no-color` flag and no `NO_COLOR` handling anywhere in `cmd/vanguard/` or `internal/` (only `CLICOLOR_FORCE` is read, in `render_pretty.go`).
- **Missing Feature — colored default `text` output** (L840, L860: "color-highlighted report… STALE red, DIRTY yellow, FRESH green"). Code: `renderText` emits plain, colorless lines; no status glyphs, no ANSI. All colors currently live in the new pretty renderer only.
- **Missing Feature — hash-mode >10,000-file advisory on observe** (L627, L688: "Observe emits an advisory warning when a hash-mode scope exceeds 10,000 files and suggests narrowing the watch"). Code: no such warning emitted by `observe`/`HashProvider` (the only 10,000 constant is `pathmatch.maxExpansions`, a different brace-expansion bound).
- **Missing Feature — `max_depth` ≤ 32 validation cap** (L693, L925: "declared max_depth — 32 (config validation)"). Code: no `MaxDepth`/depth-cap validation exists in `internal/config` or `internal/structure`.
- **Logic Shortcut — `introducing_commits[]` ordering** (L767: "sorted oldest → newest (chronological reading order; equal-position ties break by OID lexicographically)"). Code: `sort.Strings(commits)` — plain OID-alphabetical, no chronological ordering anywhere.
- **Missing Feature — orphaned lock-entry advisory on every check** (L621: "Existing orphans flagged as an advisory warning on every check"). Code: `check` emits no orphan-entry warning; only the unrelated `anchor_orphaned` envelope field exists.
- **Code feature absent from design — `--format pretty`** (`cmd/vanguard/main.go` L161/L212; `internal/check/render_pretty.go`). The design's "Output formats and terminal presentation" (L828–863) never mentions `pretty`; its constants table and encoding rules don't cover the pretty palette, `COLUMNS`-based responsive width, centering, or the off-terminal `pretty → text` degradation. Consequently L840's "ANSI styling applies to `text` only" is now false in both directions: text carries no ANSI, and pretty does.
- Consistent (verified, not drift): lock schema `sha256v2:` kind tags (L449/L478 vs `scope.go`), single-file `blob:` hashing, lock-lost "full discovery set" semantics (L774 vs `engine.lockLost(unobserved, unwatched)` — a never-observed unwatched doc reports `unobserved`), envelope `"schema": 1` (L767 vs `envelope.go` `SchemaVersion = 1`), `observe --prune` standalone semantics (L621 vs `main.go`/`observe.go`), `--max-recovery-walk` 1000 (L674 vs `check.go` L311), clock-skew 5m, frontmatter 256 KiB cap, binding timeout 600s, merge-against-first-parent.

## design_draft.md

- Drafting history (AGENTS.md: `vanguard.md` wins). Carries the **same divergences as vanguard.md** — text was carried over verbatim: `--no-color`/`NO_COLOR` (L680, L862), 10,000-file advisory (L689), `max_depth` 32 cap (L694), `introducing_commits[]` oldest→newest (L768), no mention of `--format pretty`.
- No drift-remediation action needed here; fixes belong in `vanguard.md` only.

## discussion_summary.md

- **Superseded watch model (D3, L41–47)**: lists `refs:` and `docs:` watches (propagation). Reversed by `vanguard.md` D5 (L69: "exactly `paths` + `review_after`… `docs` and `refs` watches removed"); code parses only `paths` + `review_after` (`docmodel.go` L33).
- **Superseded anchoring (D4, L50)**: "Every CLI invocation lazily records an observation first… `vanguard stale` is the only command" and "no stored anchor field". Code: no lazy observe, no `stale` subcommand (`init`/`check`/`observe` only), explicit `anchor` stored per lock entry.
- **Superseded positioning (L13, L20)**: "standalone CLI that lives outside the codebase… ledger outside the repository". Actual: `.vanguard/` store inside the repo, committed by default (`vanguard.md` L186).
- Flagged so a naive reader does not treat D3/D4 as the current contract; the doc self-identifies as predecessor context for the draft.

## lock-integrity-section.en.md

- **Missing Features — entire proposal unimplemented.** The doc specifies: `vanguard verify` (Tier B), `verify --base <ref>` (Tier C), `check --verify-lock`, `--max-lock-history` (default 200), exit bit `64` = `lock_integrity`, envelope field `lock_integrity: {checked, skipped[], findings}`, Tier A advisory codes `lock_noncanonical` / `lock_uncommitted` / `lock_entry_shape` / `lock_orphan_entries`. Code: none of these exist — `main.go` registers only `init`, `check`, `observe`; no bit 64; no integrity codes.
- **Zombie-reference risk**: `quality_test.go` L551 already whitelists `verify` as a hint-referenced subcommand, anticipating a command that does not exist — a hint emitted with `vanguard verify …` would pass the hint test but fail at runtime.
- The doc self-marks "proposed addition for v1. To be ratified" and is **not referenced by `vanguard.md`** — an unratified proposal document sitting in the ratified designs directory, with zero code coverage.
