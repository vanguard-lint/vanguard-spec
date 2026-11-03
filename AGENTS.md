# AGENTS.md

Go CLI that reports which spec documents are stale (`check`) and records human
attestation (`observe`). This file is for contributors of this repo (humans and
agents), not for users of the tool.

Normative design: `docs/designs/vanguard.md`. It wins over
`docs/designs/design_draft.md`, which is drafting history only.

## Commands

- Build / test: from `vanguard/` run `go build ./...` and `go test ./...`
  (CI adds `-timeout 300s`; config: `vanguard/go.mod`)
- Prose lint: `vale docs/` (config: `.vale.ini`)
- Freshness of all docs: `vanguard check --format json`
- Freshness for a path: `vanguard check --path <path> --format json`
- Git hooks: `git config core.hooksPath .githooks` (commit-msg + pre-commit)

## Invariants (a violation is a bug, not a style issue)

- `check` never writes. `observe` is the only writer of `vanguard.lock`.
- Never hand-edit `vanguard.lock`.
- No network and no LLM in `check` / `observe`.
- Binding outcomes never map to `stale` (own exit bit). Bindings run via argv, never through a shell.
- Structure errors fail closed. Never downgrade a fatal to a warning.

## Working rules

- Spec first: change `docs/designs/vanguard.md`, then the code.
- Do NOT run `observe` just to turn `check` green. Run it only after you have
  re-read the document and committed the change. Never use `observe --all`.
- Editing a watched doc costs: edit, commit, `observe`, commit the lock.
- Reviewer agents (fresh context, no conversation history): judge the document
  against the files it references only. Report findings; do not edit the document.

## Boundaries

- Always: run tests and `vanguard check` before declaring a task done.
- Ask first: adding a dependency; changing exit-bit semantics, the lock schema, or glob syntax.
- Never: commit secrets; edit `vanguard.lock` by hand; edit build output or
  anything the release workflow produces (releases are cut by
  `.github/workflows/release.yml`, not by hand).

## Read on demand (do not load all of the design doc)

- Lock format and anchoring: design doc, "Anchoring"
- Exit bits and JSON envelope: "Output contract"
- Corner cases (symlinks, submodules, blobless clones): "Edge-case resolutions"
