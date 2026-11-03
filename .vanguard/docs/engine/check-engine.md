---
id: check-engine
schema: package-contract
watches:
  paths:
    - "vanguard/internal/check/**/*.go"
  review_after: 30d
---

# check — The Engine Core

Two-phase check (structure fails closed during discovery, then per-document
freshness over the document, clock, and paths axes), the observe attestation
engine (the only lockfile-mutating path), binding execution, and the output
contract. All check modes (all/doc/path/range) plus observe emit one envelope
that is identical in JSON and YAML, with exit bits composing by OR.

## Interface

- `Load(storeDir, Options)` — reads config, target, provider (git with hash
  fallback), discovery, and the lock; a missing lock is an empty in-memory
  lock with `LockSeen=false`, a corrupt one is fatal [engine.go:Load].
- `Check(Selection) (*Result, error)` — dispatches to checkAll/checkDoc/
  checkPath/checkRange; unknown mode is a usage error [check.go:Check].
- `Observe(ObserveRequest) (*Result, error)` — attests documents; per-document
  refusals; a compare-and-swap conflict aborts the whole run with no partial
  write [observe.go:Observe].
- `Result{Envelope, Fatals, Refusals}` — one run's envelope plus run-level
  reports [check.go:Result]; `Selection` names the run [check.go:Selection],
  `ObserveRequest` the attestation [observe.go:ObserveRequest].
- Exit bits (compose by OR): fatal=1, stale=2, dirty=4, gaps=8 (`--strict`
  only), binding_failed=16, affected_in_range=32 [envelope.go:BitStale].
- `ApplyBindings(env)` / `RunBindings(docID, names)` — pending notation for
  referenced-but-not-run bindings; direct-argv execution of allowlisted
  commands, exit code is the only decision input
  [bindings.go:RunBindings, check.go:ApplyBindings].
- `Render(env, format)` — json, yaml, or text; unknown format is a usage
  error [render.go:Render].
- `Envelope` — mode, schema, exit_bits, documents[], unwatched[],
  unobserved[], affected_in_range[], lock_lost, hints[], lock, refused[],
  pruned[] [envelope.go:Envelope].

## Invariants

- Freshness precedence: stale (any axis) over dirty (uncommitted churn only)
  over fresh [check.go:evaluateObserved].
- The paths axis is the net-state rule: the scope hash at HEAD against the
  lock; history is attribution only [check.go:gitPathsAxis].
- Anchor attribution follows three paths — reachable, recovered within the
  bound (default 1000), orphaned [check.go:gitPathsAxis].
- Gap categories (unobserved, unwatched) set no bit by default; `--strict`
  elevates them to bit 8 [check.go:checkAll].
- Lock-lost: absent lockfile with every document unobserved sets bit 2 in all
  mode only [engine.go:lockLost].
- Attestation monotonicity: a timestamp before the previous observed_at is
  refused; an `--at` beyond clock_skew_tolerance in the future is refused
  [observe.go:attest].
- Observe preconditions: HEAD exists, watch scope dirty-free, document clean,
  non-empty scope — each refusal carries its reason and hint; dirty scope
  refusals also name the offending paths [observe.go:attest].
- `--all` attests only the currently unobserved set, never stale documents
  [observe.go:Observe].
- Bindings never interpolate a shell and their results never become
  staleness [bindings.go:RunBindings].
- Envelope arrays are sorted by id bytes [check.go:sortDocs].

## Side effects

Mutates and writes `vanguard.lock` via `lockfile.Save` (the only mutating
path) [observe.go:Observe]; executes allowlisted binding commands with
VANGUARD_TARGET/VANGUARD_DOC/VANGUARD_STORE in the environment
[bindings.go:RunBindings]; prints the provenance line to stderr
[check.go:ApplyBindings].

## Verification

Initless check, observe-then-fresh, code churn, document edit, dirty vs
stale, observe refusals, lock-lost, review_after, strict gaps, path reverse
lookup, lock-free range mode, and the JSON envelope shape [check_test.go].
