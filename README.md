# vanguard-spec

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

A spec-driven verification harness that locks down architectural invariants before code is written — designed around the Elephant-Goldfish Model for agentic workflows.

Vanguard is a standalone command-line tool that keeps specification documents honest. It manages a project's spec documents (markdown, JSON, YAML, and DSL dialects such as C4), verifies that each document follows the structure its owner declared, and answers one question deterministically:

![Which documents have gone stale — and what exactly made them stale.](https://img.shields.io/badge/Which%20documents%20have%20gone%20stale%20%E2%80%94%20and%20what%20exactly%20made%20them%20stale.-e64438?style=for-the-badge)

## Principles

- **Fresh only when attested** — staleness clears by a human `observe`, committed to git.
- **Never silently fresh** — a lost lock, a broken link, a bad file: always red.
- **One deterministic answer** — which docs are stale, and which commit did it.
- **Zero opinions** — the engine owns only identity, watches, and the lock.
- **Machine-native output** — JSON envelope and exit bits for CI and agents.
