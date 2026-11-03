// Package lockfile reads and writes vanguard.lock — the shared attestation
// record, committed like go.sum. Canonical form: document ids sorted
// lexicographically, fields in a fixed order, 2-space indent, LF endings,
// trailing newline, no comments. Observe is the only writer; writes are
// atomic (temp + rename) and compare-and-swap on content.
package lockfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// Version is the lock schema version. An unknown major fails closed.
const Version = 1

// Entry is one document's baseline. Shape varies with the declared watches
// and records the provider mode at observe time: anchor present = git mode,
// scope_snapshot present = HashProvider mode; a review_after-only document
// stores observed_at + content_hash only.
type Entry struct {
	Anchor         string
	ObservedAt     string
	ScopeTreeHash  string
	ScopeFileCount int
	ContentHash    string
	ScopeSnapshot  map[string]string

	// unknown preserves fields this version does not know (forward
	// compatibility across minor versions).
	unknown map[string]json.RawMessage
}

// Lock is the parsed vanguard.lock.
type Lock struct {
	Version int
	Docs    map[string]Entry
}

// Load reads and parses the lock. raw is the exact on-disk bytes for the
// observe-time compare-and-swap.
func Load(path string) (*Lock, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	lock, err := Parse(raw)
	if err != nil {
		return nil, raw, err
	}
	return lock, raw, nil
}

