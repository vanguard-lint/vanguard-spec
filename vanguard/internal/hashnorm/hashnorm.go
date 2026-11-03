// Package hashnorm implements vanguard's content-hash normalization pipeline.
//
// Every content hash in the lock is computed over a fixed, machine-independent
// pipeline, applied to text files before hashing:
//
//  1. Binary detection runs on the raw bytes before any transformation: a NUL
//     byte in the first 8 KiB marks the content binary, and binary content is
//     hashed raw, unchanged.
//  2. A leading UTF-8 BOM is stripped.
//  3. CRLF and lone CR fold to LF.
//  4. All trailing LF bytes at EOF are stripped.
//  5. Everything else is preserved exactly: no Unicode canonization, inner
//     whitespace untouched.
//
// The streaming Hasher holds trailing LFs in a pending buffer so the EOF rule
// survives chunk boundaries; CRLF pairs split across writes fold correctly.
package hashnorm

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
)

// binaryWindow is the fixed binary-detection window: the first 8 KiB of raw
// bytes decide whether content hashes raw or normalized.
const binaryWindow = 8 * 1024

// IsBinary reports whether data contains a NUL byte within the first 8 KiB.
func IsBinary(data []byte) bool {
	if len(data) > binaryWindow {
		data = data[:binaryWindow]
	}
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

// Sum returns "sha256:<hex>" of data over the normalization pipeline.
func Sum(data []byte) string {
	return "sha256:" + Hex(data)
}

// Hex returns the hex sha256 of data over the normalization pipeline.
func Hex(data []byte) string {
	h := New()
	_, _ = h.Write(data)
	return h.Hex()
}

// Hasher streams bytes through the pipeline with bounded memory. The first
// binaryWindow raw bytes are buffered for binary detection; everything after
// flows through the text pipeline (or raw, once binary). Write never fails.
type Hasher struct {
	sum       hash.Hash
	buf       []byte
	decided   bool
	binary    bool
	pre       [3]byte
	preN      int
	bomDone   bool
	pendingLF int
	crPending bool
}

// New returns a streaming pipeline hasher.
func New() *Hasher {
	return &Hasher{sum: sha256.New()}
}

// Write consumes p and always returns len(p), nil.
func (h *Hasher) Write(p []byte) (int, error) {
	if !h.decided {
		h.buf = append(h.buf, p...)
		if len(h.buf) < binaryWindow {
			return len(p), nil
		}
		h.decide()
		return len(p), nil
	}
	h.consume(p)
	return len(p), nil
}

// Hex finalizes the stream and returns the hex digest. The pending trailing
// LFs (and a trailing CR folded to LF) are dropped by the EOF rule.
func (h *Hasher) Hex() string {
	if !h.decided {
		h.decide()
	}
	if !h.binary {
		if !h.bomDone {
			// Fewer than three bytes arrived: no BOM possible — flush the
			// held bytes through the pipeline.
			h.bomDone = true
			for i := 0; i < h.preN; i++ {
				h.core(h.pre[i])
			}
			h.preN = 0
		}
		if h.crPending {
			h.crPending = false
			h.pendingLF++ // a trailing CR normalizes to LF, then strips at EOF
		}
	}
	return hex.EncodeToString(h.sum.Sum(nil))
}

func (h *Hasher) decide() {
	h.decided = true
	h.binary = IsBinary(h.buf)
	if h.binary {
		h.sum.Write(h.buf) // binary content hashes raw, unchanged
	} else {
		h.consume(h.buf)
	}
	h.buf = nil
}

func (h *Hasher) consume(p []byte) {
	if h.binary {
		h.sum.Write(p)
		return
	}
	for _, b := range p {
		h.feedByte(b)
	}
}

func (h *Hasher) feedByte(b byte) {
	if !h.bomDone {
		// Buffer the first three bytes; strip them when they form a UTF-8
		// BOM, otherwise replay them through the core pipeline.
		h.pre[h.preN] = b
		h.preN++
		if h.preN < 3 {
			return
		}
		h.bomDone = true
		h.preN = 0
		if h.pre[0] == 0xEF && h.pre[1] == 0xBB && h.pre[2] == 0xBF {
			return // BOM stripped
		}
		h.core(h.pre[0])
		h.core(h.pre[1])
		h.core(h.pre[2])
		return
	}
	h.core(b)
}

// core folds line endings and strips trailing LFs from the hashed bytes.
func (h *Hasher) core(b byte) {
	switch {
	case b == '\r':
		if h.crPending {
			h.lf() // the previous CR stands alone: it folds to LF
		}
		h.crPending = true
	case h.crPending:
		h.crPending = false
		if b == '\n' {
			h.lf() // CRLF folds to exactly one LF
			return
		}
		h.lf()
		h.emit(b)
	case b == '\n':
		h.lf()
	default:
		h.emit(b)
	}
}

func (h *Hasher) lf() { h.pendingLF++ }

func (h *Hasher) emit(b byte) {
	var lf [1]byte
	lf[0] = '\n'
	for i := 0; i < h.pendingLF; i++ {
		h.sum.Write(lf[:])
	}
	h.pendingLF = 0
	h.sum.Write([]byte{b})
}
