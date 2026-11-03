---
id: hashnorm
watches:
  paths:
    - "vanguard/internal/hashnorm/**/*.go"
  review_after: 30d
---

# hashnorm — Content Normalization Pipeline

Fixed, machine-independent normalization before every content hash: binary
detection on the first 8 KiB, UTF-8 BOM strip, CR fold, trailing-LF strip at
EOF, everything else byte-exact. No Unicode canonization — NFC and NFD forms
hash differently by design. All lockfile content hashes flow through this
pipeline; the streaming `Hasher` holds trailing LFs and a pending CR in state
so the EOF rule and CRLF folding survive chunk boundaries.

## Interface

- `IsBinary(data []byte) bool` — true iff a NUL byte exists within the first
  8 KiB (`binaryWindow`) [hashnorm.go:IsBinary].
- `Sum(data []byte) string` — `"sha256:<hex>"` over the pipeline
  [hashnorm.go:Sum]; `Hex` is the same without the prefix.
- `Hasher` + `New()` — streaming hasher with bounded memory; buffers the
  first 8 KiB for binary detection, then streams text or raw bytes
  [hashnorm.go:Hasher].

## Invariants

- The binary window is fixed at 8 KiB: a NUL at index 8191 is inside, at 8192
  outside [hashnorm.go:binaryWindow].
- Binary content hashes raw — `decide` writes the buffer straight to the
  digest [hashnorm.go:decide].
- The first 3 bytes are buffered and either stripped as a UTF-8 BOM or
  replayed [hashnorm.go:feedByte].
- CRLF folds to one LF; a lone CR also folds; trailing LFs strip at EOF
  [hashnorm.go:core, hashnorm.go:emit].
- `Write` never fails — it always returns `len(p), nil` [hashnorm.go:Hasher.Write].

## Side effects

None — pure functions over byte slices.

## Verification

All 8 mandatory vectors have tests (`TestMandatoryVectors`, vectors 7–8 in
`TestChunkBoundaries`), plus chunk-boundary identity
(`TestStreamingMatchesWhole`), lone-CR folding, and whole-vs-stream equality
[hashnorm_test.go].
