package structure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const profile = `{
  "x-vanguard": {
    "format": "markdown",
    "required_sections": ["Interface", "Invariants"],
    "citation_sections": ["Interface"],
    "citation_pattern": "\\[[a-z0-9_./-]+\\.go:[A-Za-z0-9_]+\\]"
  }
}`

const existenceOnly = `{"title": "no x-vanguard block"}`

func writeSchema(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name+".schema.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func doc(body string) []byte {
	return []byte(body)
}

// vmd validates as a markdown document (the common case in tests).
func vmd(file string, body []byte, p *Profile) []Violation {
	return Validate(file, body, p, "markdown")
}

func rules(violations []Violation) []string {
	var out []string
	for _, v := range violations {
		out = append(out, v.Rule)
	}
	return out
}

func assertRules(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rules = %v, want %v", got, want)
	}
}

func TestDeclaredButMissingSchemaIsFatal(t *testing.T) {
	dir := writeSchema(t, "real", profile)
	_, err := Load(dir, "ghost")
	if err == nil || !strings.Contains(err.Error(), "declared but not found") {
		t.Fatalf("missing schema must fail closed, got %v", err)
	}
}

func TestCorruptSchemaIsFatal(t *testing.T) {
	dir := writeSchema(t, "broken", "{not json")
	if _, err := Load(dir, "broken"); err == nil {
		t.Fatal("corrupt schema must fail closed")
	}
}

func TestInvalidCitationPatternIsFatal(t *testing.T) {
	dir := writeSchema(t, "badre", `{"x-vanguard": {"citation_pattern": "["}}`)
	if _, err := Load(dir, "badre"); err == nil || !strings.Contains(err.Error(), "citation_pattern") {
		t.Fatalf("invalid regex must fail closed, got %v", err)
	}
}

func TestMaxDepthCapIsConfigValidation(t *testing.T) {
	dir := writeSchema(t, "deep", `{"x-vanguard": {"max_depth": 33}}`)
	if _, err := Load(dir, "deep"); err == nil || !strings.Contains(err.Error(), "cap is 32") {
		t.Fatalf("max_depth over the cap must fail closed at load, got %v", err)
	}
	dir = writeSchema(t, "neg", `{"x-vanguard": {"max_depth": -1}}`)
	if _, err := Load(dir, "neg"); err == nil || !strings.Contains(err.Error(), "negative") {
		t.Fatalf("negative max_depth must fail closed at load, got %v", err)
	}
}

func TestMaxDepthEnforced(t *testing.T) {
	p := &Profile{MaxDepth: 3}
	violations := vmd("d.md", doc("# t\n\n## ok\n\n#### too deep\n"), p)
	if len(violations) != 1 || violations[0].Rule != "structure_max_depth" || violations[0].Line != 5 {
		t.Fatalf("depth-4 heading must violate max_depth 3 at line 5, got %v", violations)
	}
	if got := vmd("d.md", doc("# t\n\n## ok\n\n### fine\n"), p); len(got) != 0 {
		t.Fatalf("depth-3 heading is within max_depth, got %v", got)
	}
	// Shebangs and tags are not ATX headings.
	if got := vmd("d.md", doc("#!/usr/bin/env sh\n####not-a-heading\n"), p); len(got) != 0 {
		t.Fatalf("non-ATX # lines must not violate, got %v", got)
	}
}

func TestExistenceOnlySchemaValidatesNothing(t *testing.T) {
	dir := writeSchema(t, "thin", existenceOnly)
	p, err := Load(dir, "thin")
	if err != nil {
		t.Fatal(err)
	}
	assertRules(t, rules(vmd("d.md", doc("# Anything\n\nno sections at all\n"), p)), nil)
}

func TestSchemaNameGrammar(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"", ".hidden", "-lead", "UPPER", "has space"} {
		if _, err := Load(dir, name); err == nil {
			t.Fatalf("name %q must be rejected", name)
		}
	}
}

func TestMissingSectionCaught(t *testing.T) {
	dir := writeSchema(t, "p", profile)
	p, _ := Load(dir, "p")
	body := doc("# Doc\n\n## Interface\n\n- a [x.go:Y]\n")
	assertRules(t, rules(vmd("d.md", body, p)), []string{"structure_missing_section"})
}

func TestSectionOrderCaught(t *testing.T) {
	dir := writeSchema(t, "p", profile)
	p, _ := Load(dir, "p")
	body := doc("# Doc\n\n## Invariants\n\n- i [x.go:Y]\n\n## Interface\n\n- a [x.go:Y]\n")
	got := vmd("d.md", body, p)
	assertRules(t, rules(got), []string{"structure_section_order"})
	if got[0].Line != 3 {
		t.Fatalf("order violation must point at the misplaced heading, got line %d", got[0].Line)
	}
}

func TestDuplicateSectionCaught(t *testing.T) {
	dir := writeSchema(t, "p", profile)
	p, _ := Load(dir, "p")
	body := doc("# Doc\n\n## Interface\n\n- a [x.go:Y]\n\n## Interface\n\n- b [x.go:Y]\n\n## Invariants\n\n- i [x.go:Y]\n")
	assertRules(t, rules(vmd("d.md", body, p)), []string{"structure_duplicate_section"})
}

func TestUncitedClaimCaughtWithLine(t *testing.T) {
	dir := writeSchema(t, "p", profile)
	p, _ := Load(dir, "p")
	body := doc("# Doc\n\n## Interface\n\n- a [x.go:Y]\n- b: trust me\n\n## Invariants\n\n- i: no citation needed here\n")
	got := vmd("d.md", body, p)
	assertRules(t, rules(got), []string{"structure_missing_citation"})
	if got[0].Line != 6 {
		t.Fatalf("citation violation must point at the uncited bullet, got line %d", got[0].Line)
	}
}

func TestValidDocumentPasses(t *testing.T) {
	dir := writeSchema(t, "p", profile)
	p, _ := Load(dir, "p")
	body := doc("# Doc\n\n## Interface\n\n- a [x.go:Y]\n\n## Invariants\n\n- i: prose ok\n")
	assertRules(t, rules(vmd("d.md", body, p)), nil)
}

func TestSectionNameIsCaseExact(t *testing.T) {
	dir := writeSchema(t, "p", profile)
	p, _ := Load(dir, "p")
	body := doc("# Doc\n\n## interface\n\n- a [x.go:Y]\n")
	assertRules(t, rules(vmd("d.md", body, p)),
		[]string{"structure_missing_section", "structure_missing_section"})
}

func TestFormatMismatchIsLoud(t *testing.T) {
	dir := writeSchema(t, "p", profile)
	p, _ := Load(dir, "p")
	got := Validate("d.json", doc("{\"id\": 1}"), p, "json")
	assertRules(t, rules(got), []string{"structure_schema_format_mismatch"})
}

func TestNilProfileValidatesNothing(t *testing.T) {
	assertRules(t, rules(Validate("d.md", doc("junk"), nil, "markdown")), nil)
}
