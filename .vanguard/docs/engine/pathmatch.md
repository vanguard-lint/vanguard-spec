---
id: pathmatch
schema: package-contract
watches:
  paths:
    - "vanguard/internal/pathmatch/**/*.go"
  review_after: 30d
---

# pathmatch — Closed Glob Syntax and the --path Decision

The v1 closed glob grammar and its consumers: one-level brace expansion,
canonicalization, scope-hash strategy classification, segment matching, and
the filesystem-free `CouldClaim` reverse lookup used by `check --path`.
Matching is case-sensitive over slash-separated paths; `**` is
whole-segments-only; dotfiles match like any file.

## Interface

- `Expand(glob) ([]string, error)` — one level of brace alternatives, each
  canonicalized, deduplicated, in declaration order [pathmatch.go:Expand].
- `Canonical(glob) (string, error)` — normalize and validate: strips `./` and
  trailing `/`, bare directory forms become `dir/**`, `.` becomes `**`,
  collapses consecutive `**` [pathmatch.go:Canonical].
- `Classify(canonical) Strategy` — `**` → RepositoryWide, filter-free `dir/**`
  → Subtree, anything with a filter → Enumerated [pathmatch.go:Classify].
- `Match(canonical, path) bool` — segment matching; `**` absorbs any run of
  segments including zero [pathmatch.go:Match].
- `CouldClaim(canonical, p, pIsDir) bool` — the `--path` decision procedure;
  directory arguments use segment reasoning with no filesystem access
  [pathmatch.go:CouldClaim].

## Invariants

- Closed grammar: `*`, `**`, `?`, classes, one brace level, backslash
  escapes; negation with `!`, fused `**`, nested braces, empty alternatives,
  and unterminated constructs are fatal [pathmatch.go:validate].
- Class negation must use `^`; `[!` is rejected and `[]` never matches
  [pathmatch.go:classEnd].
- Equivalent spellings classify identically, so they hash the same scope
  [pathmatch.go:Classify].
- Directory decision rules: glob exhausted while the path continues → false;
  path consumed → true; `**` absorbs the remaining segments; a literal
  mismatch prunes [pathmatch.go:couldClaimDir].

## Side effects

None — pure string logic.

## Verification

The 11 mandatory `decide()` vectors (`TestPathVectors`), 10 normalization
spellings, 4 classification outcomes, brace expansion, 9 rejections, escape
semantics, 16 match cases, and dotfile matching [pathmatch_test.go].
