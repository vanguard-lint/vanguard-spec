# Security Policy

vanguard is an offline, deterministic CLI: it reads a repository, hashes
declared scopes, and prints a verdict. That shape defines what counts as a
security problem here.

## Supported versions

Only the latest commit on `main` is supported. The project does not maintain
release branches; the supported way to run it is from source:

```shell
git clone https://github.com/vanguard-lint/vanguard-spec
cd vanguard-spec/vanguard
go install ./cmd/vanguard
```

or check out the latest tag (for example `v0.1.0`) before building.

## Reporting

Report vulnerabilities privately through
[GitHub Security Advisories](https://github.com/vanguard-lint/vanguard-spec/security/advisories/new) —
**do not open a public issue** for anything in scope below.

Please include: the exact command, the repository state (commit), the
`vanguard.lock` content, and the observed versus expected verdict. Reports
receive a response within 14 days.

## In scope

- A way to make `vanguard check` report **fresh** for a document whose watched
  scope changed (verdict bypass, lock forgery, hash collision avenue).
- Path traversal or injection through watch patterns, document ids, or
  repository content (for example, crafted file names reaching the JSON
  envelope or the shell of a CI consumer).
- The exit bitmask disagreeing with the JSON envelope.

## Out of scope

- The project website (`vanguard-lint.github.io`): static hosting of static
  content; report content errors as normal issues.
- False-positive staleness verdicts without a bypass — those are bugs, use a
  normal issue.
- Social engineering of repository collaborators.
