# vanguard-spec

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

A spec-driven verification harness that locks down architectural invariants before code is written — designed around the Elephant-Goldfish Model for agentic workflows.

## Principles

- **Spec first, code later**: the specification is the contract; implementation only follows a frozen spec.
- **Deterministic over interpretive**: staleness, structure, and freshness are computed and reproducible — never LLM-judged inside the engine.
- **Built for agents first**: machine-readable output (a versioned envelope, a composable exit bitmask) and a self-describing CLI; human maintainers and AI agents are both first-class consumers.
- **EGM as the workflow frame**: designs are drafted and reviewed through the Elephant-Goldfish Model skills (`egm-draft`, `egm-review`) — a fixed drafting/gate/review loop instead of ad-hoc processes.
