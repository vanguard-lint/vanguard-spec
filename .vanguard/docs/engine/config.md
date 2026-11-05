---
id: config
schema: package-contract
watches:
  paths:
    - "vanguard/internal/config/**/*.go"
  review_after: 30d
---

# config — Strict Store Configuration

Loads and strictly validates `vanguard.yaml` — the user-owned configuration
defining discovery globs, the target path, named targets, tunables,
allowlisted bindings, and the extension-to-format map. Fail-closed entry
point: anchors, aliases, merge keys, unknown keys, bad discovery globs, and
bad binding shapes are all fatal; advisory warnings (missing version) ride
along on the returned Config.

## Interface

- `Load(path string) (*Config, error)` — reads the file, strips a UTF-8 BOM,
  delegates to `Decode`; an error is fatal (exit bit 1), warnings advisory
  [config.go:Load].
- `Decode(data []byte) (*Config, error)` — two passes: a yaml.Node pass that
  rejects anchors/aliases/merge keys, then a `KnownFields(true)` strict pass,
  then defaults and validation [config.go:Decode].
- `Config{Version, Discovery, TargetPath, Targets, ClockSkewTolerance,
  Bindings, Formats, Warnings}` — fully defaulted [config.go:Config].
- `Targets` — the optional `targets:` map (name → local path) declaring
  named targets beside the legacy singular `target:`; 0.4.0 decoupled stores
  [config.go:Targets].
- `Binding{Cmd, CWD, Timeout}` — one allowlisted command; documents reference
  bindings by name, never free text [config.go:Binding].
- `DefaultClockSkewTolerance` = 5m, `DefaultBindingTimeout` = 600s
  [config.go:DefaultClockSkewTolerance].

## Invariants

- Unknown keys are fatal [config.go:Decode]; anchors, aliases, and merge keys
  are fatal before any decode [config.go:rejectAnchors].
- Unknown config major fails closed; missing version is advisory (assumes 1,
  appends a warning) [config.go:Decode].
- Discovery globs cannot escape docs: empty globs, `..`, `../` prefixes, and
  `/../` segments are rejected; the glob must compile under the closed
  pathmatch syntax [config.go:validateDiscoveryGlob].
- Default discovery is `["**/*.md"]`; the default formats map
  `{md: markdown, json: json, yaml: yaml}` merges with overrides, which must
  name one of markdown/json/yaml/dsl [config.go:Decode].
- Binding names follow the shared id grammar; `cmd` must be a non-empty
  array; `cwd` is `""`, `"target"`, or `"store"`; timeout and
  `clock_skew_tolerance` go through `duration.ParseTunable` [config.go:Decode].
- Target names follow the same shared id grammar and paths must be non-empty
  [config.go:Decode]; whether a declared target path exists is enforced at
  engine load (a declared target pointing at nothing is a config error, never
  a silent fall into hash mode) [check/engine.go:Load].

## Side effects

Reads the config file from disk (read-only I/O) [config.go:Load].

## Verification

Defaults, advisory missing version, unknown major failure, binding
parse/rejections, anchor rejection, discovery escape rejection, format
merging, and the id grammar [config_test.go].
