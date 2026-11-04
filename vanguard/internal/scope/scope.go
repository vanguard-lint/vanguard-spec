// Package scope implements the scope-hash formulas: the entry-list hash, the
// multi-glob combination, and the store-exclusion filters.
//
// scope_tree_hash = sha256(concat, for each glob, sorted by glob bytes:
//
//	glob || 0x00 || per-glob-hash || 0x0A)
//
// where a per-glob hash is either a kind-prefixed git tree OID (subtree
// strategy) or the enumerated entry hash
//
//	sha256(concat, for each entry, sorted by path bytes:
//	    path || 0x00 || mode || 0x00 || content-identity || 0x0A)
package scope

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/vanguard-lint/vanguard/internal/pathmatch"
)

// Entry is one file's identity inside a scope. Hash is the content identity:
// the git blob SHA in git mode, or the normalized "sha256:<hex>" content hash
// in HashProvider mode. Mode participates exactly as git records it.
type Entry struct {
	Path string
	Mode string
	Hash string
}

// FilterStore drops entries whose path lies inside the store directory —
// the enumeration strategy's path-prefix filter (construction point 3).
func FilterStore(entries []Entry, storeDir string) []Entry {
	if storeDir == "" {
		return entries
	}
	prefix := storeDir + "/"
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Path == storeDir || strings.HasPrefix(e.Path, prefix) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// HashEntries hashes an entry list over the exact formula. The list is sorted
// by path bytes first; the caller's order never matters.
func HashEntries(entries []Entry) string {
	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	h := sha256.New()
	var buf []byte
	for _, e := range sorted {
		buf = buf[:0]
		buf = append(buf, e.Path...)
		buf = append(buf, 0x00)
		buf = append(buf, e.Mode...)
		buf = append(buf, 0x00)
		buf = append(buf, e.Hash...)
		buf = append(buf, 0x0A)
		h.Write(buf)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// HashEnumerated hashes every entry matching the canonical glob, excluding
// store paths before hashing.
func HashEnumerated(glob string, entries []Entry, storeDir string) string {
	filtered := FilterStore(entries, storeDir)
	matched := make([]Entry, 0, len(filtered))
	for _, e := range filtered {
		if pathmatch.Match(glob, e.Path) {
			matched = append(matched, e)
		}
	}
	return HashEntries(matched)
}

// HashRepositoryWide hashes the synthetic level-1 list with the store's
// level-1 directory excluded by name (construction point 1). The caller
// passes the root listing: one entry per top-level file or tree.
func HashRepositoryWide(rootEntries []Entry, storeDir string) string {
	kept := make([]Entry, 0, len(rootEntries))
	for _, e := range rootEntries {
		name := e.Path
		if i := strings.IndexByte(name, '/'); i >= 0 {
			name = name[:i]
		}
		if storeDir != "" && name == storeDir {
			continue
		}
		kept = append(kept, e)
	}
	return HashEntries(kept)
}

// Pair couples a canonical glob with its per-glob hash.
type Pair struct {
	Glob string
	Hash string
}

// Combine hashes the per-glob results into the document's scope hash. Globs
// are sorted as written — the post-expansion canonical bytes. The digest is
// tagged scheme v2 so the kind survives hashing: a lock written by an older
// scheme (or a future one) fails the SameKind check loud — stale with a
// re-run-observe hint — instead of masquerading as ordinary staleness.
func Combine(pairs []Pair) string {
	sorted := make([]Pair, len(pairs))
	copy(sorted, pairs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Glob < sorted[j].Glob })

	h := sha256.New()
	var buf []byte
	for _, p := range sorted {
		buf = buf[:0]
		buf = append(buf, p.Glob...)
		buf = append(buf, 0x00)
		buf = append(buf, p.Hash...)
		buf = append(buf, 0x0A)
		h.Write(buf)
	}
	return "sha256v2:" + hex.EncodeToString(h.Sum(nil))
}

// SameKind reports whether two kind-prefixed hashes are comparable. A kind
// mismatch means the globs or the scope-hash scheme changed (stale, re-run-
// observe hint) — never an internal error.
func SameKind(a, b string) bool {
	return kindOf(a) == kindOf(b)
}

func kindOf(s string) string {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		return s[:i]
	}
	return ""
}
