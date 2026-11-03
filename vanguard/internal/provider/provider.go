// Package provider resolves the target: GitProvider over an embedded git
// object database (go-git), HashProvider over plain files. The freshness hot
// path works on commit graph + tree metadata only; blob contents are touched
// solely for dirty detection of scoped stat-mismatch candidates.
package provider

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/vanguard-lint/vanguard/internal/hashnorm"
	"github.com/vanguard-lint/vanguard/internal/pathmatch"
	"github.com/vanguard-lint/vanguard/internal/scope"
)

// Provider answers scope-state and worktree questions for one target.
type Provider interface {
	// GitMode reports whether the target has a local .git.
	GitMode() bool
	// HEAD returns the current commit OID, or "" when the repository is
	// unborn.
	HEAD() (string, error)
	// ScopeHash computes the combined scope hash over the canonical globs
	// at HEAD (git mode) or at the current file state (hash mode).
	ScopeHash(globs []string) (string, error)
	// Snapshot returns the path → normalized content hash map of every
	// file matching the globs (hash mode).
	Snapshot(globs []string) (map[string]string, error)
	// Dirty returns the uncommitted churn paths inside the globs (git
	// mode; hash mode has no dirty concept and returns nil).
	Dirty(globs []string) ([]string, error)
	// AnchorReachable reports whether the anchor commit is an ancestor of
	// HEAD — the fast-path condition.
	AnchorReachable(anchor string) (bool, error)
	// RecoveredAnchor walks the first-parent chain from HEAD (bounded) and
	// returns the first commit whose scope hash equals the lock's value.
	RecoveredAnchor(globs []string, want string, maxWalk int) (string, error)
	// Attribution walks from..to (to defaults to HEAD) and returns the
	// changed paths inside the globs plus the introducing commits, oldest
	// first. Merge commits diff against their first parent only.
	Attribution(from, to string, globs []string) (changedPaths, commits []string, err error)
	// ChangedPathsBetween lists every path changed between from..to.
	ChangedPathsBetween(from, to string) ([]string, error)
	// FileStatus reports the worktree status of one path: "clean" or a
	// git status word (git mode; "clean" in hash mode).
	FileStatus(rel string) (string, error)
}

// GitProvider serves a target with a local .git.
type GitProvider struct {
	root     string
	storeDir string // level-1 store directory name, e.g. ".vanguard"
	repo     *git.Repository
}

// OpenGit opens the repository at root (following .git files for linked
// worktrees). It returns an error when root holds no git repository — the
// caller falls back to the HashProvider.
func OpenGit(root, storeDir string) (*GitProvider, error) {
	repo, err := git.PlainOpenWithOptions(root, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return nil, err
	}
	return &GitProvider{root: root, storeDir: storeDir, repo: repo}, nil
}

func (g *GitProvider) GitMode() bool { return true }

func (g *GitProvider) HEAD() (string, error) {
	ref, err := g.repo.Reference(plumbing.HEAD, true)
	if err != nil {
		if err == plumbing.ErrReferenceNotFound {
			return "", nil // unborn repository
		}
		return "", err
	}
	if ref.Hash().IsZero() {
		return "", nil
	}
	return ref.Hash().String(), nil
}

func (g *GitProvider) headTree() (*object.Tree, error) {
	head, err := g.HEAD()
	if err != nil || head == "" {
		return nil, err
	}
	commit, err := g.repo.CommitObject(plumbing.NewHash(head))
	if err != nil {
		return nil, err
	}
	return commit.Tree()
}

// ScopeHash computes the per-glob hashes by strategy and combines them.
func (g *GitProvider) ScopeHash(globs []string) (string, error) {
	tree, err := g.headTree()
	if err != nil {
		return "", err
	}
	pairs := make([]scope.Pair, 0, len(globs))
	for _, glob := range globs {
		var hash string
		switch pathmatch.Classify(glob) {
		case pathmatch.RepositoryWide:
			entries, err := treeEntries(tree, "")
			if err != nil {
				return "", err
			}
			hash = scope.HashRepositoryWide(entries, g.storeDir)
		case pathmatch.Subtree:
			dir := strings.TrimSuffix(glob, "/**")
			oid, err := subtreeOID(tree, dir)
			if err != nil {
				return "", err
			}
			hash = oid
		default:
			entries, err := enumerateTree(tree, glob, g.storeDir)
			if err != nil {
				return "", err
			}
			hash = scope.HashEntries(entries)
		}
		pairs = append(pairs, scope.Pair{Glob: glob, Hash: hash})
	}
	if len(pairs) == 0 {
		return "", nil // no paths watch: nothing to hash
	}
	return scope.Combine(pairs), nil
}

