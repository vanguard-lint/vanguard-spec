// Package docmodel extracts the engine-owned document model from discovered
// files and walks the document root in the fixed discovery order.
//
// Every discovered document carries exactly these engine fields; everything
// else is user-defined and opaque: id, schema, watches (paths, review_after),
// bindings. Frontmatter parsing is strict: anchors, aliases, merge keys, and
// duplicate keys are fatal; a UTF-8 BOM is tolerated and stripped.
package docmodel

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vanguard-lint/vanguard/internal/duration"
	"github.com/vanguard-lint/vanguard/internal/pathmatch"
	"gopkg.in/yaml.v3"
)

// FrontmatterCap is the structure-layer resource cap: a larger frontmatter is
// a fatal with a split-the-document hint.
const FrontmatterCap = 256 * 1024

// MaxIDLen is the closed id grammar bound.
const MaxIDLen = 128

// Watches are the staleness sources declared in frontmatter.
type Watches struct {
	Paths       []string `yaml:"paths"` // raw globs; canonicalized during discovery
	ReviewAfter string   `yaml:"review_after"`
}

// Doc is one discovered document.
type Doc struct {
	ID       string
	Schema   string
	Watches  Watches
	Bindings []string
	RelPath  string // relative to docs/, slash-separated
	Format   string // markdown | json | yaml | dsl
	Raw      []byte // full file bytes, BOM stripped
	// PathGlobs are the canonical expanded watch globs.
	PathGlobs []string
	// ReviewAfter is the parsed review_after, zero when undeclared.
	ReviewAfterSeconds float64
}

// Warning is an advisory discovery note (file + reason — G8).
type Warning struct {
	File   string
	Reason string
}

type frontmatter struct {
	ID       string   `yaml:"id"`
	Schema   string   `yaml:"schema"`
	Watches  *Watches `yaml:"watches"`
	Bindings []string `yaml:"bindings"`
}

// Discover walks docsRoot in lexicographic path order, parses candidates, and
// returns the documents. It never follows symlinks and skips unknown formats
// with advisory warnings. A duplicate id or a broken frontmatter is fatal.
func Discover(docsRoot string, include []string, formats map[string]string) ([]Doc, []Warning, error) {
	globs := make([]string, 0, len(include))
	for _, g := range include {
		expanded, err := pathmatch.Expand(g)
		if err != nil {
			return nil, nil, fmt.Errorf("discovery glob %q: %w", g, err)
		}
		globs = append(globs, expanded...)
	}

	var docs []Doc
	var warnings []Warning
	seen := map[string]string{} // id -> rel path

	// WalkDir surfaces a missing root as a raw lstat error; the store owner
	// needs the recovery path instead.
	if info, statErr := os.Stat(docsRoot); statErr != nil || !info.IsDir() {
		return nil, nil, fmt.Errorf(
			"document root %s does not exist - no documents to discover; restore the store documents (e.g. git checkout -- %s) or run vanguard init",
			docsRoot, docsRoot)
	}

	err := filepath.WalkDir(docsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(docsRoot, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			warnings = append(warnings, Warning{File: rel,
				Reason: "symlink — not a document, skipped"})
			return nil
		}
		matched := false
		for _, g := range globs {
			if pathmatch.Match(g, rel) {
				matched = true
				break
			}
		}
		if !matched {
			return nil
		}
		format, ok := formats[strings.TrimPrefix(filepath.Ext(rel), ".")]
		if !ok {
			warnings = append(warnings, Warning{File: rel,
				Reason: "extension maps to no registered format handler — skipped"})
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", rel, readErr)
		}
		raw = bytes.TrimPrefix(raw, []byte("\xEF\xBB\xBF"))
		doc, parseErr := parseDocument(rel, format, raw)
		if parseErr != nil {
			return parseErr
		}
		if prev, dup := seen[doc.ID]; dup {
			return fmt.Errorf("duplicate document id %q (%s and %s)", doc.ID, prev, rel)
		}
		seen[doc.ID] = rel
		docs = append(docs, *doc)
		return nil
	})
	if err != nil {
		return nil, warnings, err
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].RelPath < docs[j].RelPath })
	return docs, warnings, nil
}

