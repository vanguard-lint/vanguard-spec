# EGM Goldfish Verification — Wave 1 (2026-11-04)

Protocol: 5 fresh-context reviewers (no conversation history, no access to prior drift reports), each judging `docs/designs/vanguard.md` against `vanguard/` code only. Compiled + deduplicated below. Evidence: doc L# (true file lines, 1,084 total) + code file:line.

## Verdict

**Medium risk, 3 Blockers / ~12 Major / ~15 Minor+Nit.** The doc's core is sound (hierarchy, tables, sync, deps, exit bits, lock schema, hint texts all verified clean), but the honest ledger itself contains a false row, and ~10 present-tense claims still describe unimplemented behavior without inline flags.

## Blockers

| # | Finding | Evidence |
|---|---|---|
| B1 | **Ledger row "structure-verification: Not started" is FALSE** — a structure phase ships: `internal/structure` (schema load, required_sections, ordering, duplicates, citations) enforced at `engine.go:93-116` (missing `schema:` fatal), surfaced into `violations[]` (`check.go:64-77`), gating `observe` (`observe.go:137-148`). The parenthetical "violations[] carries only lock-entry parse failures" is also false. Shipped mechanism deviates from the body's design (schema files + `x-vanguard` block + regex heading scan — no goldmark, no JSON Schema 2020-12, no dsl), so doc+ledger jointly misdescribe real behavior | `structure/structure.go:39-181`, `engine/engine.go:93-116`, `check/check.go:64-77`, `observe.go:137-148` |
| B2 | **D9 (L73) vs L342 contradiction on `schema:`**: D9 says "omitted field is a load-time fatal"; L342 says "missing `schema:` is valid for markdown, json, and yaml — optional for every format". Code enforces the FATAL (`engine.go:98-100`) → L342 is the false statement | `engine/engine.go:98-100` |
| B3 | **`--max-recovery-walk` is a no-op**: accepted but never wired (`main.go:167` `_ = maxRecovery`); `Selection.MaxRecoveryWalk` never populated; `recoveryBound` always 1000 | `main.go:113-122,167`, `check.go:307-312` |

## Major

1. **Ledger monopoly violated + incomplete** (claims "one source of truth", L88): no rows for `install`/provisioning (L594-605 — zero code), bindings execution status, `--output`, `cmd:` shorthand, process-tree kill, `.vanguard/bin/` PATH, store-inside-scope fatal validation (L377-387), doc-watches-own-store advisory (L375), check-side future-`observed_at` guard (L628). Status prose duplicated at L561 + deps table L893-894.
2. **Present-tense claims of unimplemented behavior without inline flags**: command table L685 (phase-1 structure), observe precondition L391, CUJ 2 L113, `--no-color` L697/L860/L881, 10k advisory L645, orphan advisory L639, `max_depth` L947, `introducing_commits` L785 (needs the inline-deviation pattern that L561 models).
3. **Lock-lost definition drift**: code counts unwatched docs (`engine.go:146-148`); doc closed definition (L486/L786/glossary) says "every discovered document **reports unobserved**".
4. **Lock kind prefix `blob:` missing** from L465/L494 enumeration (code emits it for single-file watches, `provider.go:172-173`).
5. **`--repository-path` flag declared (L649), absent in code** (only `target.path` + store-parent fallback).
6. **`check <file>` path resolution absent**: code resolves only exact doc id (`check.go:334-337`); declared chain + hint truncated.
7. **`vanguard install` declared (D3, L600-605), does not exist** — and NG7/CUJ5 argue it serves no v1 journey (zoom-out: defer).
8. **Attribution traversal**: first-parent-only walk (`provider.go:503`) vs doc's `anchor..HEAD` revwalk; `AnchorReachable` full-ancestry fast path can over-collect to root post-merge.
9. **Committed-doc precondition checks the wrong repo/path**: hardcoded `".vanguard/docs/"` against the TARGET provider (`observe.go:203,263-265`); decoupled store silently passes; HashProvider target skips entirely.
10. **`--format text|json|yaml` omits `pretty`** (L695 vs L860).
11. **`--run-bindings` + range mode silently ignored** vs doc's "inapplicable flag = usage error" (L691 vs `main.go:149`).
12. **CAS internals undocumented**: `.cas` token file + 2s retry + 10s steal (`lockfile.go:22-54`) vs doc's "no retry loop, no merge logic" (L531).

## Minor / Nit (selected)

- Observe envelope: no hints emitted (L835); refusal reasons differ from closed list (L844: code says "structure violations"/"config error"); bindings `pending` not emitted for unobserved docs (L779 vs `check.go:151-157`).
- unobserved hint text differs (L488 vs `check.go:155`); committed-refusal hint missing parenthetical (L393).
- Windows console-mode UTF-8 claim (L858) — no code.
- Style: "the engine's only external process" false as written (bindings spawn processes); "loud-only error budget" undefined; TTY/color rules duplicated L858 vs L881; hyphen-vs-em-dash in L858/L860 (5 spaced hyphens vs 478 em-dash convention); "honest/promise" self-framing; "scope/scheme change" undefined slash term; "gray-purple" imprecise.
- Structural: status vocabulary fragmented (Not started / Deviates / shipped / planned / post-v1 / pending / "first scheduled v1.1 feature"); bit-64 in ledger breaks closed exit-bit vocabulary; decided item sits under Open Questions (L56); `&#124;` HTML entity L695; unbalanced paren in `decide()` block L728.
- Comprehension: EGM/C4/ODB/DIP undefined at first use; forward references in Goals; `output_parser` mentioned once, unspecified (L296).

## Verified clean (no drift)

Command-surface flags (except B3/#5/#6/#10), constants table, exit-bit table, envelope fields + sort contracts + refusal bit mapping, lock field names + canonical form + CAS message, stale/lock-lost/kind-mismatch/document-changed/future-guard hint texts, dependency table vs go.mod, **byte-identical sync** source↔attested copy, lock-integrity banner accuracy (all 3 goldfish that checked it), heading hierarchy, 18 tables well-formed, frontmatter conventions.

## Fix wave — split

**Wave A (mechanical, no decisions needed):** fix ledger row structure-verification (B1) + add ledger rows for install/bindings/etc (1) + inline "(not yet implemented — see ledger)" flags at all present-tense sites (2) + lock-lost wording (3) + `blob:` enumeration (4) + `pretty` in --format row (10) + style/structural nits.

**Wave B (decisions needed from owner):** B3 (wire flag vs remove from surface), #5/#6 (implement resolution vs amend doc), #7 (defer install per zoom-out), #8 (traversal contract), #9 (precondition repo), #11 (usage error), #12 (document CAS or simplify), zoom-out YAGNI block (braces, yaml, pretty scope, dsl, `--output`, `--max-recovery-walk`, `clock_skew_tolerance`, `cmd:` shorthand, `sha256:` legacy).
