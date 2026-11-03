---
name: Feature request
about: Propose a new verdict, watch rule, or envelope field
title: ''
labels: enhancement
assignees: ''
---

**The decision this unlocks**

What question about a repository can you not answer deterministically today?
Describe it as a decision, not a mechanism.

**Proposed behavior**

Command syntax, envelope field, or verdict change — concretely. If it touches
the lock schema, the verdict set, or the exit bitmask, say so: those changes
require a design document update (`docs/designs/vanguard.md`) in the same
pull request.

**Determinism check**

Does the proposal depend only on repository content and git history? Features
that require network access, model judgment, or human scoring are out of
scope for the engine.

**Alternatives considered**

What existing command or flag gets you closest today, and what is missing.
