---
id: command-surface
schema: package-contract
watches:
  paths:
    - "vanguard/cmd/**/*.go"
  review_after: 30d
---

# command-surface — Command Surface

Cobra wiring for init, check, and observe. The CLI locates the store, maps
flags onto the engine's Selection and ObserveRequest, renders the envelope,
and exits with the composed exit bits. All logic lives in internal/check;
this package is wiring only.

## Interface

- `vanguard init` — scaffolds `.vanguard/` (store, docs/, schemas/, default
  vanguard.yaml, empty vanguard.lock); refuses to overwrite an existing store
  [main.go:initCmd].
- `vanguard check [doc-id]` — mode precedence: `--from` → range, `--path` →
  path, positional id → doc, else all [main.go:checkCmd].
- `vanguard observe <doc-id>` — the only lockfile-mutating command; flags
  `--all` (guarded bootstrap hatch), `--prune` (standalone), `--at`
  (RFC3339), `--format` [main.go:observeCmd].
- `emit(env, format, output)` — renders to `--output` or stdout, then exits
  with `env.ExitBits` when non-zero [main.go:emit].
- `resolvePathArg(targetDir, arg)` — resolves the `--path` argument against
  the target root: absolute paths must land inside it, `..` segments
  rejected; reports whether the path is a directory [main.go:resolvePathArg].

## Invariants

- `--now` and `--at` must parse as RFC3339 or the command errors
  [main.go:checkCmd, main.go:observeCmd].
- `--strict` combined with `--from`/`--to` is a usage error
  [main.go:checkCmd].
- `--max-recovery-walk` (default 1000) is parsed into the engine's
  `Options.MaxRecoveryWalk` [main.go:checkCmd] but is currently inert: the
  effective bound comes from `Selection.MaxRecoveryWalk`, which the CLI
  never sets, so the engine fallback of 1000 always applies.
- `--fail-on-affected` sets exit bit 32 when affected_in_range is non-empty
  (range mode only) [main.go:checkCmd].
- `--run-bindings` executes the allowlisted bindings for documents in scope;
  in range mode the flag is silently ignored — no error, no exit bit, no
  hint [main.go:checkCmd].
- Bare `observe` (no id, no `--all`, no `--prune`) is refused by the engine
  as a usage error [main.go:observeCmd].
- `findStore` walks upward to the filesystem root and errors with "run
  vanguard init first" [main.go:findStore].
- The process exit code is the envelope's exit bits [main.go:emit].

## Side effects

init creates the store directories and writes vanguard.yaml + vanguard.lock
[main.go:initCmd]; emit writes the output file or stdout and may exit the
process [main.go:emit].

## Verification

Covered indirectly by the engine-level end-to-end tests in
internal/check (check_test.go) driving the same entry points; the CLI adds
flag parsing and store discovery on top.
