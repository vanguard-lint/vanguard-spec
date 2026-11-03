package check

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// BindingsFor returns the binding references for the documents in scope:
// pending when not executed (the only notation for referenced-but-not-run).
func (e *Engine) BindingsFor(docID string, names []string) []Binding {
	out := make([]Binding, 0, len(names))
	for _, n := range names {
		if _, ok := e.Cfg.Bindings[n]; !ok {
			// An unresolvable reference is a structure failure — the doc
			// references a binding the allowlist never declared.
			out = append(out, Binding{Name: n, Status: "failed",
				ExitCode: -1, Command: "(unresolved)",
				Stderr: fmt.Sprintf("binding %q is not in the vanguard.yaml allowlist", n)})
			continue
		}
		out = append(out, Binding{Name: n, Status: "pending"})
	}
	return out
}

// RunBindings executes the allowlisted commands for one document. Exit code
// is the only decision input; results are transient and never persisted.
// No shell interpolation, ever: direct argv exec.
func (e *Engine) RunBindings(docID string, names []string) ([]Binding, bool) {
	var out []Binding
	anyFailed := false
	for _, n := range names {
		b, ok := e.Cfg.Bindings[n]
		if !ok {
			anyFailed = true
			out = append(out, Binding{Name: n, Status: "failed",
				ExitCode: -1, Command: "(unresolved)",
				Stderr: fmt.Sprintf("binding %q is not in the vanguard.yaml allowlist", n)})
			continue
		}
		result := Binding{Name: n}
		result.Command = strings.Join(b.Cmd, " ")

		ctx := context.Background()
		if b.Timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, b.Timeout)
			defer cancel()
		}
		cmd := exec.CommandContext(ctx, b.Cmd[0], b.Cmd[1:]...) //nolint:gosec // allowlisted argv, never a shell
		cmd.Dir = e.TargetDir
		if b.CWD == "store" {
			cmd.Dir = e.StoreDir
		}
		cmd.Env = append(cmd.Environ(),
			"VANGUARD_TARGET="+e.TargetDir,
			"VANGUARD_DOC="+docID,
			"VANGUARD_STORE="+e.StoreDir,
		)
		output, err := cmd.CombinedOutput()
		if err != nil {
			anyFailed = true
			result.Status = "failed"
			result.Stderr = string(output)
			if exitErr, ok := err.(*exec.ExitError); ok {
				result.ExitCode = exitErr.ExitCode()
			} else if ctx.Err() == context.DeadlineExceeded {
				result.ExitCode = -1
				result.Stderr = "binding timed out"
			} else {
				result.ExitCode = -1
			}
		} else {
			result.Status = "pass"
		}
		out = append(out, result)
	}
	return out, anyFailed
}

// ProvenanceLine reports which allowlist revision executes — always printed
// under --run-bindings.
func (e *Engine) ProvenanceLine(n int) string {
	head, err := e.Prov.HEAD()
	if err != nil || head == "" {
		return fmt.Sprintf("bindings from vanguard.yaml @ no-git (%d commands)", n)
	}
	return fmt.Sprintf("bindings from vanguard.yaml @ %s (%d commands)", short(head), n)
}

func short(oid string) string {
	if len(oid) > 7 {
		return oid[:7]
	}
	return oid
}