// parseDocument extracts the engine fields per format.
func parseDocument(rel, format string, raw []byte) (*Doc, error) {
	meta, body, err := extractEngineFields(format, raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	if meta.ID == "" {
		return nil, fmt.Errorf("%s: engine field id is required — the document model carries id, schema, watches, bindings", rel)
	}
	if err := validateID(meta.ID); err != nil {
		return nil, fmt.Errorf("%s: id %q: %w", rel, meta.ID, err)
	}
	doc := &Doc{ID: meta.ID, Schema: meta.Schema, Watches: Watches{}, Bindings: meta.Bindings, RelPath: rel, Format: format, Raw: raw}
	_ = body
	if meta.Watches != nil {
		doc.Watches = *meta.Watches
		for _, g := range doc.Watches.Paths {
			expanded, err := pathmatch.Expand(g)
			if err != nil {
				return nil, fmt.Errorf("%s: watch glob %q: %w", rel, g, err)
			}
			doc.PathGlobs = append(doc.PathGlobs, expanded...)
		}
		if doc.Watches.ReviewAfter != "" {
			d, err := duration.Parse(doc.Watches.ReviewAfter)
			if err != nil {
				return nil, fmt.Errorf("%s: review_after: %w", rel, err)
			}
			doc.ReviewAfterSeconds = d.Seconds()
		}
	}
	return doc, nil
}

// extractEngineFields parses the format's metadata location.
func extractEngineFields(format string, raw []byte) (*frontmatter, []byte, error) {
	switch format {
	case "markdown":
		return parseMarkdownFrontmatter(raw)
	case "json", "yaml":
		return parseTopLevelFields(raw)
	case "dsl":
		return parseDSLBlock(raw)
	default:
		return nil, nil, fmt.Errorf("no handler for format %q", format)
	}
}

func parseMarkdownFrontmatter(raw []byte) (*frontmatter, []byte, error) {
	if !bytes.HasPrefix(raw, []byte("---\n")) && !bytes.HasPrefix(raw, []byte("---\r\n")) {
		return &frontmatter{}, raw, fmt.Errorf("markdown documents must start with a YAML frontmatter block (---)")
	}
	rest := raw[4:]
	end := -1
	for i := 0; i+3 < len(rest); i++ {
		if rest[i] == '\n' && (rest[i+1] == '-' || rest[i+1] == '.') &&
			i+4 <= len(rest) &&
			(bytes.HasPrefix(rest[i+1:], []byte("---")) || bytes.HasPrefix(rest[i+1:], []byte("..."))) {
			end = i
			break
		}
	}
	if end < 0 {
		return &frontmatter{}, raw, fmt.Errorf("frontmatter block is never closed")
	}
	block := rest[:end]
	if len(block) > FrontmatterCap {
		return &frontmatter{}, raw, fmt.Errorf("frontmatter exceeds %d KiB — split the document", FrontmatterCap/1024)
	}
	meta, err := decodeStrict(block)
	if err != nil {
		return &frontmatter{}, raw, err
	}
	return meta, raw, nil
}

func parseTopLevelFields(raw []byte) (*frontmatter, []byte, error) {
	if len(raw) > FrontmatterCap {
		return &frontmatter{}, raw, fmt.Errorf("document exceeds %d KiB — split the document", FrontmatterCap/1024)
	}
	// Whole-document formats: the engine fields are a subset of the user's
	// document. The engine rejects YAML tricks across the full tree, then
	// decodes ONLY its four engine keys strictly. Everything else —
	// schema_version, source, nodes, ... — is user-owned and opaque (G5);
	// an unknown user field is content, never an error.
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return &frontmatter{}, raw, fmt.Errorf("document is not valid YAML: %w", err)
	}
	if err := rejectYAMLTricks(&root); err != nil {
		return &frontmatter{}, raw, err
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return &frontmatter{}, raw, fmt.Errorf("document must be a mapping at the top level")
	}
	m := root.Content[0]
	if m.Kind != yaml.MappingNode {
		return &frontmatter{}, raw, fmt.Errorf("document must be a mapping at the top level")
	}
	engine := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	seen := map[string]bool{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		key, val := m.Content[i], m.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			return &frontmatter{}, raw, fmt.Errorf("frontmatter: mapping key at line %d is not a scalar", key.Line)
		}
		if seen[key.Value] {
			return &frontmatter{}, raw, fmt.Errorf("frontmatter: duplicate key %q at line %d", key.Value, key.Line)
		}
		seen[key.Value] = true
		switch key.Value {
		case "id", "schema", "watches", "bindings":
			engine.Content = append(engine.Content, key, val)
		}
	}
	out, err := yaml.Marshal(engine)
	if err != nil {
		return &frontmatter{}, raw, fmt.Errorf("frontmatter: %w", err)
	}
	meta, err := decodeStrict(out)
	if err != nil {
		return &frontmatter{}, raw, err
	}
	return meta, raw, nil
}

