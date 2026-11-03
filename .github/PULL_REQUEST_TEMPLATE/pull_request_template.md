---
name: Pull request
about: Propose a change to vanguard
title: ''
labels: ''
assignees: ''
---

Fixes # (issue)

## Overview

What changes, and why. One logical change per pull request.

**Schema, verdict, or exit-bitmask change?**

If yes, link the updated section of `docs/designs/vanguard.md` included in
this pull request. Pull requests that change normative behavior without the
design document will be sent back.

## Verdict impact

For engine changes: run `vanguard check` before and after on a real
repository and paste both envelopes (trim if long).

## Testing

- [ ] `go test ./...` passes
- [ ] New behavior is covered by a test
- [ ] `website/` untouched, or screenshot attached

This contribution is AI-assisted: (yes / no)
