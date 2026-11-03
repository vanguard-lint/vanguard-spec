---
id: structure
schema: package-contract
watches:
  paths:
    - "vanguard/internal/structure/**/*.go"
  review_after: 30d
---

# structure — Declared Schema Enforcement

Enforces the schema a document declares: `schema: <name>` binds the document
to `.vanguard/schemas/<name>.schema.json`, whose x-vanguard block carries the
markdown structure contract. Loading fails closed; validation is line-based,
deterministic, and located.

## Interface

- `Load(schemasDir, name) (*Profile, error)` — reads
  `<schemasDir>/<name>.schema.json`, rejects invalid id grammar, fails on
  missing files, non-JSON bodies, and an uncompilable citation_pattern; a
  schema without an x-vanguard block binds existence only
  [structure.go:Load].
- `Profile{Format, RequiredSections, CitationSections, CitationPattern}` —
  the machine-enforceable projection of the schema's x-vanguard block
  [structure.go:Profile].
- `Validate(file, body, prof, format) []Violation` — returns every finding
  for one document body; a non-markdown body under a markdown contract is a
  format-mismatch finding; a nil profile validates nothing
  [structure.go:Validate].
- `Violation{Rule, File, Line, Message}` — one located finding; line numbers
  are 1-based and point at the offending heading or bullet
  [structure.go:Violation].

## Invariants

- Fail closed: a declared but missing or corrupt schema is an error at load,
  never a silently skipped check [structure.go:Load].
- Section names match exactly and case-sensitively (`interface` is not
  `Interface`) [structure.go:Validate].
- Required sections must be present, in contract order, and unique; order
  violations point at the misplaced heading [structure.go:Validate].
- In a citation section, every `- ` bullet must match the citation pattern;
  violations point at the uncited line [structure.go:Validate].
- Findings are deterministic for a given input — no map iteration order
  leaks into results [structure.go:Validate].

## Side effects

Reads schema files under the schemas directory (read-only I/O)
[structure.go:Load].

## Verification

Missing/corrupt schema fatality, invalid citation pattern, existence-only
schemas, id grammar, missing/order/duplicate sections, uncited claims with
exact lines, valid-document passes, case-sensitive titles, format mismatch,
and the nil profile [structure_test.go].
