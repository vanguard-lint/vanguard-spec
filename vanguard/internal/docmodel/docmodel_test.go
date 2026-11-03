package docmodel

import (
	"os"
	"path/filepath"
	"testing"
)

func writeDoc(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const formats = "md=markdown" // not used directly; see Discover calls

func mdFront(id string) string {
	return "---\nid: " + id + "\nwatches:\n  paths: [\"src/payments/**\"]\n  review_after: 90d\n---\n\n# Body\n"
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "b-second.md", mdFront("b-doc"))
	writeDoc(t, root, "a-first.md", mdFront("a-doc"))
	writeDoc(t, root, "skip.md", "no frontmatter block")

	docs, _, err := Discover(root, []string{"**/*.md"}, map[string]string{"md": "markdown"})
	// A markdown document without a frontmatter block is malformed — the
	// engine id cannot be found — and fails closed.
	if err == nil {
		t.Fatal("markdown without frontmatter must fail closed")
	}
	_ = docs
}

func TestDiscoverHappyPath(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "b-second.md", mdFront("b-doc"))
	writeDoc(t, root, "a-first.md", mdFront("a-doc"))

	docs, warnings, err := Discover(root, []string{"**/*.md"}, map[string]string{"md": "markdown"})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("discovered %d docs: %+v (warnings %v)", len(docs), docs, warnings)
	}
	// Lexicographic path order.
	if docs[0].RelPath != "a-first.md" || docs[1].RelPath != "b-second.md" {
		t.Fatalf("order: %s, %s", docs[0].RelPath, docs[1].RelPath)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %+v", warnings)
	}
	d := docs[1]
	if d.ID != "b-doc" || d.Format != "markdown" {
		t.Fatalf("doc = %+v", d)
	}
	if len(d.PathGlobs) != 1 || d.PathGlobs[0] != "src/payments/**" {
		t.Fatalf("path globs = %v", d.PathGlobs)
	}
	if d.ReviewAfterSeconds != 90*24*3600 {
		t.Fatalf("review_after seconds = %v", d.ReviewAfterSeconds)
	}
}

func TestDiscoverUnknownFormatSkips(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "x.toml", "---\nid: x\n---\n")
	_, warnings, err := Discover(root, []string{"**/*"}, map[string]string{"md": "markdown"})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || warnings[0].File != "x.toml" {
		t.Fatalf("warnings = %+v", warnings)
	}
}

func TestDuplicateIDIsFatal(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "one.md", mdFront("same"))
	writeDoc(t, root, "two.md", mdFront("same"))
	if _, _, err := Discover(root, []string{"**/*.md"}, map[string]string{"md": "markdown"}); err == nil {
		t.Fatal("duplicate id must be fatal")
	}
}

func TestIDGrammarEnforced(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "bad.md", "---\nid: Bad-ID\n---\nbody")
	if _, _, err := Discover(root, []string{"**/*.md"}, map[string]string{"md": "markdown"}); err == nil {
		t.Fatal("bad id must be fatal")
	}
}

func TestBraceWatchExpansion(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "d.md", "---\nid: d\nwatches:\n  paths: [\"src/{a,b}/**\"]\n---\n")
	docs, _, err := Discover(root, []string{"**/*.md"}, map[string]string{"md": "markdown"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"src/a/**", "src/b/**"}
	if len(docs[0].PathGlobs) != 2 {
		t.Fatalf("globs = %v", docs[0].PathGlobs)
	}
	for i := range want {
		if docs[0].PathGlobs[i] != want[i] {
			t.Fatalf("globs = %v", docs[0].PathGlobs)
		}
	}
}

func TestYAMLDocument(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "spec.yaml", "id: yaml-doc\nwatches:\n  paths: [\"api/**\"]\n")
	docs, _, err := Discover(root, []string{"**/*.yaml"}, map[string]string{"yaml": "yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].ID != "yaml-doc" || docs[0].PathGlobs[0] != "api/**" {
		t.Fatalf("docs = %+v", docs)
	}
}

func TestDSLMetadataBlock(t *testing.T) {
	root := t.TempDir()
	content := "// id: c4-payments\n// schema: structurizr\nworkspace \"Payments\" {\n"
	writeDoc(t, root, "model.c4", content)
	docs, _, err := Discover(root, []string{"**/*.c4"}, map[string]string{"c4": "dsl"})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].ID != "c4-payments" || docs[0].Schema != "structurizr" {
		t.Fatalf("docs = %+v", docs)
	}
}

func TestDSLFieldsOutsideLeadingBlockAreInvisible(t *testing.T) {
	raw := "workspace {\n// id: too-late\n}\n"
	if _, err := parseDocument("x.c4", "dsl", []byte(raw)); err == nil {
		t.Fatal("engine fields must live in the leading comment block")
	}
}

func TestAnchorAndAliasInFrontmatterRejected(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "a.md", "---\nbase: &x v\nid: *x\n---\n")
	if _, _, err := Discover(root, []string{"**/*.md"}, map[string]string{"md": "markdown"}); err == nil {
		t.Fatal("aliases must be fatal")
	}
}

func TestBOMTolerated(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "bom.md", "\xEF\xBB\xBF---\nid: bom-doc\n---\nbody")
	docs, _, err := Discover(root, []string{"**/*.md"}, map[string]string{"md": "markdown"})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].ID != "bom-doc" {
		t.Fatalf("docs = %+v", docs)
	}
}

func TestReviewAfterGrammarEnforced(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "bad.md", "---\nid: bad\nwatches:\n  review_after: 30s\n---\n")
	if _, _, err := Discover(root, []string{"**/*.md"}, map[string]string{"md": "markdown"}); err == nil {
		t.Fatal("seconds are not in the review_after grammar")
	}
}
