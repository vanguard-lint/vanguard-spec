# vanguard-spec

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

A spec-driven verification harness that locks down architectural invariants before code is written.

## Principles

- **Spec first, code later**: the specification is the contract; implementation only follows a frozen spec.
- **Authoritative rules live here**: standards in `resources/` are the single source of truth; other docs link to them instead of copying.
- **Upstream quality engine**: EGM provides multi-stage reviewer swarms (`egm-review`, `egm-drift-report`); this repo defines what quality means for our specs.
