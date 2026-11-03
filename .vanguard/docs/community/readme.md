---
id: readme
schema: document
watches:
  paths:
    - "README.md"
  review_after: 180d
---

Binding card: README.md is managed outside the store; this card watches it and goes stale when it changes, forcing a re-review. The watched file is the single source of truth.
