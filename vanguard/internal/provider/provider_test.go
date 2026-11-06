package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func signature() *object.Signature {
	return &object.Signature{Name: "t", Email: "t@t", When: time.Now()}
}

// initRepo creates a git repository with one committed file layout:
// src/payments/api.go, src/payments/README.md, docs/guide.md
// and returns (root, commit1 SHA).
func initRepo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	w, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "src/payments/api.go", "package payments\n")
	write(t, root, "src/payments/README.md", "# payments\n")
	write(t, root, "docs/guide.md", "# guide\n")
	if _, err := w.Add("."); err != nil {
		t.Fatal(err)
	}
	c, err := w.Commit("initial", &git.CommitOptions{Author: signature()})
	if err != nil {
		t.Fatal(err)
	}
	return root, c.String()
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func openGit(t *testing.T, root string) *GitProvider {
	t.Helper()
	p, err := OpenGit(root, ".vanguard")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGitHead(t *testing.T) {
	root, c1 := initRepo(t)
	p := openGit(t, root)
	head, err := p.HEAD()
	if err != nil {
		t.Fatal(err)
	}
	if head != c1 {
		t.Fatalf("HEAD = %s, want %s", head, c1)
	}
}

func TestScopeHashSubtreeVsEnumerated(t *testing.T) {
	root, _ := initRepo(t)
	p := openGit(t, root)

	sub, err := p.ScopeHash([]string{"src/payments/**"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sub, "sha256v2:") {
		t.Fatalf("combined hash must be kind-prefixed: %s", sub)
	}
	// A different scope must hash differently.
	other, err := p.ScopeHash([]string{"docs/**"})
	if err != nil {
		t.Fatal(err)
	}
	if other == sub {
		t.Fatal("different scopes must hash differently")
	}
	// Same content, same hash: deterministic across calls.
	again, err := p.ScopeHash([]string{"src/payments/**"})
	if err != nil || again != sub {
		t.Fatalf("scope hash must be deterministic: %s vs %s", again, sub)
	}
}

func TestScopeHashReactsToChange(t *testing.T) {
	root, _ := initRepo(t)
	p := openGit(t, root)
	before, err := p.ScopeHash([]string{"src/payments/**/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "src/payments/gateway.go", "package payments\n")
	// Worktree changes do not move the HEAD tree: the hash is decided at
	// HEAD — commit to see it move.
	commitAll(t, p, "add gateway")
	// The commit is an external state change: a provider is one run's
	// snapshot (canon: "Memoization within a run"), so the next run
	// re-opens.
	p = openGit(t, root)
	after, err := p.ScopeHash([]string{"src/payments/**/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("committed churn must move the scope hash")
	}
}

func TestDirtyDetection(t *testing.T) {
	root, _ := initRepo(t)
	p := openGit(t, root)

	// Clean worktree → no churn in scope.
	dirty, _, err := p.Dirty([]string{"src/payments/**"})
	if err != nil || len(dirty) != 0 {
		t.Fatalf("clean worktree: %v %v", dirty, err)
	}

	// Uncommitted churn inside the scope.
	write(t, root, "src/payments/api.go", "package payments // changed\n")
	p = openGit(t, root) // new run snapshot after external churn
	dirty, _, err = p.Dirty([]string{"src/payments/**"})
	if err != nil || len(dirty) != 1 || dirty[0] != "src/payments/api.go" {
		t.Fatalf("dirty = %v %v", dirty, err)
	}

	// Unrelated churn stays out of the scope.
	write(t, root, "docs/guide.md", "# changed\n")
	p = openGit(t, root) // new run snapshot after external churn
	dirty, _, err = p.Dirty([]string{"src/payments/**"})
	if err != nil || len(dirty) != 1 {
		t.Fatalf("scope filtering failed: %v", dirty)
	}

	// Untracked files count as churn (git's classification).
	write(t, root, "src/payments/new.go", "package payments\n")
	p = openGit(t, root) // new run snapshot after external churn
	dirty, _, err = p.Dirty([]string{"src/payments/**"})
	if err != nil || len(dirty) != 2 {
		t.Fatalf("untracked must count: %v", dirty)
	}
}

func TestAnchorReachable(t *testing.T) {
	root, c1 := initRepo(t)
	p := openGit(t, root)
	if ok, err := p.AnchorReachable(c1); err != nil || !ok {
		t.Fatalf("initial commit is an ancestor: %v %v", ok, err)
	}
	// A dangling commit (not in HEAD's history) is not reachable.
	dangling := t.TempDir()
	repo2, err := git.PlainInit(dangling, false)
	if err != nil {
		t.Fatal(err)
	}
	w, _ := repo2.Worktree()
	write(t, dangling, "x.txt", "x")
	_, _ = w.Add(".")
	c2, err := w.Commit("dangling", &git.CommitOptions{Author: signature()})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := p.AnchorReachable(c2.String()); err != nil || ok {
		t.Fatalf("foreign commit must not be reachable: %v %v", ok, err)
	}
}

func TestAttribution(t *testing.T) {
	root, c1 := initRepo(t)
	p := openGit(t, root)
	write(t, root, "src/payments/gateway.go", "package payments\n")
	c2 := commitAll(t, p, "add gateway")
	write(t, root, "docs/guide.md", "# guide v2\n")
	c3 := commitAll(t, p, "docs update")

	paths, commits, err := p.Attribution(c1, "", []string{"src/payments/**"})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "src/payments/gateway.go" {
		t.Fatalf("changed paths = %v", paths)
	}
	if len(commits) != 1 || commits[0] != c2.String() {
		t.Fatalf("introducing commits = %v (want %s)", commits, c2.String())
	}
	_ = c3
}

// Chronological order: introducing commits list oldest → newest, ties
// broken by OID — the design contract, not an alphabetical accident.
func TestAttributionChronological(t *testing.T) {
	root, c1 := initRepo(t)
	p := openGit(t, root)
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	write(t, root, "src/payments/a.go", "package payments\n")
	w, err := p.repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Add("."); err != nil {
		t.Fatal(err)
	}
	c2, err := w.Commit("first", &git.CommitOptions{
		Author:            signature(),
		Committer:         &object.Signature{Name: "t", Email: "t@x", When: base},
		AllowEmptyCommits: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "src/payments/b.go", "package payments\n")
	if _, err := w.Add("."); err != nil {
		t.Fatal(err)
	}
	c3, err := w.Commit("second", &git.CommitOptions{
		Author:            signature(),
		Committer:         &object.Signature{Name: "t", Email: "t@x", When: base.Add(2 * time.Hour)},
		AllowEmptyCommits: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, commits, err := p.Attribution(c1, "", []string{"src/payments/**"})
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 || commits[0] != c2.String() || commits[1] != c3.String() {
		t.Fatalf("commits must read oldest → newest, got %v (want [%s %s])", commits, c2, c3)
	}
	// The OID tie-break: two commits with identical committer instants
	// still yield a deterministic, OID-sorted list.
	write(t, root, "src/payments/a.go", "package payments // v2\n")
	if _, err := w.Add("."); err != nil {
		t.Fatal(err)
	}
	c4, err := w.Commit("tie-a", &git.CommitOptions{
		Author:            signature(),
		Committer:         &object.Signature{Name: "t", Email: "t@x", When: base.Add(4 * time.Hour)},
		AllowEmptyCommits: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "src/payments/b.go", "package payments // v2\n")
	if _, err := w.Add("."); err != nil {
		t.Fatal(err)
	}
	c5, err := w.Commit("tie-b", &git.CommitOptions{
		Author:            signature(),
		Committer:         &object.Signature{Name: "t", Email: "t@x", When: base.Add(4 * time.Hour)},
		AllowEmptyCommits: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Two more commits are an external state change: the next attribution
	// is a new run and re-opens the provider (one snapshot per run).
	p = openGit(t, root)
	_, commits, err = p.Attribution(c1, "", []string{"src/payments/**"})
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 4 {
		t.Fatalf("expected [c2 c3 tie-a tie-b], got %v", commits)
	}
	tail := map[string]bool{c4.String(): true, c5.String(): true}
	if !tail[commits[2]] || !tail[commits[3]] {
		t.Fatalf("the equal-instant pair must fill the tail, got %v", commits)
	}
	if commits[2] >= commits[3] {
		t.Fatalf("equal-instant commits must break ties by OID, got %v", commits[2:])
	}
}

func commitAll(t *testing.T, p *GitProvider, msg string) plumbing.Hash {
	t.Helper()
	w, err := p.repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Add("."); err != nil {
		t.Fatal(err)
	}
	c, err := w.Commit(msg, &git.CommitOptions{Author: signature(), AllowEmptyCommits: true})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestHashProvider(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/a.go", "package a\n")
	write(t, root, "src/b.go", "package b\n")
	write(t, root, ".vanguard/vanguard.lock", "{}")
	p := OpenHash(root, ".vanguard")

	if p.GitMode() {
		t.Fatal("hash provider is not git mode")
	}
	snap, err := p.Snapshot([]string{"src/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap) != 2 {
		t.Fatalf("snapshot = %v", snap)
	}
	for _, sum := range snap {
		if !strings.HasPrefix(sum, "sha256:") {
			t.Fatalf("snapshot hash must be prefixed: %s", sum)
		}
	}
	hash, err := p.ScopeHash([]string{"src/*.go"})
	if err != nil || !strings.HasPrefix(hash, "sha256v2:") {
		t.Fatalf("scope hash = %s %v", hash, err)
	}
	if _, _, err := p.Attribution("a", "b", []string{"**"}); err == nil {
		t.Fatal("attribution must be reported unavailable in hash mode")
	}
	if dirty, _, err := p.Dirty([]string{"**"}); err != nil || dirty != nil {
		t.Fatalf("hash mode has no dirty concept: %v %v", dirty, err)
	}
}

func TestUnbornRepository(t *testing.T) {
	root := t.TempDir()
	if _, err := git.PlainInit(root, false); err != nil {
		t.Fatal(err)
	}
	p := openGit(t, root)
	head, err := p.HEAD()
	if err != nil || head != "" {
		t.Fatalf("unborn HEAD = %q %v", head, err)
	}
}
