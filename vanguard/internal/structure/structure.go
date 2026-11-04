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
	MaxDepth         int      `json:"max_depth"`         // deepest allowed heading level; 0 = unchecked; validation cap 32
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
// profile. The contract may sit at the top level or inside one combinator
// branch (anyOf/oneOf/allOf, one level deep — the shape package contracts
// use); several identical copies are one contract, and several distinct
// copies are an ambiguous schema that fails closed — guessing a merge would
// silently weaken whichever constraint the author meant. A schema without
// an x-vanguard block binds existence only — the declaration is checked,
// the body is not. An invalid citation pattern is a corrupt schema, fatal
// at the call site.
func Load(schemasDir, name string) (*Profile, error) {
	if !validName(name) {
		return nil, fmt.Errorf("schema name %q is not a valid id", name)
	}
	raw, err := os.ReadFile(filepath.Join(schemasDir, name+".schema.json"))
	if err != nil {
		return nil, fmt.Errorf("schema %q is declared but not found in %s: %w", name, schemasDir, err)
	}
	var doc struct {
		XVanguard *Profile          `json:"x-vanguard"`
		AnyOf     []json.RawMessage `json:"anyOf"`
		OneOf     []json.RawMessage `json:"oneOf"`
		AllOf     []json.RawMessage `json:"allOf"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("schema %q is not valid JSON: %w", name, err)
	}
	profiles := []*Profile{}
	if doc.XVanguard != nil {
		profiles = append(profiles, doc.XVanguard)
	}
	branches := make([]json.RawMessage, 0, len(doc.AnyOf)+len(doc.OneOf)+len(doc.AllOf))
	branches = append(branches, doc.AnyOf...)
	branches = append(branches, doc.OneOf...)
	branches = append(branches, doc.AllOf...)
	for _, branch := range branches {
		var b struct {
			XVanguard *Profile `json:"x-vanguard"`
		}
		if err := json.Unmarshal(branch, &b); err != nil {
			return nil, fmt.Errorf("schema %q has an invalid combinator branch: %w", name, err)
		}
		if b.XVanguard != nil {
			profiles = append(profiles, b.XVanguard)
		}
	}
	if len(profiles) == 0 {
		return &Profile{}, nil
	}
	canonical, err := json.Marshal(profiles[0])
	if err != nil {
		return nil, fmt.Errorf("schema %q: %w", name, err)
	}
	for _, p := range profiles[1:] {
		other, err := json.Marshal(p)
		if err != nil {
			return nil, fmt.Errorf("schema %q: %w", name, err)
		}
		if string(other) != string(canonical) {
			return nil, fmt.Errorf("schema %q declares %d distinct x-vanguard contracts across the top level and combinator branches — ambiguous, refusing to guess", name, len(profiles))
		}
	}
	p := profiles[0]
	if p.CitationPattern != "" {
		if _, err := regexp.Compile(p.CitationPattern); err != nil {
			return nil, fmt.Errorf("schema %q has an invalid citation_pattern: %w", name, err)
		}
	}
	if p.MaxDepth < 0 {
		return nil, fmt.Errorf("schema %q declares a negative max_depth", name)
	}
	if p.MaxDepth > 32 {
		return nil, fmt.Errorf("schema %q declares max_depth %d — the validation cap is 32", name, p.MaxDepth)
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

	// One scan marks fenced regions and collects the headings. A ``` or
	// ~~~ line toggles a fence; fence contents are quoted material — a
	// heading or bullet inside a fence is neither the document's structure
	// nor one of its claims, so the scanner must not see it (quoted
	// examples would otherwise count as sections, and quoted sections as
	// duplicates).
	lines := strings.Split(string(body), "\n")
	fenced := make([]bool, len(lines))
	inFence := false
	marker := ""
	type heading struct {
		title string
		line  int
	}
	var headings []heading
	first := map[string]int{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			m := trimmed[:3]
			if !inFence {
				inFence, marker = true, m
			} else if strings.HasPrefix(trimmed, marker) {
				inFence = false
			}
			continue
		}
		if inFence {
			fenced[i] = true
			continue
		}
		if strings.HasPrefix(line, "## ") {
			title := strings.TrimSpace(line[3:])
			headings = append(headings, heading{title: title, line: i + 1})
			if _, ok := first[title]; !ok {
				first[title] = i + 1
			}
		}
	}

	at := func(title string) (int, bool) {
		line, ok := first[title]
		return line, ok
	}

	// Heading depth: a declared max_depth bounds the deepest ATX heading
	// level; exceeding it is a finding (the 32-entry validation cap lives
	// in Load — config validation, not per-document work). Fence contents
	// are invisible here too: a commented heading inside a shell block is
	// not the document's structure.
	if prof.MaxDepth > 0 {
		for i, line := range lines {
			if fenced[i] || !strings.HasPrefix(line, "#") {
				continue
			}
			level := len(line) - len(strings.TrimLeft(line, "#"))
			rest := line[level:]
			if len(rest) > 0 && rest[0] != ' ' {
				continue // not an ATX heading (shebang, tag, ...)
			}
			if level > prof.MaxDepth {
				out = append(out, Violation{
					Rule:    "structure_max_depth",
					File:    file,
					Line:    i + 1,
					Message: fmt.Sprintf("heading level %d exceeds the declared max_depth %d", level, prof.MaxDepth),
				})
			}
		}
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

	// Citations: every bullet inside a citation section must match. Fence
	// contents are quoted material, not claims of the document.
	if prof.CitationPattern == "" {
		return out
	}
	re := regexp.MustCompile(prof.CitationPattern)
	for _, name := range prof.CitationSections {
		start, ok := at(name)
		if !ok {
			continue
		}
		end := len(lines) + 1
		for _, h := range headings {
			if h.line > start {
				end = h.line
				break
			}
		}
		// A bullet spans its continuation lines: markdown wraps freely,
		// and the citation may sit on any line of the item. The item ends
		// at a blank line, a new bullet, a heading, or a fence. Checking
		// only the first line would phantom-fail every wrapped claim —
		// the same quoted-material blindness the fence rule fixes.
		for i := start; i < len(lines); i++ {
			n := i + 1
			if n >= end {
				break
			}
			if fenced[i] {
				continue
			}
			trimmed := strings.TrimSpace(lines[i])
			if !strings.HasPrefix(trimmed, "- ") {
				continue
			}
			text := trimmed
			for j := i + 1; j+1 < end && j < len(lines); j++ {
				if fenced[j] {
					break
				}
				t := strings.TrimSpace(lines[j])
				if t == "" || strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "#") {
					break
				}
				text += " " + t
				i = j
			}
			if !re.MatchString(text) {
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
