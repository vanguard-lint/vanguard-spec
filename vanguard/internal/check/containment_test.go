package check

// Containment benchmarks (Q-3.a, Q-5.b-2, Q-5.d-2): static, OS-independent
// proofs that the engine never reaches outside its boundary. The oracle is
// the AST/import graph itself — no strace needed.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func engineSources(t *testing.T, dirs ...string) map[string]string {
	t.Helper()
	pkgDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	// package dir is internal/check; the module root is two levels up.
	moduleRoot := filepath.Dir(filepath.Dir(pkgDir))
	out := map[string]string{}
	for _, d := range dirs {
		dir := filepath.Join(moduleRoot, d)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			out[d+"/"+e.Name()] = string(raw)
		}
	}
	return out
}

func importsOf(t *testing.T, file, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	var out []string
	for _, imp := range f.Imports {
		out = append(out, strings.Trim(imp.Path.Value, `"`))
	}
	return out
}

// Q-3.a-1: no engine package opens sockets — "net" appears nowhere in the
// import graph of the engine.
func TestEngineImportsNoNetwork(t *testing.T) {
	srcs := engineSources(t,
		"internal/check", "internal/docmodel", "internal/provider",
		"internal/lockfile", "internal/scope", "internal/structure",
		"internal/hashnorm", "internal/config", "internal/pathmatch",
		"internal/duration")
	for file, src := range srcs {
		for _, imp := range importsOf(t, file, src) {
			if imp == "net" || imp == "net/http" || strings.HasPrefix(imp, "net/") {
				t.Errorf("%s imports %q — the engine must not open sockets", file, imp)
			}
		}
	}
}

// Q-3.a-2 / Q-5.b-2: os/exec appears in exactly two sanctioned places —
// bindings.go (allowlisted commands) and provider.go (the git CLI invoked
// with a fixed argv) — and no engine file ever routes a command through a
// shell.
func TestBindingsAreTheOnlyExecAndNeverShell(t *testing.T) {
	srcs := engineSources(t,
		"internal/check", "internal/docmodel", "internal/provider",
		"internal/lockfile", "internal/scope", "internal/structure",
		"internal/hashnorm", "internal/config", "internal/pathmatch",
		"internal/duration")
	sanctioned := map[string]bool{
		"internal/check/bindings.go":    true,
		"internal/provider/provider.go": true,
	}
	for file, src := range srcs {
		for _, imp := range importsOf(t, file, src) {
			if imp == "os/exec" && !sanctioned[file] {
				t.Errorf("%s imports os/exec — only bindings.go and provider.go may execute", file)
			}
		}
		// No shell ever: neither as an argv[0] literal nor as a flag pair.
		for _, shell := range []string{"sh -c", "cmd.exe", "/bin/sh", "cmd /c", "powershell"} {
			if strings.Contains(src, shell) {
				t.Errorf("%s mentions %q — execution is direct argv, never a shell", file, shell)
			}
		}
	}
	// The provider's single exec site invokes git via LookPath with a
	// fixed argv — no interpolation.
	prov := srcs["internal/provider/provider.go"]
	if !strings.Contains(prov, `exec.LookPath("git")`) || !strings.Contains(prov, "exec.Command(gitBin") {
		t.Fatal("provider must execute only the git binary via LookPath + fixed argv")
	}
	if strings.Count(prov, "exec.Command(") != 1 {
		t.Fatal("provider must have exactly one exec.Command call site")
	}
}

// Q-5.d-2: no recording capability exists — check is unconditionally
// read-only, so the CLI must not even offer it.
func TestNoRecordCapability(t *testing.T) {
	srcs := engineSources(t, "cmd/vanguard")
	for file, src := range srcs {
		if strings.Contains(src, "--record") || strings.Contains(strings.ToLower(src), "vanguard record") {
			t.Errorf("%s mentions a record capability — check has none", file)
		}
	}
}
