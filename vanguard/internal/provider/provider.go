// Package provider resolves the target: GitProvider over an embedded git
// object database (go-git), HashProvider over plain files. The freshness hot
// path works on commit graph + tree metadata only; blob contents are touched
// solely for dirty detection of scoped stat-mismatch candidates.
package provider

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

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
	// Dirty returns the uncommitted churn paths inside the globs plus
	// advisory warnings (git mode; hash mode has no dirty concept and
	// returns nil, nil). Candidates come from the worktree status; each is
	// confirmed by comparing worktree bytes against the HEAD blob over the
	// Hash normalization pipeline.
	Dirty(globs []string) (dirty, warnings []string, err error)
	// AnchorReachable reports whether the anchor commit is an ancestor of
	// HEAD — the fast-path condition.
	AnchorReachable(anchor string) (bool, error)
	// RecoveredAnchor walks the first-parent chain from HEAD (bounded) and
	// returns the first commit whose scope hash equals the lock's value.
	RecoveredAnchor(globs []string, want string, maxWalk int) (string, error)
	// Attribution walks from..to (to defaults to HEAD) and returns the
	// changed paths inside the globs plus the introducing commits oldest →
	// newest (committer date; equal-position ties break by OID). Merge
	// commits diff against their first parent only.
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

	// Within-run memoization (canon: "Memoization within a run"): one
	// snapshot per target per run. HEAD, the HEAD tree, the worktree
	// status, and attribution baselines are pure functions of state that
	// cannot change mid-run by design; capturing them once keeps every
	// document in a run on the same snapshot instead of a drift blend.
	// Nothing here is ever persisted: observe stays the only writer, and
	// worktree-dependent state is never reused across runs.
	headOnce   sync.Once
	headHash   string
	headErr    error
	treeOnce   sync.Once
	tree       *object.Tree
	treeErr    error
	statusOnce sync.Once
	status     git.Status
	statusErr  error
	// baselines memoizes Attribution's reachable set per anchor string.
	// The engine drives providers from a single goroutine, so no lock.
	baselines map[string]map[plumbing.Hash]bool
	// reach memoizes AnchorReachable per anchor: a pure function of
	// (anchor, frozen HEAD), decided at most once per anchor per run.
	reach map[string]bool
	// changes memoizes each commit's first-parent changed-path set,
	// store-filtered, before any document's glob matching: a pure function
	// of the commit pair, independent of the globs.
	changes map[plumbing.Hash]*commitChange
}

// commitChange is one commit's first-parent diff, store-filtered and
// pre-glob (canon: "History questions are cached per answer, not per
// document").
type commitChange struct {
	paths  []string      // changed paths outside the store dir
	when   time.Time     // committer time (attribution sort key)
	parent plumbing.Hash // first parent; zero at the root
}

// OpenGit opens the repository rooted exactly at root (no upward detection:
// a parent's unrelated repository must never serve as the target). It
// returns an error when root holds no git repository — the caller falls
// back to the HashProvider.
func OpenGit(root, storeDir string) (*GitProvider, error) {
	repo, err := git.PlainOpen(root)
	if err != nil {
		return nil, err
	}
	return &GitProvider{root: root, storeDir: storeDir, repo: repo,
		baselines: map[string]map[plumbing.Hash]bool{},
		reach:     map[string]bool{},
		changes:   map[plumbing.Hash]*commitChange{}}, nil
}

func (g *GitProvider) GitMode() bool { return true }

func (g *GitProvider) HEAD() (string, error) {
	g.headOnce.Do(func() {
		ref, err := g.repo.Reference(plumbing.HEAD, true)
		if err != nil {
			if err == plumbing.ErrReferenceNotFound {
				return // unborn repository: "", nil
			}
			g.headErr = err
			return
		}
		if ref.Hash().IsZero() {
			return
		}
		g.headHash = ref.Hash().String()
	})
	return g.headHash, g.headErr
}

