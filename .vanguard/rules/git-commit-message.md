# Git Commit Message Rule

Applies to **every commit** in this repository. vanguard-spec is a
multi-harness agent plugin: skills, agents, commands, runtime code (JS), and
behavioral eval scenarios, distributed to several harnesses (Claude Code,
Codex, OpenCode, DeepSeek harness, ...). Several of its files are behavioral
contracts, not just documentation — this rule makes that visible in history.

Commit messages are written in **English** only.

## Structure

```
<type>(<scope>)?: <subject>

break:      # only when a contract changes (see Conflicts below)
- <incompatibility; who must update what>
change:
- <behavioral, wording, or runtime change to existing content>
refs:      # optional
- <issue / PR / ADR reference>
```

- **subject** — imperative, lowercase start, no trailing period, ≤ 72 chars.
  The body carries *what* and *why*; never *how* implementation details.
- **scope** (optional) — use exactly one of:
  - **Harness adapters only**: the harness id — `claude`, `codex`,
    `opencode`, `deepseek`, or the adapter's folder name.
  - **Component-local**: `skill`, `agent`, `command`, `rule`, `spec`,
    `resource`, `test`, `infra`, `site`.
  - A change spanning 2+ harnesses takes **no scope** and lists affected
    harnesses under `change:` in the body.

## type

| type | meaning |
|---|---|
| `spec` | change to a behavioral contract (see Contract files) — **body required** |
| `feat` | new skill, agent, command, adapter, capability, or runtime module |
| `fix` | correction of a defect: wrong instruction, broken link, contradicton, runtime bug, harness incompatibility |
| `test` | add or change eval scenarios (`test_scenarios.yaml`), TEST.md, unit/integration tests |
| `refactor` | restructuring without changing behavior or prescriptions |
| `perf` | runtime performance change (JS code paths, eval runner) |
| `build` | plugin manifests and packaging: `plugin.json`, `marketplace.json`, harness adapter configs, bundling |
| `ci` | workflows, validation scripts, lint/format automation |
| `docs` | prose that is not a contract: README, guides, comments, examples |
| `style` | formatting/whitespace with zero content or behavior change |
| `chore` | housekeeping: renames, moving files, dependency bumps |
| `revert` | reverts a previous commit (`revert: type subject`) |

Type precedence when a commit matches several: `spec` → `fix` → `feat` →
`test` → the rest.

## Contract files

These files are **contracts** — harnesses and agents execute or enforce them.
A change is `spec` (or `fix` when correcting a defect) and requires a body:

- `resources/document_standards/*.md` — authorative rubrics
- `resources/execution_instructions.md` — runtime guardrails
- `skills/<name>/SKILL.md` — prompt/protocol text and frontmatter
- `agents/<name>.md` — persona prompts and frontmatter
- `commands/*.md` — command definitions
- plugin manifests: `plugin.json`, `.claude-plugin/`, `.codex-plugin/`,
  `.agents/`, and per-harness adapter configs

Prompt edits that change behavior (instructions, gates, output format) are
`spec` or `fix` depending on intent. Pure typo/formatting in those files is
`fix` or `style` with no body required. Adding a whole new contract file is
`feat` with a body.

## Behavioral evidence rule

A commit that changes skill/agent/command behavior (**must** ship the
updated `test_scenarios.yaml` / `TEST.md` in the same commit. If scenarios
cannot cover the change yet, state the gap explicitly:

```
spec(skill): require artifact-first output for resolve findings
change:
- findings above 50 lines must be written to a file, not chat
test gap: eval scenarios extend in follow-up commit
```

## break / change

- **break** — anything previously produced or installed that stops working:
  a standard changed thresholds or removed mandatory sections; a skill
  renamed an output path, command, or artifact; a manifest change breaks
  install or marketplace resolution on an existing harness. Every `break`
  references the ADR that authorizes it, or states
  `refs: <issue/PR>` if no ADR applies.

- **change** — edits that do not invalidate anything already produced, plus
  per-harness notes for cross-harness commits (`affects: claude, opencode`).

The body is required for: any `spec`, any `break`, any commit touching **3+
files**, any `feat`, and any cross-harness commit.

## Why this shape

1. **Contracts visible at a glance** — `git log --grep '^spec'` lists every
   behavioral contract change, mirroring the `FROZEN` node status in
   spec-driven quality control.
2. **Harness dimension explicit** — harness-scoped commits match upstream
   EGM practice (`feat(claude): ...`) and make adapter drift auditable.
3. **Evidence rides with the change** — behavioral edits without scenario
   updates are called out instead of silently skipped; ties the repo to its
   "lock through spec" philosophy.

## Examples

Contract change (body required):

```
spec(zoom-out-standard): require per-question evidence citations

break:
- Reviews written against the old rubric lack citation lists; they must be
  re-reviewed or grandfathered as pre-citation format.

refs: ADR-0003
```

Harness-scoped adapter change:

```
feat(claude): register skills and agents via plugin manifest

new:
- .claude-plugin/plugin.json lists 7 skills for /skill: invocation

change:
- affects: claude only; codex adapter follows in next commit
```

Skill behavioral change with evidence:

```
spec(skill): gate finalization on directory probe step

change:
- draft skill must detect repo layout before writing docs/designs
test:
- scenario directory_finalize_modal_selection updated
```

Fix:

```
fix(claude): correct hook path resolution on Windows hosts
```

Housekeeping:

```
chore: move shared resources under platform-neutral root
```
