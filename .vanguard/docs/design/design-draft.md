---
id: design-draft
schema: document
watches:
  paths:
    - "docs/designs/design_draft.md"
  review_after: 180d
---

Binding card: docs/designs/design_draft.md is managed outside the store; this card watches it and goes stale when it changes, forcing a re-review. The watched file is the single source of truth.
