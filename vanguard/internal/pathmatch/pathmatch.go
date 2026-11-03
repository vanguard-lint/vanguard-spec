// Package pathmatch implements vanguard's closed glob syntax: brace
// expansion, normalization, classification into hash strategies, matching,
// and the --path decision procedure.
//
// Closed syntax (v1): the tokens are *, ** (whole segments only), ?, character
// classes ([abc], [^abc], ranges), one level of brace alternatives {a,b}, and
// backslash escapes. Negation, fused ** (a**b), nested braces, empty
// alternatives, and a trailing lone backslash are rejected. Matching is
// case-sensitive over slash-separated paths; dotfiles match like any file.
package pathmatch

import (
	"fmt"
	"strings"
)

// Strategy is the scope-hash computation a canonical glob selects.
type Strategy int

const (
	// Enumerated: every glob with a filter — enumerate matching entries and
	// hash the sorted list.
	Enumerated Strategy = iota
	// Subtree: only for filter-free directory forms (dir/**, dir/, dir) —
	// the hash is the git tree OID of that directory's subtree.
	Subtree
	// RepositoryWide: the ** form — a synthetic level-1 list.
	RepositoryWide
)

// SyntaxError reports an invalid glob with the reason.
type SyntaxError struct {
	Glob   string
	Reason string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("invalid glob %q: %s", e.Glob, e.Reason)
}

func fatal(glob, reason string) error {
	return &SyntaxError{Glob: glob, Reason: reason}
}

// Expand expands one level of brace alternatives and returns the canonical,
// normalized globs in declaration order. The pipeline is fixed:
// expand → normalize each expanded glob → validate → collapse **.
func Expand(glob string) ([]string, error) {
	expanded, err := expandBraces(glob)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(expanded))
	for _, g := range expanded {
		c, err := Canonical(g)
		if err != nil {
			return nil, err
		}
		if !contains(out, c) {
			out = append(out, c)
		}
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// expandBraces expands the first brace group and recurses; nested braces and
// empty alternatives are fatal.
func expandBraces(s string) ([]string, error) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '{':
			end, err := closingBrace(s, i)
			if err != nil {
				return nil, err
			}
			alts := strings.Split(s[i+1:end], ",")
			for _, alt := range alts {
				if alt == "" {
					return nil, fatal(s, "empty brace alternative")
				}
			}
			prefix, suffix := s[:i], s[end+1:]
			var out []string
			for _, alt := range alts {
				rest, err := expandBraces(prefix + alt + suffix)
				if err != nil {
					return nil, err
				}
				out = append(out, rest...)
			}
			return out, nil
		}
	}
	return []string{s}, nil
}

func closingBrace(s string, open int) (int, error) {
	depth := 1
	for j := open + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '{':
			return 0, fatal(s, "nested braces")
		case '}':
			depth--
			if depth == 0 {
				return j, nil
			}
		}
	}
	return 0, fatal(s, "unterminated brace")
}

// Canonical normalizes one expanded glob and validates the closed syntax.
// Leading ./ is stripped, a trailing / is stripped, a bare directory form
// (dir/, dir, ., ./) normalizes to its directory scope, consecutive **
// segments collapse.
func Canonical(glob string) (string, error) {
	original := glob
	if glob == "" {
		return "", fatal(original, "empty glob")
	}
	if strings.HasPrefix(glob, "!") {
		return "", fatal(original, "negation is not supported")
	}
	if strings.HasSuffix(glob, "\\") && !strings.HasSuffix(glob, "\\\\") {
		return "", fatal(original, "trailing lone backslash")
	}
	glob = strings.TrimPrefix(glob, "./")
	glob = strings.TrimSuffix(glob, "/")
	if glob == "" || glob == "." {
		return "**", nil
	}
	if err := validate(glob); err != nil {
		return "", err
	}
	segs := strings.Split(glob, "/")
	out := make([]string, 0, len(segs))
	for _, seg := range segs {
		if seg == "**" && len(out) > 0 && out[len(out)-1] == "**" {
			continue // consecutive ** segments collapse
		}
		out = append(out, seg)
	}
	c := strings.Join(out, "/")
	if !hasMeta(c) {
		// A filter-free spelling without trailing /** is a directory scope:
		// dir/ and dir are dir/**.
		c += "/**"
	}
	return c, nil
}

