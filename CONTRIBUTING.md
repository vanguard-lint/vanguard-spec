# Contributing to vanguard

Thanks for wanting to keep specifications honest. This project is small by
design, so contributions are accepted along a few narrow rails.

## Talk before you code

Open an issue before sending a pull request — for new features, verdict
changes, or schema edits. Large pull requests without a matching issue will
be closed after review, not merged silently.

If your change affects the **lock schema**, the **verdict set**, or the
**exit bitmask**, update the design document
(`docs/designs/vanguard.md`) in the same pull request. The design document
is normative: the engine, the lock, and this website all follow it, never
the other way around.

## Development setup

The tool is a single Go module. You need:

1. Go (see `go.mod` for the minimum version)
2. Git — vanguard reads your repository history directly

```shell
git clone https://github.com/vanguard-lint/vanguard-spec
cd vanguard-spec
go build ./...
go test ./...
```

The website under `website/` is static — open `website/index.html` in a
browser or serve the folder with any file server. No build step.

## Commit messages

Follow Conventional Commits: `type(scope): subject` — for example
`feat(check): report unobserved documents in the json envelope` or
`docs(design): specify the dirty verdict`. Keep the subject imperative and
under 72 characters.

## Pull requests

- One logical change per pull request.
- Update tests next to the behavior you changed; a verdict change without a
  test will be asked back.
- Describe the before/after verdict of `vanguard check` on a real repository
  when the change affects output.
- If the pull request is AI-assisted, say so in the description.

## License

By contributing, you agree that your contributions are licensed under the
Apache License 2.0, the project's license.
