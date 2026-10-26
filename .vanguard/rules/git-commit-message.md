# Git Commit Message Rule

Applies to **every commit** in this repository. This rule is tailored to what
vanguard-spec actually is: a small, standards-and-methodology repo (markdown
rubrics, rules, no application source code), whose files act as behavioral
contracts. It deliberately does **not** copy enterprise rules (no ticket codes,
no mandatory scopes) and follows the style of our upstream reference,
[Elephant-Goldfish Model](https://github.com/google/elephant-goldfish-model).

## Structure

```
<type>(<scope>)?: <subject>

break:      # only when a contract changes (see below)
- <incompatibility and what callers/readers must update>
change:
- <behavioral or wording change to existing content>
```

- **subject** — imperative, lowercase start, no trailing period, ≤ 72 chars.
  Describe *what* and *why* in the body; the subject stays short.
- **scope** (optional) — directory scope when the change is localized:
  `spec` (document_standards), `rule`, `resource`, `infra`, `site`.
- Simple commits may commit with subject only; the body is required for any
  `spec` change, any `break`, and any change touching 3+ files.

## type

| type | meaning |
|---|---|
| `spec` | change to an authoritative standard/rubric in `resources/document_standards/` or `resources/execution_instructions.md` — a contract change; **body required** |
| `feat` | new rule file, new resource, new capability of the repo |
| `fix` | correction of an error in an existing rule/standard (wrong instruction, broken link, contradiction) |
| `docs` | prose that is not a contract: README, comments, examples |
| `chore` | housekeeping with no content change: renames, moving files |
| `ci` | workflows, validation scripts, automation |
| `revert` | reverts a previous commit (`revert: type subject`) |
| `refactor` | restructuring content without changing what it prescribes |

Type precedence when a commit could match several: `spec` → `fix` → `feat` →
`docs` → the rest. A commit that both fixes and extends a standard is `spec`,
with the fix recorded under `change:` in the body.

## break / change

- **break** — the previous text prescribed behavior that no longer works:
  a standard changed thresholds, removed mandatory sections, renamed a
  document type, or contradicts what existing specs were written against.
  Every `break` must reference the ADR that authorizes it (or state
  `"no ADR — agreed in <issue/PR link>"`).
- **change** — edits that do not invalidate anything already produced.
- **new** is folded into the type (`feat`, `spec`) since this repo has no
  runtime; do not use a `new:` block.

## Why this shape

1. **Standards are contracts.** The `spec` type plus a mandatory body makes
   contract changes visible at a glance in `git log` — the same spirit as the
   `FROZEN` node status in spec-driven quality control.
2. **No ticket codes.** Nothing here is traced to a Redmine; noise codes would
   add overhead without traceability. Tickets, if introduced later, go in the
   body as `refs: #123`.
3. **Conventional Commits base.** Tooling that parses `type(scope): subject`
   keeps working, and history stays readable next to EGM upstream history.

## Examples

Contract change (body required):

```
spec(zoom-out-standard): require per-question evidence citations

break:
- Reviews written against the old rubric lack citation lists; they must be
  re-reviewed or grandfathered and marked as pre-citation format.

change:
- The evidence table replaces the free-form "observations" section.
- refs: ADR-0003
```

Fix:

```
fix(rule): correct relative links after moving rules to .vanguard/rules
```

Housekeeping:

```
chore: rename README intro to match vanguard terminology
```