// Parse decodes lock bytes (exposed for tests).
func Parse(raw []byte) (*Lock, error) {
	var wire struct {
		Version *int                       `json:"version"`
		Docs    map[string]json.RawMessage `json:"docs"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&wire); err != nil {
		return nil, fmt.Errorf("vanguard.lock is corrupt: %w", err)
	}
	lock := &Lock{Docs: map[string]Entry{}}
	if wire.Version == nil {
		return nil, fmt.Errorf("vanguard.lock is corrupt: missing version")
	}
	lock.Version = *wire.Version
	if lock.Version != Version {
		return nil, fmt.Errorf("unsupported lock version %d — upgrade vanguard or restore the lock from git", lock.Version)
	}
	for id, rawEntry := range wire.Docs {
		entry, err := parseEntry(rawEntry)
		if err != nil {
			return nil, fmt.Errorf("vanguard.lock entry %q: %w", id, err)
		}
		lock.Docs[id] = entry
	}
	return lock, nil
}

func parseEntry(raw json.RawMessage) (Entry, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return Entry{}, fmt.Errorf("is corrupt: %w", err)
	}
	entry := Entry{unknown: map[string]json.RawMessage{}}
	known := map[string]bool{}
	get := func(key string) (json.RawMessage, bool) {
		known[key] = true
		v, ok := m[key]
		return v, ok
	}
	if v, ok := get("anchor"); ok {
		if err := json.Unmarshal(v, &entry.Anchor); err != nil {
			return entry, fmt.Errorf("anchor: %w", err)
		}
	}
	if v, ok := get("observed_at"); ok {
		if err := json.Unmarshal(v, &entry.ObservedAt); err != nil {
			return entry, fmt.Errorf("observed_at: %w", err)
		}
	}
	if v, ok := get("scope_tree_hash"); ok {
		if err := json.Unmarshal(v, &entry.ScopeTreeHash); err != nil {
			return entry, fmt.Errorf("scope_tree_hash: %w", err)
		}
	}
	if v, ok := get("scope_file_count"); ok {
		if err := json.Unmarshal(v, &entry.ScopeFileCount); err != nil {
			return entry, fmt.Errorf("scope_file_count: %w", err)
		}
	}
	if v, ok := get("content_hash"); ok {
		if err := json.Unmarshal(v, &entry.ContentHash); err != nil {
			return entry, fmt.Errorf("content_hash: %w", err)
		}
	}
	if v, ok := get("scope_snapshot"); ok {
		if err := json.Unmarshal(v, &entry.ScopeSnapshot); err != nil {
			return entry, fmt.Errorf("scope_snapshot: %w", err)
		}
	}
	for k, v := range m {
		if !known[k] {
			entry.unknown[k] = v
		}
	}
	return entry, nil
}

// Serialize renders the canonical form.
func Serialize(lock *Lock) []byte {
	ids := make([]string, 0, len(lock.Docs))
	for id := range lock.Docs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var buf bytes.Buffer
	buf.WriteString("{\n  \"version\": ")
	fmt.Fprintf(&buf, "%d", lock.Version)
	buf.WriteString(",\n  \"docs\": {")
	if len(ids) == 0 {
		buf.WriteString("}\n}\n")
		return buf.Bytes()
	}
	buf.WriteString("\n")
	for i, id := range ids {
		buf.WriteString("    ")
		writeJSONString(&buf, id)
		buf.WriteString(": ")
		entryJSON(&buf, lock.Docs[id])
		if i < len(ids)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("  }\n}\n")
	return buf.Bytes()
}

func entryJSON(buf *bytes.Buffer, e Entry) {
	var inner bytes.Buffer
	inner.WriteString("{")
	first := true
	put := func(key, val string) {
		if !first {
			inner.WriteString(",")
		}
		first = false
		inner.WriteString("\n      ")
		writeJSONString(&inner, key)
		inner.WriteString(": ")
		writeJSONString(&inner, val)
	}
	putInt := func(key string, n int) {
		if !first {
			inner.WriteString(",")
		}
		first = false
		inner.WriteString("\n      ")
		writeJSONString(&inner, key)
		fmt.Fprintf(&inner, ": %d", n)
	}
	if e.Anchor != "" {
		put("anchor", e.Anchor)
	}
	put("observed_at", e.ObservedAt)
	if e.ScopeTreeHash != "" {
		put("scope_tree_hash", e.ScopeTreeHash)
	}
	if e.ScopeFileCount != 0 {
		putInt("scope_file_count", e.ScopeFileCount)
	}
	put("content_hash", e.ContentHash)
	if len(e.ScopeSnapshot) > 0 {
		if !first {
			inner.WriteString(",")
		}
		first = false
		inner.WriteString("\n      \"scope_snapshot\": {")
		keys := make([]string, 0, len(e.ScopeSnapshot))
		for k := range e.ScopeSnapshot {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			if i > 0 {
				inner.WriteString(",")
			}
			inner.WriteString("\n        ")
			writeJSONString(&inner, k)
			inner.WriteString(": ")
			writeJSONString(&inner, e.ScopeSnapshot[k])
		}
		inner.WriteString("\n      }")
	}
	// Unknown fields ride along, sorted, after the known ones.
	ukeys := make([]string, 0, len(e.unknown))
	for k := range e.unknown {
		ukeys = append(ukeys, k)
	}
	sort.Strings(ukeys)
	for _, k := range ukeys {
		if !first {
			inner.WriteString(",")
		}
		first = false
		inner.WriteString("\n      ")
		writeJSONString(&inner, k)
		inner.WriteString(": ")
		inner.Write(e.unknown[k])
	}
	inner.WriteString("\n    }")
	buf.Write(inner.Bytes())
}

func writeJSONString(buf *bytes.Buffer, s string) {
	enc, err := json.Marshal(s)
	if err != nil {
		panic(err) // strings always marshal
	}
	buf.Write(enc)
}

// Save writes the lock via an atomic temp-file rename. When captured is nil
// the lockfile must still be absent (the first observe creates it);
// otherwise the on-disk bytes must equal captured — the compare-and-swap
// that keeps concurrent observes from trampling each other.
func Save(path string, lock *Lock, captured []byte) error {
	current, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && captured == nil {
			return atomicWrite(path, Serialize(lock))
		}
		return fmt.Errorf("lock changed during observe — re-run: %w", err)
	}
	if !bytes.Equal(current, captured) {
		return fmt.Errorf("lock changed during observe — re-run")
	}
	return atomicWrite(path, Serialize(lock))
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp-" + shortHash(data)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func shortHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:4])
}
