#!/usr/bin/env python3
"""Validate commit messages against .vanguard/rules/git-commit-message.md.

Enforced checks (all deterministic):
- subject: <type>(<scope>)?: <subject> with allowlisted type and scope
- subject: starts lowercase/digit, no trailing period, <= 72 chars
- body required for: spec, feat, any `break:` block, commits touching 3+ files
- `break:` requires a `refs:` line

Merge commits are skipped. Exits non-zero and prints every violation.
"""

from __future__ import annotations

import re
import subprocess
import sys

TYPES = frozenset(
    "spec feat fix test refactor perf build ci docs style chore revert".split()
)
SCOPES = frozenset(
    "claude codex opencode deepseek "
    "skill agent command rule spec resource test infra site website readme".split()
)
SUBJECT_RE = re.compile(r"^([a-z]+)(?:\(([a-z0-9-]+)\))?: (\S.*)$")
MAX_SUBJECT = 72


def git(*args: str) -> str:
    return subprocess.run(
        ["git", *args], capture_output=True, text=True, check=True
    ).stdout


def changed_file_count(sha: str) -> int:
    out = git("show", "--name-only", "--format=", sha)
    return len([line for line in out.splitlines() if line.strip()])


def violations(sha: str, files_hint: int | None = None) -> list[str]:
    raw = git("show", "--format=%B", "--no-patch", sha)
    lines = raw.splitlines()
    subject = lines[0] if lines else ""
    errors: list[str] = []

    if subject.startswith("Merge "):
        return errors

    m = SUBJECT_RE.match(subject)
    if not m:
        return [
            f"{sha[:9]}: subject {subject!r} does not match "
            "<type>(<scope>)?: <subject>"
        ]
    c_type, scope, rest = m.groups()
    if c_type not in TYPES:
        errors.append(f"{sha[:9]}: unknown type {c_type!r}")
    if scope is not None and scope not in SCOPES:
        errors.append(f"{sha[:9]}: unknown scope {scope!r}")
    if len(subject) > MAX_SUBJECT:
        errors.append(f"{sha[:9]}: subject is {len(subject)} chars (limit {MAX_SUBJECT})")
    if rest.endswith("."):
        errors.append(f"{sha[:9]}: subject must not end with a period")

    body = lines[1:]
    has_break = any(line.strip().startswith("break:") for line in body)
    needs_body = (
        c_type in ("spec", "feat") or has_break
    )
    n_files = files_hint if files_hint is not None else changed_file_count(sha)
    if n_files >= 3:
        needs_body = True

    content = [
        line
        for line in body
        if line.strip() and not re.match(r"^\s*(break:|change:|refs:|new:|test:)", line)
    ]
    if needs_body and not content:
        errors.append(
            f"{sha[:9]}: body required ({c_type or 'commit'}"
            + (", break block" if has_break else "")
            + (f", {n_files} files" if n_files >= 3 else "")
            + ")"
        )
    if has_break and not any(line.strip().startswith("refs:") for line in body):
        errors.append(f"{sha[:9]}: break: block requires a refs: line")

    return errors


def commit_range(base: str, head: str) -> list[str]:
    log = git("log", "--format=%H", f"{base}..{head}")
    return [sha for sha in log.splitlines() if sha.strip()]


def main() -> int:
    if len(sys.argv) == 3:
        shas = commit_range(sys.argv[1], sys.argv[2])
    else:
        shas = [line.strip() for line in sys.stdin if line.strip()]

    if not shas:
        print("commit-lint: no commits to check")
        return 0

    all_errors: list[str] = []
    for sha in shas:
        all_errors.extend(violations(sha))

    for err in all_errors:
        print(err)
    if all_errors:
        print(
            f"\ncommit-lint: {len(all_errors)} violation(s) — see "
            ".vanguard/rules/git-commit-message.md"
        )
        return 1
    print(f"commit-lint: {len(shas)} commit(s) OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