func (g *GitProvider) headTree() (*object.Tree, error) {
	g.treeOnce.Do(func() {
		head, err := g.HEAD()
		if err != nil || head == "" {
			g.treeErr = err
			return
		}
		commit, err := g.repo.CommitObject(plumbing.NewHash(head))
		if err != nil {
			g.treeErr = err
			return
		}
		g.tree, g.treeErr = commit.Tree()
	})
	return g.tree, g.treeErr
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
	if err == nil {
		return "tree:" + sub.Hash.String(), nil
	}
	if err == object.ErrDirectoryNotFound || err == object.ErrEntryNotFound {
		// The path may be a single FILE: a filter-free bare name
		// canonicalizes to path/**, and hashing it as a missing subtree
		// pinned the scope to the empty tree — a watched file's content
		// changes never tripped the net-state rule (silently fresh). Hash
		// the blob instead so a file watch is content-sensitive.
		if entry, eerr := t.FindEntry(dir); eerr == nil && entry.Mode != filemode.Dir {
			return "blob:" + entry.Hash.String(), nil
		}
		return "tree:" + emptyTreeSHA, nil
	}
	return "", err
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
//
// Two-phase, per the design: go-git's worktree status supplies the
// CANDIDATE set only; every candidate is then confirmed by comparing the
// worktree bytes against the HEAD blob over the Hash normalization
// pipeline. go-git's raw comparison hashes worktree bytes against the
// index and reports phantom churn when core.autocrlf renormalizes line
// endings — the normalized confirm folds CRLF to LF and filters those
// phantoms without consulting the git CLI. Untracked files have no HEAD
// blob and are dirty outright. A blobless clone that lacks the blob makes
// the comparison impossible — never silently: the file is reported dirty
// and a warning carries the hint.
func (g *GitProvider) Dirty(globs []string) ([]string, []string, error) {
	status, err := g.goGitStatus()
	if err != nil {
		return nil, nil, err
	}
	var out, warnings []string
	for path, s := range status {
		if s.Staging == git.Unmodified && s.Worktree == git.Unmodified {
			continue
		}
		if g.storeDir != "" && (path == g.storeDir || strings.HasPrefix(path, g.storeDir+"/")) {
			continue // vanguard's own bookkeeping is never churn
		}
		claimed := false
		for _, glob := range globs {
			if pathmatch.Match(glob, path) {
				claimed = true
				break
			}
		}
		if !claimed {
			continue
		}
		if s.Worktree == git.Untracked || s.Staging == git.Untracked {
			out = append(out, path)
			continue
		}
		equal, missing, err := g.worktreeMatchesHead(path)
		if err != nil {
			return nil, nil, err
		}
		if missing {
			out = append(out, path)
			warnings = append(warnings, fmt.Sprintf(
				"this clone does not hold the code blob for %s — confirm in a full checkout", path))
			continue
		}
		if !equal {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	sort.Strings(warnings)
	return out, warnings, nil
}

// worktreeMatchesHead compares the worktree bytes of one path against the
// HEAD blob over the Hash normalization pipeline. missing=true reports a
// blob the clone does not hold (blobless) — the loud case, never silent.
func (g *GitProvider) worktreeMatchesHead(path string) (equal, missing bool, err error) {
	blob, ok, err := g.headBlob(path)
	if err != nil {
		return false, false, err
	}
	if !ok {
		return false, true, nil
	}
	local, err := os.ReadFile(filepath.Join(g.root, filepath.FromSlash(path)))
	if err != nil {
		return false, false, err
	}
	return hashnorm.Sum(local) == hashnorm.Sum(blob), false, nil
}

// headBlob reads the HEAD blob content of one path. ok=false when the path
// is absent at HEAD, or when the clone does not hold the blob (blobless).
func (g *GitProvider) headBlob(path string) ([]byte, bool, error) {
	tree, err := g.headTree()
	if err != nil {
		return nil, false, err
	}
	t := tree
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if i == len(parts)-1 {
			for j := range t.Entries {
				e := &t.Entries[j]
				if e.Name != part || e.Mode == filemode.Dir {
					continue
				}
				blob, err := g.repo.BlobObject(e.Hash)
				if err != nil {
					if errors.Is(err, plumbing.ErrObjectNotFound) {
						return nil, false, nil
					}
					return nil, false, err
				}
				r, err := blob.Reader()
				if err != nil {
					return nil, false, err
				}
				defer func() { _ = r.Close() }()
				data, readErr := io.ReadAll(r)
				if readErr != nil {
					return nil, false, readErr
				}
				return data, true, nil
			}
			return nil, false, nil
		}
		t, err = t.Tree(part)
		if err != nil {
			return nil, false, nil // path absent at HEAD
		}
	}
	return nil, false, nil
}

// goGitStatus classifies every worktree path via go-git. Used as the
// candidate generator only — its raw byte-vs-index comparison reports
// phantom churn under core.autocrlf, which Dirty's normalized confirm
// filters.
func (g *GitProvider) goGitStatus() (git.Status, error) {
	// The full worktree scan is the most expensive primitive the engine
	// has; captured once per run and shared by every document on this
	// target (canon: "Memoization within a run").
	g.statusOnce.Do(func() {
		w, err := g.repo.Worktree()
		if err != nil {
			g.statusErr = err
			return
		}
		g.status, g.statusErr = w.Status()
	})
	return g.status, g.statusErr
}

// FileStatus reports the worktree classification of one path: "clean" when
// the worktree bytes match the HEAD blob over the Hash normalization
// pipeline, "untracked" when HEAD holds no such path, and "modified" when
// the normalized contents differ. The normalized compare is immune to
// core.autocrlf phantom churn.
func (g *GitProvider) FileStatus(rel string) (string, error) {
	equal, missing, err := g.worktreeMatchesHead(rel)
	if err != nil {
		return "", err
	}
	if missing {
		return "untracked", nil
	}
	if equal {
		return "clean", nil
	}
	return "modified", nil
}

// HeadState reports HEAD detachment and whether a merge or rebase is in
// progress (the .git state files exist). Detached: HEAD resolves directly
// to a commit instead of a branch ref. A linked worktree's `.git` file
// (gitdir pointer) is followed for the marker checks.
func (g *GitProvider) HeadState() (detached, midMerge bool, err error) {
	ref, headErr := g.repo.Head()
	if headErr != nil {
		return false, false, headErr
	}
	detached = ref.Name() == plumbing.HEAD
	dotgit := filepath.Join(g.root, ".git")
	if fi, statErr := os.Stat(dotgit); statErr == nil && !fi.IsDir() {
		if b, readErr := os.ReadFile(dotgit); readErr == nil {
			line := strings.TrimSpace(string(b))
			if p, ok := strings.CutPrefix(line, "gitdir: "); ok {
				if filepath.IsAbs(p) {
					dotgit = p
				} else {
					dotgit = filepath.Join(g.root, p)
				}
			}
		}
	}
	for _, marker := range []string{"MERGE_HEAD", "rebase-merge", "rebase-apply"} {
		if _, statErr := os.Stat(filepath.Join(dotgit, marker)); statErr == nil {
			midMerge = true
		}
	}
	return detached, midMerge, nil
}

// AnchorReachable walks all parents from HEAD (visited set; the walk is the
// commit-graph hot path — no blobs). The answer is a pure function of
// (anchor, frozen HEAD), so it is decided at most once per anchor per run.
func (g *GitProvider) AnchorReachable(anchor string) (bool, error) {
	if cached, ok := g.reach[anchor]; ok {
		return cached, nil
	}
	reachable, err := g.anchorReachable(anchor)
	if err != nil {
		return false, err
	}
	g.reach[anchor] = reachable
	return reachable, nil
}

func (g *GitProvider) anchorReachable(anchor string) (bool, error) {
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
// commits inside the globs. Commits are sorted oldest → newest
// (chronological reading order; equal-position ties break by OID
// lexicographically). Merge commits diff against their first parent only.
func (g *GitProvider) Attribution(from, to string, globs []string) ([]string, []string, error) {
	// Baseline: every commit reachable from `from` is the anchor's side of
	// history. The walk stops at the first commit in this set, so the
	// fast-path attribution matches the anchor..HEAD revwalk semantics even
	// when the anchor reached HEAD only through a merge's second parent
	// (a regular merge, not a squash): the first-parent descent from HEAD
	// stops at the branch point instead of walking to the root.
	baseline := map[plumbing.Hash]bool{}
	if from != "" {
		if cached, ok := g.baselines[from]; ok {
			baseline = cached
		} else {
			fromHash, resolveErr := g.resolveRef(from)
			if resolveErr != nil {
				return nil, nil, resolveErr
			}
			stack := []plumbing.Hash{fromHash}
			for len(stack) > 0 {
				h := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if baseline[h] {
					continue
				}
				baseline[h] = true
				c, err := g.repo.CommitObject(h)
				if err != nil {
					continue // truncated history (shallow): stop expanding
				}
				stack = append(stack, c.ParentHashes...)
			}
			g.baselines[from] = baseline
		}
	}
	changedSet := map[string]bool{}
	commitSet := map[string]bool{}
	commitTimes := map[string]time.Time{}

	var walk func(h plumbing.Hash) error
	walk = func(h plumbing.Hash) error {
		// stop at the anchor's side of history: its own state is the
		// baseline, never churn
		if baseline[h] {
			return nil
		}
		c, err := g.commitChange(h)
		if err != nil {
			return err
		}
		touched := false
		for _, path := range c.paths {
			for _, glob := range globs {
				if pathmatch.Match(glob, path) {
					changedSet[path] = true
					touched = true
					break
				}
			}
		}
		if touched {
			commitSet[h.String()] = true
			commitTimes[h.String()] = c.when
		}
		if c.parent.IsZero() {
			return nil
		}
		return walk(c.parent)
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
	// oldest → newest (chronological reading order); equal-position ties
	// break by OID lexicographically.
	sort.Slice(commits, func(i, j int) bool {
		ti, tj := commitTimes[commits[i]], commitTimes[commits[j]]
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return commits[i] < commits[j]
	})
	return paths, commits, nil
}

// commitChange returns the commit's first-parent diff — store-filtered,
// pre-glob — computing it at most once per commit per run. Merges diff
// against their first parent only; the root commit diffs against an empty
// tree. The result is a pure function of the commit pair, independent of
// any document's globs.
func (g *GitProvider) commitChange(h plumbing.Hash) (*commitChange, error) {
	if cached, ok := g.changes[h]; ok {
		return cached, nil
	}
	commit, err := g.repo.CommitObject(h)
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	var parentTree *object.Tree
	entry := &commitChange{when: commit.Committer.When}
	if commit.NumParents() > 0 {
		entry.parent = commit.ParentHashes[0] // merges diff against first parent only
		parent, err := commit.Parent(0)
		if err != nil {
			return nil, err
		}
		parentTree, err = parent.Tree()
		if err != nil {
			return nil, err
		}
	}
	diff, err := object.DiffTree(parentTree, tree)
	if err != nil {
		return nil, err
	}
	for _, c := range diff {
		path := c.To.Name
		if path == "" {
			path = c.From.Name
		}
		if g.storeDir != "" && (path == g.storeDir || strings.HasPrefix(path, g.storeDir+"/")) {
			continue
		}
		entry.paths = append(entry.paths, path)
	}
	g.changes[h] = entry
	return entry, nil
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

// Snapshot enumerates the HEAD tree files matching the globs, mapping each
// path to its blob SHA — the git-mode content identity. Used for the scope
// file count at observe time.
func (g *GitProvider) Snapshot(globs []string) (map[string]string, error) {
	tree, err := g.headTree()
	if err != nil {
		return nil, err
	}
	entries, err := walkAll(tree, "", g.storeDir)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range entries {
		for _, glob := range globs {
			if pathmatch.Match(glob, e.Path) {
				out[e.Path] = e.Hash
				break
			}
		}
	}
	return out, nil
}

// walkAll lists every blob entry under the tree, store paths filtered.
func walkAll(t *object.Tree, prefix, storeDir string) ([]scope.Entry, error) {
	if storeDir != "" && (prefix == storeDir || strings.HasPrefix(prefix, storeDir+"/")) {
		return nil, nil
	}
	var out []scope.Entry
	for i := 0; i < len(t.Entries); i++ {
		e := &t.Entries[i]
		path := e.Name
		if prefix != "" {
			path = prefix + "/" + e.Name
		}
		if e.Mode == filemode.Dir {
			sub, err := t.Tree(e.Name)
			if err != nil {
				return nil, err
			}
			rest, err := walkAll(sub, path, storeDir)
			if err != nil {
				return nil, err
			}
			out = append(out, rest...)
			continue
		}
		out = append(out, scope.Entry{Path: path, Mode: fmt.Sprintf("%o", e.Mode), Hash: e.Hash.String()})
	}
	return out, nil
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

func (h *HashProvider) Dirty([]string) ([]string, []string, error) { return nil, nil, nil }

func (h *HashProvider) AnchorReachable(string) (bool, error) { return false, nil }

func (h *HashProvider) RecoveredAnchor([]string, string, int) (string, error) { return "", nil }

func (h *HashProvider) Attribution(_, _ string, _ []string) ([]string, []string, error) {
	return nil, nil, fmt.Errorf("attribution is unavailable in hash mode — the target has no git history")
}

func (h *HashProvider) ChangedPathsBetween(_, _ string) ([]string, error) {
	return nil, fmt.Errorf("range attribution is unavailable in hash mode — the target has no git history")
}

func (h *HashProvider) FileStatus(string) (string, error) { return "clean", nil }

func (h *HashProvider) HeadState() (bool, bool, error) { return false, false, nil }

// fileSum hashes a file through the streaming normalization pipeline.
func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := hashnorm.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + h.Hex(), nil
}
