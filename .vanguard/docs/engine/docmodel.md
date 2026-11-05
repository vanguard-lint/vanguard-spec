---
id: docmodel
schema: package-contract
watches:
  paths:
    - "vanguard/internal/docmodel/**/*.go"
  review_after: 30d
---

# docmodel — Discovery and the Engine Document Model

Extracts the engine-owned fields (id, schema, target, watches.paths,
watches.review_after, bindings) from discovered files under the docs root —
markdown frontmatter, top-level JSON/YAML, and DSL leading-comment blocks —
walking the docs root in fixed lexicographic path order. Parsing is
fail-closed; unknown-format files and symlinks are skipped with advisory
warnings.

## Interface

- `Discover(docsRoot, include, formats) ([]Doc, []Warning, error)` — expands
  include globs via `pathmatch.Expand`, walks with `filepath.WalkDir` in
  lexicographic order, parses candidates, dedupes ids, returns docs sorted by
  RelPath; a missing or non-directory docs root is a fatal that names the
  path and the recovery (restore the store documents or `vanguard init`),
  never a raw lstat error [docmodel.go:Discover].
- `Doc{ID, Schema, Target, Watches, Bindings, RelPath, Format, Raw,
  PathGlobs, ReviewAfterSeconds}` — engine fields only; everything else in a
  document is user-defined and opaque [docmodel.go:Doc].
- `Target` — the optional engine-owned `target:` frontmatter key naming one
  configured target; empty means the primary target. The grammar is
  validated at discovery; whether the name is configured is enforced at
  engine load [docmodel.go:parseDocument, check/engine.go:Load].
- `Watches{Paths, ReviewAfter}` — the staleness sources: raw watch globs
  (canonicalized into `PathGlobs`) and the declared `review_after`
  [docmodel.go:Watches].
- `Warning{File, Reason}` — advisory discovery note for skipped files
  [docmodel.go:Warning].
- `FrontmatterCap` = 256 KiB, `MaxIDLen` = 128 [docmodel.go:FrontmatterCap].

## Invariants

- `id` is required and must match `[a-z0-9][a-z0-9._-]*` (max 128, ASCII);
  duplicate ids across files are fatal, recording both paths
  [docmodel.go:parseDocument, docmodel.go:Discover].
- Watch globs canonicalize at discovery into `PathGlobs`; an unexpandable
  watch glob is fatal [docmodel.go:parseDocument].
- `review_after` uses the day grammar (seconds rejected) into
  `ReviewAfterSeconds` [docmodel.go:parseDocument].
- Markdown requires a closed `---` frontmatter block; over the cap is fatal
  with a split-the-document hint [docmodel.go:parseMarkdownFrontmatter].
- Frontmatter YAML is strict: duplicate keys, anchors, aliases are fatal
  [docmodel.go:decodeStrict, docmodel.go:rejectYAMLTricks].
- Whole-document formats (json/yaml) extract the engine keys strictly
  while every other top-level field is user-owned content, never an error;
  duplicate top-level keys and anchors anywhere in the file stay fatal
  [docmodel.go:parseTopLevelFields].
- DSL engine fields come only from the leading comment block; fields later in
  the file are invisible [docmodel.go:parseDSLBlock].
- BOM tolerated and stripped; symlinks never followed; unknown formats
  skipped with warnings [docmodel.go:Discover].

## Side effects

Reads candidate files and walks the docs root (read-only I/O)
[docmodel.go:Discover].

## Verification

Fail-closed missing frontmatter, happy path order, unknown-format skip,
duplicate id fatality, id grammar, brace watch expansion, YAML/DSL formats,
anchor rejection, BOM, the review_after grammar, and opaque user fields with
duplicate-key/anchor fatality for whole-document formats [docmodel_test.go].
