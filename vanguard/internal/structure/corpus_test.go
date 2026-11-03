package structure

// Q-1.1.f-4: hostile inputs never crash or hang — the validator either
// returns findings or completes silently, deterministically, inside a time
// bound. There is no fuzz harness yet; this corpus is the bounded
// adversarial set.

import (
	"strings"
	"testing"
	"time"
)

func runBounded(t *testing.T, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		done <- struct{}{}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s hung", name)
	}
}

func TestAdversarialCorpusNeverCrashesOrHangs(t *testing.T) {
	dir := writeSchema(t, "p", profile)
	p, _ := Load(dir, "p")

	corpus := map[string]string{
		"huge body":         "# D\n\n## Interface\n\n" + strings.Repeat("- item [x.go:Y]\n", 20000),
		"many headings":     "# D\n\n" + strings.Repeat("## Section\n\n- a [x.go:Y]\n\n", 5000),
		"invalid utf-8":     "# D\n\n## Interface\n\n- a [x.go:Y]\n\xff\xfe\n## Invariants\n",
		"nul bytes":         "# D\n\n## Interface\x00\n\n- a [x.go:Y]\n",
		"crlf endings":      "# D\r\n\r\n## Interface\r\n\r\n- a [x.go:Y]\r\n\r\n## Invariants\r\n",
		"deep nesting":      "# D\n\n## Interface\n\n" + strings.Repeat("  ", 10000) + "- a [x.go:Y]\n",
		"heading storm dup": "# D\n\n" + strings.Repeat("## Interface\n\n- a [x.go:Y]\n\n", 3000),
		"empty":             "",
		"only separators":   "---\n---\n---\n",
	}
	for name, body := range corpus {
		runBounded(t, name, func() {
			v1 := vmd("d.md", []byte(body), p)
			v2 := vmd("d.md", []byte(body), p)
			if len(v1) != len(v2) {
				t.Fatalf("%s: non-deterministic finding count: %d vs %d", name, len(v1), len(v2))
			}
			for i := range v1 {
				if v1[i] != v2[i] {
					t.Fatalf("%s: non-deterministic findings at %d", name, i)
				}
			}
		})
	}
}

// The cap grammar still applies to hostile schema names: oversized and
// path-carrying names are rejected at load, never probed on disk.
func TestHostileSchemaNames(t *testing.T) {
	dir := t.TempDir()
	hostile := []string{
		strings.Repeat("a", 129),
		"../escape",
		"sub/dir",
		"..",
		"a\\b",
		"no spaces allowed",
		"trailing-dash-",
	}
	for _, name := range hostile {
		if _, err := Load(dir, name); err == nil {
			t.Fatalf("hostile schema name %q must be rejected", name)
		}
	}
}
