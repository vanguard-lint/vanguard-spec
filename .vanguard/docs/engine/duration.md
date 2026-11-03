---
id: duration
schema: package-contract
watches:
  paths:
    - "vanguard/internal/duration/**/*.go"
  review_after: 30d
---

# duration — The Two Duration Grammars

Parses the declared `review_after` grammar and the engine tunable grammar.
The review_after grammar is an integer plus exactly one unit — h, d (default
for a bare integer), w (=7d), mo (=30d), y (=365d) — with fixed-length
conversions so determinism beats calendar exactness. Tunables
(`clock_skew_tolerance`, per-binding timeout) use the standard Go duration
grammar, which additionally accepts sub-hour units.

## Interface

- `Parse(s string) (time.Duration, error)` — the declared grammar; rejects
  negatives, floats, seconds, multi-unit, and case-mismatched units
  [duration.go:Parse].
- `ParseTunable(s string) (time.Duration, error)` — `time.ParseDuration` for
  engine tunables (accepts `5m`, `600s`, `2m30s`) [duration.go:ParseTunable].

## Invariants

- The conversion table is fixed-length: h=1h, d=24h, w=7×24h, mo=30×24h,
  y=365×24h [duration.go:Parse].
- A bare integer defaults to days [duration.go:Parse].
- `mo` matches before single-letter suffixes; anything with an unrecognized
  suffix fails [duration.go:Parse].
- Unit matching is case-sensitive and exact (`1D` and bare `mo` are errors)
  [duration_test.go:TestRejections].

## Side effects

None — pure parsing.

## Verification

7 parse cases (90d, 24h, 2w, 1mo, 1y, bare 7, invalid minutes), tunable
cases, and 8 rejections [duration_test.go].
