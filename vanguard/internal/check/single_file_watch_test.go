package check

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
)

// Regression: a bare single-file watch ("README.md") canonicalizes to
// "README.md/**" and hashed as a missing subtree — the scope hash was the
// constant empty-tree value, so committed content changes to the watched
// file never went stale (silently fresh). The subtree strategy must hash
// the blob when the watched path is a file.
func TestSingleFileWatchGoesStaleOnContentChange(t *testing.T) {
	root := t.TempDir()
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(root, ".vanguard")
	if err := os.MkdirAll(filepath.Join(store, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	wf(t, root, ".vanguard/vanguard.yaml", "version: 1\n")
	wf(t, root, ".vanguard/vanguard.lock", "{\"version\": 1, \"docs\": {}}\n")
	wf(t, root, ".vanguard/docs/readme.md",
		"---\nid: readme\nschema: none\nwatches:\n  paths: [\"README.md\"]\n---\nBinding card: watches README.md.\n")
	wf(t, root, "README.md", "# spec v1\n")
	w, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Add("."); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit("initial", &git.CommitOptions{Author: sig()}); err != nil {
		t.Fatal(err)
	}

	e := load(t, store)
	if _, err := e.Observe(ObserveRequest{DocID: "readme"}); err != nil {
		t.Fatal(err)
	}

	// Sanity: the recorded scope hash must not be the degenerate empty-tree
	// constant for a file watch.
	entry := e.Lock.Docs["readme"]
	if entry.ScopeTreeHash == "tree:4b825dc642cb6eb9a060e54bf8d69288fbee4904" {
		t.Fatalf("file watch must hash the blob, not the empty tree: %s", entry.ScopeTreeHash)
	}

	wf(t, root, "README.md", "# spec v2 — the code this describes changed\n")
	commitAll(t, root)

	e2 := load(t, store)
	res, err := e2.Check(Selection{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	d := res.Envelope.Documents[0]
	if d.Status != StatusStale {
		t.Fatalf("status = %s, want stale after committed content change", d.Status)
	}
	if !containsString(d.TriggeredWatches, "paths") {
		t.Fatalf("triggered = %v, want paths", d.TriggeredWatches)
	}
	if len(d.ChangedPaths) != 1 || d.ChangedPaths[0] != "README.md" {
		t.Fatalf("changed paths = %v", d.ChangedPaths)
	}
}
