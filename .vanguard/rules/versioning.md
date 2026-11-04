# Versioning and Release

The vanguard CLI is a long-lived tool: teams pin it in CI, so version numbers
are a contract. This rule exists so versions move only when meaning moves —
never as decoration.

## Scheme

- Releases follow semantic versioning: `MAJOR.MINOR.PATCH`.
- Before 1.0 (the `0.y.z` era):
  - a breaking change — any change an existing invocation can observe as a
    different result — bumps **MINOR** (`0.3.0` → `0.4.0`);
  - new features and notable fixes bump **PATCH** only when they break
    nothing (`0.3.0` → `0.3.1`);
  - `0.0.x` is reserved for the bootstrap phase and must never appear after
    the first public release.
- From 1.0 onward: strict semver. Breaking changes bump MAJOR and require a
  migration note in the release notes.

## Versioned contracts inside the tool

Three embedded contract versions move with the CLI version, each with its own
rule:

| Contract | Where | Rule |
|---|---|---|
| Output envelope | `schema` field in every envelope | Adding an optional field = same version; removing, renaming, or changing the type of a field = next MAJOR (MINOR before 1.0), and `schema` increments with it |
| Lockfile shape | `version` field in `vanguard.lock` | Only observe writes it; a new field the old CLI cannot parse = MAJOR for the CLI, and the lock reader fails closed on an unknown MAJOR |
| Config grammar | `version` field in `vanguard.yaml` | Unknown MAJOR is fatal by design; new optional keys never bump it |

## Source of truth

- The version of a build comes from exactly one place: the git tag
  `vX.Y.Z` on the main branch.
- Release binaries are built by the release workflow, which injects the tag
  through `-ldflags "-X main.version=X.Y.Z"`.
- A build made from a working tree with no tag reports `dev` — never a
  number that looks released. Requiring the number to come from the tag
  makes "which build am I debugging" answerable from the binary alone.

## Process — how a version may move

1. Work lands on main as ordinary commits. No commit ever bumps a version
   string; version constants in source stay at the `dev` placeholder.
2. When the accumulated changes justify a release, the maintainer:
   - writes release notes grouped as Added / Changed / Fixed / Breaking,
     generated from the commit history — every user-visible change is listed;
   - tags `vX.Y.Z` with the bump level chosen strictly by the tables above;
   - pushes the tag; the release workflow builds and attaches binaries and
     publishes the notes.
3. Re-tagging a pushed version is forbidden. A broken release is fixed by a
   new PATCH version; if a release is unusable before anyone consumes it,
   delete the GitHub release and tag together, then re-tag once.
4. Hotfixes branch from the released tag, fix, and land as a PATCH bump —
   never by re-releasing the same number with different bytes.

## Release notes

The release notes are the tag annotation: the release workflow extracts the
annotation and publishes it as the release body. A published body that reads
as a commit message means the extraction silently fell back — that is a
bug, and the run must fail, never publish.

- Notes speak to the user of the binary, grouped as Added / Changed /
  Fixed / Breaking; every user-visible change is listed.
- Each Breaking entry carries its migration step — an incompatibility
  without a migration is an unfinished entry.
- Plain text only: no emoji, no icons, no decorative banners.
- An Install section lists the per-platform assets and checksum
  verification; a compare link closes the notes.
- The extraction step fails loud when the annotation is empty — an empty
  or fallback body is a failed release, never a published one.

## What is forbidden

- Bumping a version because a date passed, a milestone closed, or "it felt
  ready" — the bump level is a function of the diff, not the calendar.
- Shipping a version whose release notes are empty or copied from the last
  release.
- Hand-editing a version string into a built binary or a release artifact.
- Two artifacts with the same version but different content.