// validate rejects fused **, [! classes, unterminated classes, and stray
// escapes outside the closed syntax.
func validate(glob string) error {
	segs := strings.Split(glob, "/")
	for _, seg := range segs {
		if strings.Contains(seg, "**") && seg != "**" {
			return fatal(glob, "** fused into a larger token")
		}
	}
	inClass := false
	for i := 0; i < len(glob); i++ {
		switch glob[i] {
		case '\\':
			i++ // escaped character is literal
		case '[':
			if inClass {
				return fatal(glob, "nested character class")
			}
			inClass = true
			if i+1 < len(glob) && glob[i+1] == '!' {
				return fatal(glob, "character class negation must use ^")
			}
		case ']':
			inClass = false
		}
	}
	if inClass {
		return fatal(glob, "unterminated character class")
	}
	return nil
}

// Classify returns the hash strategy for a canonical glob.
func Classify(canonical string) Strategy {
	if canonical == "**" {
		return RepositoryWide
	}
	segs := strings.Split(canonical, "/")
	if segs[len(segs)-1] == "**" {
		filterFree := true
		for _, seg := range segs[:len(segs)-1] {
			if hasMeta(seg) {
				filterFree = false
				break
			}
		}
		if filterFree {
			return Subtree
		}
	}
	return Enumerated
}

func hasMeta(s string) bool {
	return strings.ContainsAny(s, "*?[\\")
}

// Match reports whether path matches the canonical glob.
func Match(canonical, path string) bool {
	return matchSegs(strings.Split(canonical, "/"), strings.Split(path, "/"))
}

func matchSegs(gi, pj []string) bool {
	if len(gi) == 0 {
		return len(pj) == 0
	}
	if gi[0] == "**" {
		for k := 0; k <= len(pj); k++ {
			if matchSegs(gi[1:], pj[k:]) {
				return true
			}
		}
		return false
	}
	if len(pj) == 0 {
		return false
	}
	if !matchSegment(gi[0], pj[0]) {
		return false
	}
	return matchSegs(gi[1:], pj[1:])
}

// matchSegment matches one path segment against one glob segment, supporting
// *, ?, classes, and escapes.
func matchSegment(pattern, s string) bool {
	if pattern == "" {
		return s == ""
	}
	switch pattern[0] {
	case '*':
		for k := 0; k <= len(s); k++ {
			if matchSegment(pattern[1:], s[k:]) {
				return true
			}
		}
		return false
	case '?':
		if s == "" {
			return false
		}
		return matchSegment(pattern[1:], s[1:])
	case '[':
		end, ok := classEnd(pattern)
		if !ok {
			return false // validation rejects this; never matches
		}
		if s == "" || !classMatches(pattern[:end+1], s[0]) {
			return false
		}
		return matchSegment(pattern[end+1:], s[1:])
	case '\\':
		if len(pattern) < 2 || s == "" || s[0] != pattern[1] {
			return false
		}
		return matchSegment(pattern[2:], s[1:])
	default:
		if s == "" || s[0] != pattern[0] {
			return false
		}
		return matchSegment(pattern[1:], s[1:])
	}
}

func classEnd(pattern string) (int, bool) {
	for j := 1; j < len(pattern); j++ {
		switch pattern[j] {
		case '\\':
			j++
		case ']':
			if j == 1 {
				return 0, false // "[]" is not a valid class
			}
			return j, true
		}
	}
	return 0, false
}

func classMatches(class string, c byte) bool {
	// class is "[...]" including both brackets.
	body := class[1 : len(class)-1]
	negate := false
	if strings.HasPrefix(body, "^") {
		negate = true
		body = body[1:]
	}
	matched := false
	for j := 0; j < len(body); j++ {
		if body[j] == '\\' && j+1 < len(body) {
			j++
			if body[j] == c {
				matched = true
			}
			continue
		}
		if j+2 < len(body) && body[j+1] == '-' {
			if body[j] <= c && c <= body[j+2] {
				matched = true
			}
			j += 2
			continue
		}
		if body[j] == c {
			matched = true
		}
	}
	return matched != negate
}

// CouldClaim is the --path decision procedure: whether some file under p
// (p included when it is a file) could match the canonical glob. For a file
// argument it is the direct glob match; for a directory it is segment
// reasoning over the glob's shape — no filesystem access.
func CouldClaim(canonical, p string, pIsDir bool) bool {
	if !pIsDir {
		return Match(canonical, p)
	}
	return couldClaimDir(strings.Split(canonical, "/"), strings.Split(p, "/"))
}

func couldClaimDir(gi, pj []string) bool {
	if len(gi) == 0 {
		return false // G exhausted while P continues: no recursion
	}
	if len(pj) == 0 {
		return true // P consumed: G can still name a deeper file
	}
	if gi[0] == "**" {
		return true // ** absorbs any run of P's segments
	}
	if !matchSegment(gi[0], pj[0]) {
		return false
	}
	return couldClaimDir(gi[1:], pj[1:])
}
