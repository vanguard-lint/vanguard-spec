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
	if !strings.HasPrefix(sub, "sha256:") {
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
	dirty, err := p.Dirty([]string{"src/payments/**"})
	if err != nil || len(dirty) != 0 {
		t.Fatalf("clean worktree: %v %v", dirty, err)
	}

	// Uncommitted churn inside the scope.
	write(t, root, "src/payments/api.go", "package payments // changed\n")
	dirty, err = p.Dirty([]string{"src/payments/**"})
	if err != nil || len(dirty) != 1 || dirty[0] != "src/payments/api.go" {
		t.Fatalf("dirty = %v %v", dirty, err)
	}

	// Unrelated churn stays out of the scope.
	write(t, root, "docs/guide.md", "# changed\n")
	dirty, err = p.Dirty([]string{"src/payments/**"})
	if err != nil || len(dirty) != 1 {
		t.Fatalf("scope filtering failed: %v", dirty)
	}

	// Untracked files count as churn (git's classification).
	write(t, root, "src/payments/new.go", "package payments\n")
	dirty, err = p.Dirty([]string{"src/payments/**"})
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
	if err != nil || !strings.HasPrefix(hash, "sha256:") {
		t.Fatalf("scope hash = %s %v", hash, err)
	}
	if _, _, err := p.Attribution("a", "b", []string{"**"}); err == nil {
		t.Fatal("attribution must be reported unavailable in hash mode")
	}
	if dirty, err := p.Dirty([]string{"**"}); err != nil || dirty != nil {
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