// treeEntries lists one tree's entries (its direct children).
func treeEntries(t *object.Tree, prefix string) ([]scope.Entry, error) {
	var out []scope.Entry
	for i := 0; i < len(t.Entries); i++ {
		e := &t.Entries[i]
		path := e.Name
		if prefix != "" {
			path = prefix + "/" + e.Name
		}
		out = append(out, scope.Entry{Path: path, Mode: fmt.Sprintf("%o", e.Mode), Hash: e.Hash.String()})
	}
	return out, nil
}

// subtreeOID resolves the tree OID at dir, or the empty-tree OID when the
// directory is gone — deterministic, and never equal to any existing subtree.
func subtreeOID(t *object.Tree, dir string) (string, error) {
	sub, err := t.Tree(dir)
	if err != nil {
		if err == object.ErrDirectoryNotFound || err == object.ErrEntryNotFound {
			return "tree:" + emptyTreeSHA, nil
		}
		return "", err
	}
	return "tree:" + sub.Hash.String(), nil
}

const emptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// enumerateTree walks the tree recursively and collects entries matching the
// glob, with the store prefix filtered before hashing.
func enumerateTree(t *object.Tree, glob string, storeDir string) ([]scope.Entry, error) {
	var out []scope.Entry
	if err := walkTreeInto(t, "", glob, storeDir, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func walkTreeInto(t *object.Tree, prefix, glob, storeDir string, out *[]scope.Entry) error {
	if storeDir != "" && (prefix == storeDir || strings.HasPrefix(prefix, storeDir+"/")) {
		return nil
	}
	for i := 0; i < len(t.Entries); i++ {
		e := &t.Entries[i]
		path := e.Name
		if prefix != "" {
			path = prefix + "/" + e.Name
		}
		if e.Mode == filemode.Dir {
			sub, err := t.Tree(e.Name)
			if err != nil {
				return err
			}
			if err := walkTreeInto(sub, path, glob, storeDir, out); err != nil {
				return err
			}
			continue
		}
		if pathmatch.Match(glob, path) {
			*out = append(*out, scope.Entry{Path: path, Mode: fmt.Sprintf("%o", e.Mode), Hash: e.Hash.String()})
		}
	}
	return nil
}

// Dirty returns uncommitted churn paths inside the globs, following git's
// own classification (untracked-not-ignored counts; ignored never appears).
// The status is computed fresh on every call — never cached — so the
// worktree state is current.
func (g *GitProvider) Dirty(globs []string) ([]string, error) {
	status, err := g.worktreeStatus()
	if err != nil {
		return nil, err
	}
	var out []string
	for path, s := range status {
		if s.Staging == git.Unmodified && s.Worktree == git.Unmodified {
			continue
		}
		if g.storeDir != "" && (path == g.storeDir || strings.HasPrefix(path, g.storeDir+"/")) {
			continue // vanguard's own bookkeeping is never churn
		}
		for _, glob := range globs {
			if pathmatch.Match(glob, path) {
				out = append(out, path)
				break
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func (g *GitProvider) worktreeStatus() (git.Status, error) {
	w, err := g.repo.Worktree()
	if err != nil {
		return git.Status{}, err
	}
	return w.Status()
}

// FileStatus reports git's classification of one worktree path.
func (g *GitProvider) FileStatus(rel string) (string, error) {
	status, err := g.worktreeStatus()
	if err != nil {
		return "", err
	}
	s, ok := status[rel]
	if !ok || (s.Staging == git.Unmodified && s.Worktree == git.Unmodified) {
		return "clean", nil
	}
	if s.Worktree == git.Untracked || s.Staging == git.Untracked {
		return "untracked", nil
	}
	return "modified", nil
}

// AnchorReachable walks all parents from HEAD (visited set; the walk is the
// commit-graph hot path — no blobs).
func (g *GitProvider) AnchorReachable(anchor string) (bool, error) {
	head, err := g.HEAD()
	if err != nil || head == "" {
		return false, err
	}
	if anchor == head {
		return true, nil
	}
	seen := map[plumbing.Hash]bool{}
	stack := []plumbing.Hash{plumbing.NewHash(head)}
	for len(stack) > 0 {
		h := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[h] {
			continue
		}
		seen[h] = true
		commit, err := g.repo.CommitObject(h)
		if err != nil {
			return false, nil // history truncated (shallow): not reachable
		}
		for _, p := range commit.ParentHashes {
			if p.String() == anchor {
				return true, nil
			}
			if !seen[p] {
				stack = append(stack, p)
			}
		}
	}
	return false, nil
}

// RecoveredAnchor walks the first-parent chain from HEAD, bounded, and
// returns the first commit whose scope hash equals want.
func (g *GitProvider) RecoveredAnchor(globs []string, want string, maxWalk int) (string, error) {
	head, err := g.HEAD()
	if err != nil || head == "" || want == "" {
		return "", err
	}
	commit, err := g.repo.CommitObject(plumbing.NewHash(head))
	if err != nil {
		return "", err
	}
	for i := 0; i < maxWalk && commit != nil; i++ {
		tree, err := commit.Tree()
		if err != nil {
			return "", err
		}
		hash, err := g.ScopeHashAt(tree, globs)
		if err != nil {
			return "", err
		}
		if hash == want {
			return commit.Hash.String(), nil
		}
		if commit.NumParents() == 0 {
			return "", nil
		}
		commit, err = commit.Parent(0) // first-parent chain only
		if err != nil {
			return "", nil
		}
	}
	return "", nil
}

// ScopeHashAt computes the scope hash at an arbitrary tree (recovery walk).
func (g *GitProvider) ScopeHashAt(tree *object.Tree, globs []string) (string, error) {
	pairs := make([]scope.Pair, 0, len(globs))
	for _, glob := range globs {
		var hash string
		switch pathmatch.Classify(glob) {
		case pathmatch.RepositoryWide:
			entries, err := treeEntries(tree, "")
			if err != nil {
				return "", err
			}
			hash = scope.HashRepositoryWide(entries, g.storeDir)
		case pathmatch.Subtree:
			dir := strings.TrimSuffix(glob, "/**")
			oid, err := subtreeOID(tree, dir)
			if err != nil {
				return "", err
			}
			hash = oid
		default:
			entries, err := enumerateTree(tree, glob, g.storeDir)
			if err != nil {
				return "", err
			}
			hash = scope.HashEntries(entries)
		}
		pairs = append(pairs, scope.Pair{Glob: glob, Hash: hash})
	}
	if len(pairs) == 0 {
		return "", nil
	}
	return scope.Combine(pairs), nil
}

// Attribution walks from..to and collects changed paths and introducing
// commits inside the globs, oldest first.
func (g *GitProvider) Attribution(from, to string, globs []string) ([]string, []string, error) {
	changedSet := map[string]bool{}
	commitSet := map[string]bool{}

	var walk func(h plumbing.Hash) error
	walk = func(h plumbing.Hash) error {
		// stop at `from`: its own state is the baseline, never churn
		if from != "" && h.String() == from {
			return nil
		}
		commit, err := g.repo.CommitObject(h)
		if err != nil {
			return err
		}
		tree, err := commit.Tree()
		if err != nil {
			return err
		}
		var parentTree *object.Tree
		if commit.NumParents() > 0 {
			parent, err := commit.Parent(0) // merges diff against first parent only
			if err != nil {
				return err
			}
			parentTree, err = parent.Tree()
			if err != nil {
				return err
			}
		}
		changes, err := object.DiffTree(parentTree, tree)
		if err != nil {
			return err
		}
		touched := false
		for _, c := range changes {
			path := c.To.Name
			if path == "" {
				path = c.From.Name
			}
			if g.storeDir != "" && (path == g.storeDir || strings.HasPrefix(path, g.storeDir+"/")) {
				continue
			}
			for _, glob := range globs {
				if pathmatch.Match(glob, path) {
					changedSet[path] = true
					touched = true
					break
				}
			}
		}
		if touched {
			commitSet[commit.Hash.String()] = true
		}
		if commit.NumParents() == 0 {
			return nil
		}
		return walk(commit.ParentHashes[0])
	}

	head, err := g.HEAD()
	if err != nil {
		return nil, nil, err
	}
	if to == "" || to == "HEAD" {
		if head == "" {
			return nil, nil, fmt.Errorf("no HEAD to walk")
		}
		to = head
	}
	start, err := g.resolveRef(to)
	if err != nil {
		return nil, nil, err
	}
	if err := walk(start); err != nil {
		return nil, nil, err
	}
	paths := keysOf(changedSet)
	sort.Strings(paths)
	commits := keysOf(commitSet)
	sort.Strings(commits) // oldest→newest refinement happens at the engine layer
	return paths, commits, nil
}

func keysOf(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

// resolveRef resolves a ref (branch, tag, or hex SHA) to a peeled commit
// hash. An unresolvable ref is a usage error, never silent.
func (g *GitProvider) resolveRef(ref string) (plumbing.Hash, error) {
	if ref == "" || ref == "HEAD" {
		head, headErr := g.HEAD()
		if headErr != nil || head == "" {
			return plumbing.ZeroHash, fmt.Errorf("no HEAD in the target repository")
		}
		return plumbing.NewHash(head), nil
	}
	resolved, err := g.repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("ref %q does not resolve in the target repository — pass a resolvable ref", ref)
	}
	commit, commitErr := g.repo.CommitObject(*resolved)
	if commitErr != nil {
		return plumbing.ZeroHash, fmt.Errorf("ref %q does not peel to a commit", ref)
	}
	return commit.Hash, nil
}

// ChangedPathsBetween lists every path changed between from..to (no glob
// filter — the --path reverse lookup uses it).
func (g *GitProvider) ChangedPathsBetween(from, to string) ([]string, error) {
	paths, _, err := g.Attribution(from, to, []string{"**"})
	return paths, err
}

// HashProvider serves a plain directory: content hashing, scope_snapshot,
// no attribution, no dirty concept.
type HashProvider struct {
	root     string
	storeDir string
}

// OpenHash builds a HashProvider over root.
func OpenHash(root, storeDir string) *HashProvider {
	return &HashProvider{root: root, storeDir: storeDir}
}

func (h *HashProvider) GitMode() bool { return false }

func (h *HashProvider) HEAD() (string, error) { return "", nil }

func (h *HashProvider) Snapshot(globs []string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(h.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(h.root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if h.storeDir != "" && rel == h.storeDir {
				return filepath.SkipDir
			}
			return nil
		}
		for _, glob := range globs {
			if pathmatch.Match(glob, rel) {
				sum, err := fileSum(path)
				if err != nil {
					return err
				}
				out[rel] = sum
			}
		}
		return nil
	})
	return out, err
}

func (h *HashProvider) ScopeHash(globs []string) (string, error) {
	snap, err := h.Snapshot(globs)
	if err != nil {
		return "", err
	}
	pairs := make([]scope.Pair, 0, len(globs))
	for _, glob := range globs {
		entries := make([]scope.Entry, 0, len(snap))
		for path, sum := range snap {
			if pathmatch.Match(glob, path) {
				entries = append(entries, scope.Entry{Path: path, Mode: "100644", Hash: sum})
			}
		}
		pairs = append(pairs, scope.Pair{Glob: glob, Hash: scope.HashEntries(entries)})
	}
	if len(pairs) == 0 {
		return "", nil
	}
	return scope.Combine(pairs), nil
}

func (h *HashProvider) Dirty([]string) ([]string, error) { return nil, nil }

func (h *HashProvider) AnchorReachable(string) (bool, error) { return false, nil }

func (h *HashProvider) RecoveredAnchor([]string, string, int) (string, error) { return "", nil }

func (h *HashProvider) Attribution(_, _ string, _ []string) ([]string, []string, error) {
	return nil, nil, fmt.Errorf("attribution is unavailable in hash mode — the target has no git history")
}

func (h *HashProvider) ChangedPathsBetween(_, _ string) ([]string, error) {
	return nil, fmt.Errorf("range attribution is unavailable in hash mode — the target has no git history")
}

func (h *HashProvider) FileStatus(string) (string, error) { return "clean", nil }

// fileSum hashes a file through the streaming normalization pipeline.
func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := hashnorm.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + h.Hex(), nil
}
