<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="resources/readme-header-dark.svg?v=9">
    <img src="resources/readme-header-light.svg?v=9" alt="vanguard-spec — lock down architectural invariants before line one" width="480">
  </picture>
</p>

<p align="center">
  <a href="https://vanguard-lint.github.io/"><strong>WEBSITE</strong></a>
  ·
  <a href="https://github.com/vanguard-lint/vanguard-spec#readme">README</a>
  ·
  <a href="https://github.com/vanguard-lint/vanguard-spec/blob/main/docs/designs/vanguard.md">DESIGN</a>
</p>

<br>

Vanguard is a standalone command-line tool that keeps specification documents honest. It manages a project's spec documents (markdown, JSON, YAML, and DSL dialects such as C4), verifies that each document follows the structure its owner declared, and answers one question deterministically:

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/core-question-dark.svg?v=2">
  <img alt="Which documents have gone stale — and what exactly made them stale." src="docs/assets/core-question-light.svg?v=2">
</picture>

## Principles

- **Fresh only when attested** — staleness clears by a human `observe`, committed to git.
- **Never silently fresh** — a lost lock, a broken link, a bad file: always red.
- **One deterministic answer** — which docs are stale, and which commit did it.
- **Zero opinions** — the engine owns only identity, watches, and the lock.
- **Machine-native output** — JSON envelope and exit bits for CI and agents.
- **EGM drives the workflow**

## License

[Apache 2.0](LICENSE)