// parseDSLBlock extracts engine fields from the leading comment block.
func parseDSLBlock(raw []byte) (*frontmatter, []byte, error) {
	lines := strings.Split(string(raw), "\n")
	meta := &frontmatter{}
	found := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#!") {
			continue
		}
		var body string
		if strings.HasPrefix(trimmed, "//") {
			body = strings.TrimSpace(trimmed[2:])
		} else if strings.HasPrefix(trimmed, "#") {
			body = strings.TrimSpace(trimmed[1:])
		} else {
			break // the leading comment block ends here
		}
		k, v, ok := strings.Cut(body, ":")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		if !isEngineKey(key) || !isSimpleValue(val) {
			continue
		}
		found++
		assignField(meta, key, val)
	}
	if found == 0 {
		return meta, raw, fmt.Errorf("engine fields must appear in the leading comment block (// or # lines with key: value)")
	}
	return meta, raw, nil
}

func isEngineKey(k string) bool {
	switch k {
	case "id", "schema", "watches", "bindings":
		return true
	}
	return false
}

func isSimpleValue(v string) bool {
	return v != "" && !strings.ContainsAny(v, "{}[]")
}

func assignField(meta *frontmatter, key, val string) {
	switch key {
	case "id":
		meta.ID = val
	case "schema":
		meta.Schema = val
	case "bindings":
		meta.Bindings = strings.Split(val, ",")
		for i := range meta.Bindings {
			meta.Bindings[i] = strings.TrimSpace(meta.Bindings[i])
		}
	}
}

// decodeStrict decodes YAML into the engine struct: duplicate keys, anchors,
// aliases, and merge keys are fatal.
func decodeStrict(data []byte) (*frontmatter, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("frontmatter is not valid YAML: %w", err)
	}
	if err := rejectYAMLTricks(&root); err != nil {
		return nil, err
	}
	var meta frontmatter
	strict := yaml.NewDecoder(bytes.NewReader(data))
	strict.KnownFields(true)
	if err := strict.Decode(&meta); err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	return &meta, nil
}

func rejectYAMLTricks(n *yaml.Node) error {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.AliasNode {
		return fmt.Errorf("frontmatter: YAML aliases are rejected — declare the value inline")
	}
	if n.Anchor != "" {
		return fmt.Errorf("frontmatter: YAML anchors are rejected — declare the value inline")
	}
	for _, c := range n.Content {
		if err := rejectYAMLTricks(c); err != nil {
			return err
		}
	}
	return nil
}

func validateID(id string) error {
	if id == "" || len(id) > MaxIDLen {
		return fmt.Errorf("must match [a-z0-9][a-z0-9._-]*, max %d chars, ASCII only", MaxIDLen)
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		ok := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
			(i > 0 && (c == '.' || c == '_' || c == '-'))
		if !ok {
			return fmt.Errorf("must match [a-z0-9][a-z0-9._-]*, max %d chars, ASCII only", MaxIDLen)
		}
	}
	return nil
}
