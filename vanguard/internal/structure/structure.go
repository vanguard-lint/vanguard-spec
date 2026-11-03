// Package structure enforces declared document schemas. A document that
// declares `schema: <name>` binds itself to .vanguard/schemas/<name>.schema.json;
// the schema's x-vanguard block carries the markdown structure contract
// (required sections, citation rules). Loading fails closed: a declared but
// missing or corrupt schema is fatal, never silently skipped — a document
// that claims a binding it does not have is exactly the lie this package
// exists to catch.
package structure

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Profile is the x-vanguard enforcement block inside a schema file.
type Profile struct {
	Format           string   `json:"format"`            // "markdown"; empty means the sections apply to markdown too
	RequiredSections []string `json:"required_sections"` // H2 titles, in order
	CitationSections []string `json:"citation_sections"` // H2 titles whose bullets must carry a citation
	CitationPattern  string   `json:"citation_pattern"`  // regex every bullet in a citation section must match
}

// Violation is one structure finding, located in the document.
type Violation struct {
	Rule    string
	File    string
	Line    int
	Message string
}

// Load reads <schemasDir>/<name>.schema.json and extracts the x-vanguard
// profile. A schema without an x-vanguard block binds existence only — the
// declaration is checked, the body is not. An invalid citation pattern is a
// corrupt schema, fatal at the call site.
func Load(schemasDir, name string) (*Profile, error) {
	if !validName(name) {
		return nil, fmt.Errorf("schema name %q is not a valid id", name)
	}
	raw, err := os.ReadFile(filepath.Join(schemasDir, name+".schema.json"))
	if err != nil {
		return nil, fmt.Errorf("schema %q is declared but not found in %s: %w", name, schemasDir, err)
	}
	var doc struct {
		XVanguard *Profile `json:"x-vanguard"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("schema %q is not valid JSON: %w", name, err)
	}
	if doc.XVanguard == nil {
		return &Profile{}, nil
	}
	p := doc.XVanguard
	if p.CitationPattern != "" {
		if _, err := regexp.Compile(p.CitationPattern); err != nil {
			return nil, fmt.Errorf("schema %q has an invalid citation_pattern: %w", name, err)
		}
	}
	return p, nil
}

// Validate checks one document body against the profile and returns every
// finding. format is the document's own format ("markdown", "json", ...);
// declaring a markdown structure contract on a non-markdown document is a
// finding in itself. Line numbers are 1-based and point at the offending
// heading or bullet. A nil profile validates nothing.
func Validate(file string, body []byte, prof *Profile, format string) []Violation {
	if prof == nil {
		return nil
	}
	var out []Violation
	if prof.Format != "" && format != "" && prof.Format != format {
		return []Violation{{
			Rule:    "structure_schema_format_mismatch",
			File:    file,
			Line:    1,
			Message: fmt.Sprintf("a %q structure contract cannot bind a non-markdown document", prof.Format),
		}}
	}

	// Headings: every H2 with its 1-based line, first occurrence wins.
	type heading struct {
		title string
		line  int
	}
	var headings []heading
	first := map[string]int{}
	for i, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		title := strings.TrimSpace(line[3:])
		headings = append(headings, heading{title: title, line: i + 1})
		if _, ok := first[title]; !ok {
			first[title] = i + 1
		}
	}

	at := func(title string) (int, bool) {
		line, ok := first[title]
		return line, ok
	}

	// Required sections: present, in order, no duplicates.
	prevLine := 0
	prevTitle := ""
	for _, want := range prof.RequiredSections {
		line, ok := at(want)
		if !ok {
			out = append(out, Violation{
				Rule:    "structure_missing_section",
				File:    file,
				Line:    1,
				Message: fmt.Sprintf("required section %q is missing", want),
			})
			continue
		}
		if line < prevLine {
			out = append(out, Violation{
				Rule:    "structure_section_order",
				File:    file,
				Line:    line,
				Message: fmt.Sprintf("section %q appears after %q; the contract order is %q then %q", want, prevTitle, prevTitle, want),
			})
		}
		prevLine, prevTitle = line, want
	}
	seen := map[string]bool{}
	for _, h := range headings {
		if seen[h.title] {
			out = append(out, Violation{
				Rule:    "structure_duplicate_section",
				File:    file,
				Line:    h.line,
				Message: fmt.Sprintf("section %q appears more than once", h.title),
			})
		}
		seen[h.title] = true
	}

	// Citations: every bullet inside a citation section must match.
	if prof.CitationPattern == "" {
		return out
	}
	re := regexp.MustCompile(prof.CitationPattern)
	for _, name := range prof.CitationSections {
		start, ok := at(name)
		if !ok {
			continue
		}
		end := len(strings.Split(string(body), "\n")) + 1
		for _, h := range headings {
			if h.line > start {
				end = h.line
				break
			}
		}
		for i, line := range strings.Split(string(body), "\n") {
			n := i + 1
			if n <= start || n >= end {
				continue
			}
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "- ") {
				continue
			}
			if !re.MatchString(trimmed) {
				out = append(out, Violation{
					Rule:    "structure_missing_citation",
					File:    file,
					Line:    n,
					Message: fmt.Sprintf("claim in %q carries no source citation matching %s", name, prof.CitationPattern),
				})
			}
		}
	}
	return out
}

// validName applies the shared id grammar: a schema name is a document id.
func validName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '.', r == '-', r == '_':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
