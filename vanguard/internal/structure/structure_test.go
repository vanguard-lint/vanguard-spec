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

const nestedInAnyOf = `{
  "anyOf": [
    {
      "x-vanguard": {
        "required_sections": ["Interface", "Invariants"],
        "citation_sections": ["Interface"],
        "citation_pattern": "\\[[a-z0-9_./-]+\\.go:[A-Za-z0-9_]+\\]"
      }
    }
  ]
}`

func TestNestedXVanguardInAnyOfIsEnforced(t *testing.T) {
	dir := writeSchema(t, "nested", nestedInAnyOf)
	p, err := Load(dir, "nested")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.RequiredSections) != 2 {
		t.Fatalf("nested contract not loaded: %+v", p)
	}
	body := doc("# t\n\n## Interface\n\n- claim without citation\n\n## Invariants\n\n- cited [b.go:Sym]\n")
	assertRules(t, rules(vmd("f.md", body, p)), []string{"structure_missing_citation"})
}

func TestAmbiguousNestedXVanguardFailsClosed(t *testing.T) {
	dir := writeSchema(t, "ambiguous", `{
  "anyOf": [{"x-vanguard": {"required_sections": ["A"]}}],
  "oneOf": [{"x-vanguard": {"required_sections": ["B"]}}]
}`)
	if _, err := Load(dir, "ambiguous"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("distinct nested contracts must fail closed, got: %v", err)
	}
}

func TestIdenticalNestedAndTopLevelXVanguardIsOneContract(t *testing.T) {
	dir := writeSchema(t, "same", `{
  "x-vanguard": {"required_sections": ["A"]},
  "anyOf": [{"x-vanguard": {"required_sections": ["A"]}}]
}`)
	if _, err := Load(dir, "same"); err != nil {
		t.Fatalf("identical copies are one contract: %v", err)
	}
}

func TestCitationOnContinuationLineSatisfiesTheClaim(t *testing.T) {
	dir := writeSchema(t, "p", profile)
	p, err := Load(dir, "p")
	if err != nil {
		t.Fatal(err)
	}
	// The citation sits on a wrapped continuation line: the whole item is
	// the claim. Checking only the bullet's first line phantom-fails this.
	body := doc("# t\n\n## Interface\n\n- `Load(store, opts)` reads config and\n  the lock; a missing lock is empty [engine.go:Load].\n\n- no citation anywhere in this item\n\n## Invariants\n\n- cited [b.go:Sym]\n")
	assertRules(t, rules(vmd("f.md", body, p)), []string{"structure_missing_citation"})
}

func TestFenceOnlySectionsAreStillMissing(t *testing.T) {
	dir := writeSchema(t, "p", profile)
	p, err := Load(dir, "p")
	if err != nil {
		t.Fatal(err)
	}
	// The required sections exist only inside a fence: quoted examples do
	// not satisfy the contract — the old scanner false-passed here.
	body := doc("# t\n\n```md\n## Interface\n## Invariants\n```\n")
	assertRules(t, rules(vmd("f.md", body, p)), []string{"structure_missing_section", "structure_missing_section"})
}

func TestQuotedHeadingIsNotADuplicate(t *testing.T) {
	dir := writeSchema(t, "p", profile)
	p, err := Load(dir, "p")
	if err != nil {
		t.Fatal(err)
	}
	// A heading quoted inside a fence is not a second section: the old
	// scanner raised a phantom duplicate here.
	body := doc("# t\n\n```md\n## Interface\n```\n\n## Interface\n\n- cited [a.go:Sym]\n\n## Invariants\n\n- cited [b.go:Sym]\n")
	assertRules(t, rules(vmd("f.md", body, p)), nil)
}

func TestFencedBulletIsNotACitationClaim(t *testing.T) {
	dir := writeSchema(t, "p", profile)
	p, err := Load(dir, "p")
	if err != nil {
		t.Fatal(err)
	}
	// The uncited example bullet lives in a fence: no phantom violation.
	body := doc("# t\n\n## Interface\n\n```yaml\n- not a claim\n```\n\n- cited [a.go:Sym]\n\n## Invariants\n\n- cited [b.go:Sym]\n")
	assertRules(t, rules(vmd("f.md", body, p)), nil)
}

func TestFencedHeadingDoesNotViolateMaxDepth(t *testing.T) {
	dir := writeSchema(t, "deep", `{"x-vanguard": {"max_depth": 2}}`)
	p, err := Load(dir, "deep")
	if err != nil {
		t.Fatal(err)
	}
	body := doc("# t\n\n```sh\n# a commented line, not a heading\n```\n\n### real\n")
	assertRules(t, rules(vmd("f.md", body, p)), []string{"structure_max_depth"})
}
