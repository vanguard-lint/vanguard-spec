# Docs Organization

Every document that describes the code of this project lives inside the
vanguard store, under `.vanguard/docs/`. That directory is the single
document root: discovery reads there and nowhere else.

## Where documentation goes

- Documentation for the Go module under `vanguard/` belongs in
  `.vanguard/docs/`. One file per subsystem, kebab-case file names, and the
  file name matches the engine field `id` in the frontmatter.
- Documents produced by the EGM bootstrap pass also land in
  `.vanguard/docs/`, following the same frontmatter contract.
- Each document declares its staleness sources in frontmatter: `watches.paths`
  points at the source it describes (for example `vanguard/**/*.go`), and
  `watches.review_after` when the text needs a scheduled review.

## What stays outside

Documents that predate this rule keep their current location and are not
migrated:

- `docs/designs/` — the design canon, including `vanguard.md`.
- `website/` — the site source, published by its own workflow.
- `README.md` — the project front page.

Treat those as read-only canon. When a statement in `.vanguard/docs/`
contradicts the design canon, the design canon wins; fix the store document
and record the review with `vanguard observe`.

## Dogfooding

The engine documented by these documents is the same engine that checks them:

- `vanguard check` runs in CI context against the store, so every spec
  document in `.vanguard/docs/` is guarded by its own watch globs.
- After changing code a document watches, update the document, commit, then
  run `vanguard observe <doc-id>` to record the new attestation.
- The `vanguard.lock` file is committed with the documents; a fresh clone
  without it reports lock-lost and degrades loud, not green.
