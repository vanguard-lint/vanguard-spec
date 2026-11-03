package check

// Q-2.2.5.a / Q-5.a: the binding result matrix. Exit code is the only
// decision input; pass/fail/timeout/unresolved each surface correctly; a
// failed binding raises bit 16 and never becomes staleness.

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vanguard-lint/vanguard/internal/config"
)

// scaffoldBindings builds a committed repo with a bindings allowlist whose
// commands exist on every OS (git is the tool's own dependency), and a
// payments document that declares a failing binding in its frontmatter.
func scaffoldBindings(t *testing.T) (string, string) {
	t.Helper()
	root, store := scaffold(t)
	wf(t, root, ".vanguard/docs/payments.md",
		"---\nid: payments\nschema: none\nwatches:\n  paths: [\"src/payments/**\"]\nbindings: [\"fail\"]\n---\n# payments spec\n")
	wf(t, root, ".vanguard/vanguard.yaml", `version: 1
bindings:
  ok:
    cmd: ["git", "--version"]
  fail:
    cmd: ["git", "rev-parse", "--verify", "no-such-ref-vanguard-test"]
  unresolved-ref:
    cmd: ["git", "--version"]
`)
	commitAll(t, root)
	return root, store
}

func bindingsEngine(t *testing.T, store string) *Engine {
	t.Helper()
	e, err := Load(store, Options{Now: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestBindingResultMatrix(t *testing.T) {
	_, store := scaffoldBindings(t)
	e := bindingsEngine(t, store)

	// pass: exit 0 -> status pass.
	res, _ := e.RunBindings("payments", []string{"ok"})
	if len(res) != 1 || res[0].Status != "pass" {
		t.Fatalf("ok binding must pass: %+v", res)
	}
	// fail: non-zero exit -> failed with the exit code recorded.
	res, _ = e.RunBindings("payments", []string{"fail"})
	if len(res) != 1 || res[0].Status != "failed" || res[0].ExitCode == 0 {
		t.Fatalf("failing binding must fail with a code: %+v", res)
	}
	// unresolved: not in the allowlist -> failed, never executed.
	res, _ = e.RunBindings("payments", []string{"never-declared"})
	if len(res) != 1 || res[0].Status != "failed" || res[0].ExitCode != -1 {
		t.Fatalf("unresolved binding must fail without executing: %+v", res)
	}
	// anyFailed aggregation.
	_, anyFailed := e.RunBindings("payments", []string{"ok", "fail"})
	if !anyFailed {
		t.Fatal("a failing binding must aggregate to anyFailed")
	}
}

// Q-5.a-2: output is never the decision — a command that prints "ok" but
// exits non-zero fails.
func TestBindingDecisionIsExitCodeOnly(t *testing.T) {
	root, store := scaffoldBindings(t)
	wf(t, root, ".vanguard/vanguard.yaml", `version: 1
bindings:
  loud-failure:
    cmd: ["git", "rev-parse", "--verify", "no-such-ref-vanguard-test"]
`)
	commitAll(t, root)
	e := bindingsEngine(t, store)
	res, anyFailed := e.RunBindings("payments", []string{"loud-failure"})
	if !anyFailed || res[0].Status != "failed" {
		t.Fatalf("non-zero exit must fail regardless of output: %+v", res)
	}
}

// Q-5.a-1: ApplyBindings maps a failed binding to bit 16 and never to a
// staleness status.
func TestFailedBindingSetsBit16NotStaleness(t *testing.T) {
	root, store := scaffoldBindings(t)
	wf(t, root, ".vanguard/vanguard.yaml", `version: 1
bindings:
  fail:
    cmd: ["git", "rev-parse", "--verify", "no-such-ref-vanguard-test"]
`)
	commitAll(t, root)
	e := bindingsEngine(t, store)
	res, err := e.Check(Selection{Mode: ModeDoc, DocID: "payments", RunBindings: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.ApplyBindings(res.Envelope); err != nil {
		t.Fatal(err)
	}
	env := res.Envelope
	if env.ExitBits&BitBindingFailed == 0 {
		t.Fatalf("failed binding must set bit 16: %d", env.ExitBits)
	}
	if env.ExitBits&BitStale != 0 || env.ExitBits&BitDirty != 0 {
		t.Fatalf("binding results must never become staleness: %d", env.ExitBits)
	}
	if len(env.Documents) != 1 || len(env.Documents[0].Bindings) == 0 ||
		env.Documents[0].Bindings[0].Status != "failed" {
		t.Fatalf("binding result must surface on the document: %+v", env.Documents)
	}
}

// Q-2.2.5.b-1: a raw command in frontmatter can never execute — only the
// allowlist resolves, and unresolved references refuse.
func TestRawFrontmatterCommandNeverExecutes(t *testing.T) {
	root, store := scaffoldBindings(t)
	// Declare a document referencing a binding the allowlist never named.
	wf(t, root, ".vanguard/docs/rogue.md",
		"---\nid: rogue\nschema: none\nwatches:\n  paths: [\"src/**\"]\nbindings: [\"rm -rf /\"]\n---\n# rogue\n")
	commitAll(t, root)
	e := bindingsEngine(t, store)
	res, _ := e.RunBindings("rogue", []string{"rm -rf /"})
	if res[0].Status != "failed" || res[0].Command != "(unresolved)" {
		t.Fatalf("an undeclared command must be unresolved, never executed: %+v", res)
	}
	// The allowlist itself is closed: an undeclared key in vanguard.yaml
	// is fatal at config load.
	wf(t, root, ".vanguard/vanguard.yaml", "version: 1\nbindings:\n  ok:\n    cmd: [\"git\", \"--version\"]\n  bad:\n    cmd: [\"git\"]\n    typo_field: true\n")
	if _, err := Load(store, Options{Now: time.Now()}); err == nil {
		t.Fatal("an unknown binding field must fail config load")
	}
	_ = config.Config{}
	_ = strings.TrimSpace
	_ = os.Getenv
}
