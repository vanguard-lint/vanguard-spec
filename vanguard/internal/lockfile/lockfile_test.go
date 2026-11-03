package lockfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleLock() *Lock {
	return &Lock{
		Version: 1,
		Docs: map[string]Entry{
			"payments-spec": {
				Anchor:         "e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3",
				ObservedAt:     "2026-11-03T09:12:00Z",
				ScopeTreeHash:  "tree:7f8a9b0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e",
				ScopeFileCount: 14,
				ContentHash:    "sha256:9f2a1c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b",
			},
			"glossary-core": {
				ObservedAt:  "2026-11-02T18:40:11Z",
				ContentHash: "sha256:5be11c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b",
			},
		},
	}
}

func TestCanonicalForm(t *testing.T) {
	out := Serialize(sampleLock())
	s := string(out)
	// ids sorted lexicographically
	if strings.Index(s, "glossary-core") > strings.Index(s, "payments-spec") {
		t.Fatal("ids must be sorted")
	}
	// 2-space indent, LF endings, trailing newline
	if !strings.HasSuffix(s, "}\n") || strings.Contains(s, "\r") {
		t.Fatalf("canonical form: %q", s[:min(60, len(s))])
	}
	// fields in fixed order inside an entry
	entry := s[strings.Index(s, "payments-spec"):]
	anchor := strings.Index(entry, "\"anchor\"")
	observed := strings.Index(entry, "\"observed_at\"")
	scope := strings.Index(entry, "\"scope_tree_hash\"")
	content := strings.Index(entry, "\"content_hash\"")
	if !(anchor < observed && observed < scope && scope < content) {
		t.Fatal("field order must be fixed: anchor, observed_at, scope_tree_hash, content_hash")
	}
	// byte-stable across serializations
	if string(Serialize(sampleLock())) != s {
		t.Fatal("serialization must be deterministic")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestRoundTrip(t *testing.T) {
	in := sampleLock()
	out, err := Parse(Serialize(in))
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != 1 || len(out.Docs) != 2 {
		t.Fatalf("round trip: %+v", out)
	}
	e := out.Docs["payments-spec"]
	if e.Anchor != in.Docs["payments-spec"].Anchor ||
		e.ScopeTreeHash != in.Docs["payments-spec"].ScopeTreeHash ||
		e.ScopeFileCount != 14 {
		t.Fatalf("entry = %+v", e)
	}
	// review_after-only shape
	g := out.Docs["glossary-core"]
	if g.Anchor != "" || g.ScopeTreeHash != "" {
		t.Fatalf("minimal entry must carry no anchor or scope hash: %+v", g)
	}
}

func TestEmptyDocsSerializeInline(t *testing.T) {
	// {"version": 1, "docs": {}} is the exact init scaffold.
	out := string(Serialize(&Lock{Version: 1, Docs: map[string]Entry{}}))
	if out != "{\n  \"version\": 1,\n  \"docs\": {}\n}\n" {
		t.Fatalf("init scaffold bytes: %q", out)
	}
}

func TestUnknownFieldsPreserved(t *testing.T) {
	raw := []byte("{\"version\":1,\"docs\":{\"x\":{\"observed_at\":\"2026-01-01T00:00:00Z\",\"content_hash\":\"sha256:ab\",\"future_field\":42}}}")
	lock, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	e := lock.Docs["x"]
	if _, ok := e.unknown["future_field"]; !ok {
		t.Fatal("unknown field must be captured")
	}
	out := string(Serialize(lock))
	if !strings.Contains(out, "\"future_field\": 42") {
		t.Fatalf("unknown field must survive a rewrite: %s", out)
	}
}

func TestCorruptLockFails(t *testing.T) {
	for _, raw := range []string{
		"not json at all",
		"{\"docs\":{}}",               // missing version
		"{\"version\":2,\"docs\":{}}", // unknown major
		"{\"version\":1,\"docs\":{\"x\":{\"content_hash\":12}}}",
	} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Fatalf("Parse(%q) must fail", raw)
		}
	}
}

func TestSaveCASAndAtomicity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vanguard.lock")
	lock := sampleLock()
	captured := Serialize(lock)
	if err := os.WriteFile(path, captured, 0o644); err != nil {
		t.Fatal(err)
	}

	// CAS passes when bytes are unchanged.
	lock.Docs["new-doc"] = Entry{ObservedAt: "2026-11-03T10:00:00Z", ContentHash: "sha256:cc"}
	if err := Save(path, lock, captured); err != nil {
		t.Fatalf("save: %v", err)
	}

	// CAS aborts when the file moved underneath.
	stale := captured
	lock2, err := Parse(stale)
	if err != nil {
		t.Fatal(err)
	}
	lock2.Docs["other-doc"] = Entry{ObservedAt: "2026-11-03T11:00:00Z", ContentHash: "sha256:dd"}
	if err := Save(path, lock2, stale); err == nil {
		t.Fatal("stale capture must abort")
	}

	// No temp files left behind.
	matches, _ := filepath.Glob(path + ".tmp-*")
	if len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
}

func TestSnapshotShape(t *testing.T) {
	lock := &Lock{Version: 1, Docs: map[string]Entry{
		"runbook-ops": {
			ObservedAt:  "2026-11-02T18:40:11Z",
			ContentHash: "sha256:3b71c0",
			ScopeSnapshot: map[string]string{
				"ops/runbook.txt": "sha256:c01d22",
			},
		},
	}}
	out, err := Parse(Serialize(lock))
	if err != nil {
		t.Fatal(err)
	}
	e := out.Docs["runbook-ops"]
	if e.ScopeSnapshot["ops/runbook.txt"] != "sha256:c01d22" {
		t.Fatalf("snapshot = %v", e.ScopeSnapshot)
	}
	if e.Anchor != "" {
		t.Fatal("hash-mode entry carries no anchor")
	}
}
