# vanguard-spec

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

Spec-driven quality control layer. Locks systems through falsifiable specifications before implementation, using EGM (Elephant-Goldfish Model) as the upstream review/verification engine.

## Structure

```
resources/
├── execution_instructions.md      # Runtime guardrails (no-code guard, execution discipline)
└── document_standards/            # Authoritative rubrics for spec documents
    ├── feature-spec-standard.md
    ├── heilmeier-standard.md
    ├── implementation-readiness-standard.md
    ├── re-implementation-standard.md
    └── zoom-out-standard.md
```

## Principles

- **Spec first, code later**: the specification is the contract; implementation only follows a frozen spec.
- **Authoritative rules live here**: standards in `resources/` are the single source of truth; other docs link to them instead of copying.
- **Upstream quality engine**: EGM provides multi-stage reviewer swarms (`egm-review`, `egm-drift-report`); this repo defines what quality means for our specs.

## Related

- [Elephant-Goldfish Model](https://github.com/google/elephant-goldfish-model) — upstream method and skill set.
