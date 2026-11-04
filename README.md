<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="resources/readme-header-dark.svg?v=9">
    <img src="resources/readme-header-light.svg?v=9" alt="vanguard-spec — lock down architectural invariants before line one" width="480">
  </picture>
</p>

<p align="center">Specs go stale. Vanguard makes the staleness visible.</p>

<p align="center">
  <a href="https://vanguard-lint.github.io/"><strong>WEBSITE</strong></a>
  ·
  <a href="https://github.com/vanguard-lint/vanguard-spec#readme">README</a>
  ·
  <a href="https://github.com/vanguard-lint/vanguard-spec/blob/main/.vanguard/docs/design/vanguard.md">DESIGN</a>
  ·
  <a href="https://vanguard-lint.github.io/blog/"><strong>BLOG</strong></a>
</p>

<br>

Vanguard is a standalone command-line tool that keeps specification documents honest. It manages a project's spec documents (markdown, JSON, and YAML), verifies that each document follows the structure its owner declared, and answers one question deterministically:

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/core-question-dark.svg?v=2">
  <img alt="Which documents have gone stale — and what exactly made them stale." src="docs/assets/core-question-light.svg?v=2">
</picture>

Use it on documents whose drift matters: design docs, runbooks, agent-facing rules, glossaries. For scratch notes, use your normal workflow.

## Principles

- **Fresh only when attested** — staleness clears by a human `observe`, committed to git.
- **Never silently fresh** — a lost lock, a broken link, a bad file: always red.
- **One deterministic answer** — which docs are stale, and which commit did it.
- **Zero opinions** — the engine owns only identity, watches, and the lock.
- **Machine-native output** — JSON envelope and exit bits for CI and agents.
- **EGM drives the workflow**

## Install

Requires Go 1.26+ (from source) or a prebuilt binary from [releases](https://github.com/vanguard-lint/vanguard-spec/releases).

### Prebuilt binary

```sh
curl -fsSL -o vanguard \
  https://github.com/vanguard-lint/vanguard-spec/releases/latest/download/vanguard-0.2.0-linux-amd64
chmod +x vanguard
```

Windows: download `vanguard-0.2.0-windows-amd64.exe` from the same release page. Every asset ships with a `.sha256` sidecar — verify before running.

### From source

```sh
go install github.com/vanguard-lint/vanguard/cmd/vanguard@latest
```

## Quickstart

Two commands carry the whole contract: `check` decides, `observe` attests.

**1. Scaffold the store**

```sh
vanguard init
```

This creates `.vanguard/` with the store (`vanguard.yaml`), the attestation lock (`vanguard.lock`), `docs/`, and `schemas/`.

**2. Declare a binding card**

Each watched document gets a card under `.vanguard/docs/` that says what it asserts and which paths it watches:

```markdown
---
id: readme
schema: document
watches:
  paths:
    - "README.md"
  review_after: 180d
---

Binding card: README.md is managed outside the store; this card watches it
and goes stale when it changes, forcing a re-review.
```

**3. Check — decide, never guess**

```sh
$ vanguard check --format json
{
  "schema": 1,
  "documents": [{
    "id": "architecture-overview",
    "status": "stale",
    "reasons": ["scope_changed"],
    "introducing_commits": ["81243c6"]
  }]
}
```

Every watched document gets exactly one verdict — `fresh`, `stale`, `dirty`, `unobserved`, or `lock lost` — composed into the exit bitmask. CI and agents act on the exit code, not on prose.

**4. Observe — attest, don't assume**

```sh
$ vanguard observe readme
stale → fresh
anchor moved to 81243c6

$ vanguard check
all documents fresh (exit 0)
```

`observe` is the only command that mutates the lock: it moves the anchor to the current commit and records the attestation. Commit the lock together with the change it attests.

## Command reference

| Command | Use it to… |
|---|---|
| `vanguard init` | Scaffold the `.vanguard/` store. |
| `vanguard check` | Classify every watched document: `fresh`, `stale`, `dirty`, `unobserved`, or `lock lost`. |
| `vanguard check <doc-id>` | Check a single document. |
| `vanguard check --path <path>` | Check the documents whose watch scope covers a path. |
| `vanguard check --from <sha> --to <sha>` | Attribute staleness to a commit range. |
| `vanguard check --strict` | Elevate unwatched/unobserved gaps to a fatal exit bit. |
| `vanguard check --format json` | Emit the JSON envelope for CI and agents. |
| `vanguard observe <doc-id>` | Attest one document — the only lockfile-mutating command. |
| `vanguard observe --all` | Bootstrap hatch: attest documents currently in the unobserved set. |
| `vanguard observe --at <RFC3339>` | Record an explicit attestation timestamp (for CI runs). |
| `vanguard observe --prune` | Drop lock entries whose id no longer exists. |

Structure errors are always fatal; per-document staleness composes into the exit bitmask, so one number carries the whole verdict.

## Learn more

- [Design document](https://github.com/vanguard-lint/vanguard-spec/blob/main/.vanguard/docs/design/vanguard.md) — the full contract: CLI surface, lock schema, exit bits.
- [Blog](https://vanguard-lint.github.io/blog/) — why spec staleness is a build problem, and how the harness is built.
- [Contributing](CONTRIBUTING.md) and [security policy](SECURITY.md).

## License

[Apache 2.0](LICENSE)
