// Command vanguard — the CLI surface: init, check, observe. Wiring only;
// the engine lives in internal/check.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/vanguard-lint/vanguard/internal/check"
)

// version is injected at release time through
// -ldflags "-X main.version=X.Y.Z" from the git tag — the single source of
// truth per .vanguard/rules/versioning.md. Untagged builds report "dev".
var version = "dev"

func main() {
	root := &cobra.Command{
		Use:     "vanguard",
		Short:   "specification staleness guard: docs that fail loud when the code they describe moves",
		Version: version,
	}
	root.AddCommand(initCmd(), checkCmd(), observeCmd())
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// findStore locates .vanguard from the working directory upward.
func findStore() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, ".vanguard")
		if fi, statErr := os.Stat(candidate); statErr == nil && fi.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no .vanguard store found from %s upward — run vanguard init first", os.Args[0])
		}
		dir = parent
	}
}

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "scaffold the .vanguard store",
		RunE: func(cmd *cobra.Command, args []string) error {
			store := ".vanguard"
			if fi, err := os.Stat(store); err == nil && fi.IsDir() {
				return fmt.Errorf(".vanguard already exists — nothing to bootstrap")
			}
			for _, dir := range []string{store, filepath.Join(store, "docs"), filepath.Join(store, "schemas")} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
			}
			cfg := "# vanguard store — user-owned configuration\n" +
				"version: 1\n" +
				"discovery:\n" +
				"  include: [\"**/*.md\"]\n"
			if err := os.WriteFile(filepath.Join(store, "vanguard.yaml"), []byte(cfg), 0o644); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(store, "vanguard.lock"),
				[]byte("{\n  \"version\": 1,\n  \"docs\": {}\n}\n"), 0o644); err != nil {
				return err
			}
			fmt.Println("vanguard store scaffolded at .vanguard/")
			fmt.Println("— commit .vanguard/ immediately — the lock is the shared attestation record; un-committed, a fresh clone reports lock-lost")
			return nil
		},
	}
}

func checkCmd() *cobra.Command {
	var (
		pathArg        string
		fromRef        string
		toRef          string
		strict         bool
		format         string
		output         string
		nowStr         string
		runBindings    bool
		maxRecovery    int
		failOnAffected bool
		noColor        bool
	)
	cmd := &cobra.Command{
		Use:   "check [doc-id]",
		Short: "two-phase read-only check: structure verification, then freshness",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Presentation knobs are read per run: the flag variable is
			// bound at construction, but its parsed value only exists here.
			check.NoColor = noColor
			store, err := findStore()
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			if nowStr != "" {
				now, err = time.Parse(time.RFC3339, nowStr)
				if err != nil {
					return fmt.Errorf("--now must be RFC3339: %w", err)
				}
			}
			engine, err := check.Load(store, check.Options{Now: now, MaxRecoveryWalk: maxRecovery})
			if err != nil {
				return err
			}

			sel := check.Selection{
				Strict:           strict,
				FailOnAffected:   failOnAffected,
				RunBindings:      runBindings,
				MaxRecoveryWalk:  maxRecovery,
			}
			var mode string
			switch {
			case fromRef != "":
				if strict {
					return fmt.Errorf("--strict combined with --from/--to is a usage error — gap categories are meaningless in range mode")
				}
				if runBindings {
					return fmt.Errorf("--run-bindings combined with --from/--to is a usage error — bindings never run in range mode")
				}
				mode, sel.From, sel.To = check.ModeRange, fromRef, toRef
			case pathArg != "":
				mode = check.ModePath
				p, isDir, rerr := resolvePathArg(engine.TargetDir, pathArg)
				if rerr != nil {
					return rerr
				}
				sel.Path, sel.PathIsDir = p, isDir
			case len(args) == 1:
				mode, sel.DocID = check.ModeDoc, args[0]
			default:
				mode = check.ModeAll
			}
			sel.Mode = mode

			res, err := engine.Check(sel)
			if err != nil {
				return err
			}
			env := res.Envelope
			if runBindings && mode != check.ModeRange {
				if err := engine.ApplyBindings(env); err != nil {
					return err
				}
			}
			return emit(engine, env, format, output)
		},
	}
	cmd.Flags().StringVar(&pathArg, "path", "", "reverse lookup: documents whose watch globs claim this path")
	cmd.Flags().StringVar(&fromRef, "from", "", "range mode: base ref")
	cmd.Flags().StringVar(&toRef, "to", "", "range mode: head ref (defaults to HEAD)")
	cmd.Flags().BoolVar(&strict, "strict", false, "elevate unwatched/unobserved gaps to exit bit 8")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text, pretty, json, yaml")
	cmd.Flags().StringVar(&output, "output", "", "write the result to a file instead of stdout")
	cmd.Flags().StringVar(&nowStr, "now", "", "override the wall clock (RFC3339) for review_after determinism")
	cmd.Flags().BoolVar(&runBindings, "run-bindings", false, "execute the allowlisted bindings for the documents in scope")
	cmd.Flags().IntVar(&maxRecovery, "max-recovery-walk", 1000, "bound for the first-parent recovery walk")
	cmd.Flags().BoolVar(&failOnAffected, "fail-on-affected", false, "range mode: set exit bit 32 when affected_in_range is non-empty")
	cmd.Flags().BoolVar(&noColor, "no-color", false, "disable ANSI styling in the pretty format (also via the NO_COLOR env)")
	return cmd
}

func observeCmd() *cobra.Command {
	var (
		all     bool
		prune   bool
		atStr   string
		format  string
		noColor bool
	)
	cmd := &cobra.Command{
		Use:   "observe <doc-id>",
		Short: "attest a document — the only lockfile-mutating command",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Presentation knobs are read per run: the flag variable is
			// bound at construction, but its parsed value only exists here.
			check.NoColor = noColor
			store, err := findStore()
			if err != nil {
				return err
			}
			engine, err := check.Load(store, check.Options{})
			if err != nil {
				return err
			}
			req := check.ObserveRequest{All: all, Prune: prune}
			if len(args) == 1 {
				req.DocID = args[0]
			}
			if atStr != "" {
				at, parseErr := time.Parse(time.RFC3339, atStr)
				if parseErr != nil {
					return fmt.Errorf("--at must be RFC3339: %w", parseErr)
				}
				req.At = at
			}
			res, err := engine.Observe(req)
			if err != nil {
				return err
			}
			return emit(engine, res.Envelope, format, "")
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "guarded bootstrap hatch: attest only documents currently in the unobserved set")
	cmd.Flags().BoolVar(&prune, "prune", false, "standalone gc pass: drop lock entries whose id no longer exists")
	cmd.Flags().StringVar(&atStr, "at", "", "explicit attestation timestamp (RFC3339)")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text, pretty, json, yaml")
	cmd.Flags().BoolVar(&noColor, "no-color", false, "disable ANSI styling in the pretty format (also via the NO_COLOR env)")
	return cmd
}

// resolvePathArg resolves the --path argument against the target root;
// absolute paths must land inside it; .. segments are rejected.
func resolvePathArg(targetDir, arg string) (string, bool, error) {
	rel := arg
	if filepath.IsAbs(arg) {
		if r, err := filepath.Rel(targetDir, arg); err == nil && !filepath.IsAbs(r) && filepath.IsLocal(r) {
			rel = r
		} else {
			return arg, false, fmt.Errorf("--path must land inside the target: %s", arg)
		}
	} else if !filepath.IsLocal(arg) {
		// A relative path escaping the target (.. segments) is rejected —
		// the contract promises traversal never reaches the engine.
		return arg, false, fmt.Errorf("--path must stay inside the target: %s", arg)
	}
	abs := filepath.Join(targetDir, rel)
	fi, err := os.Stat(abs)
	isDir := err == nil && fi.IsDir()
	return filepath.ToSlash(filepath.Clean(rel)), isDir, nil
}

// emit renders and writes the envelope, then exits with the composed bits.
// The pretty format draws on engine state (declared watches, attestation
// instants) via RenderTable when the engine is available.
func emit(e *check.Engine, env *check.Envelope, format, output string) error {
	var (
		out string
		err error
	)
	if format == "pretty" && e != nil {
		out = e.RenderTable(env)
	} else {
		out, err = check.Render(env, format)
		if err != nil {
			return err
		}
	}
	if output != "" {
		if err := os.WriteFile(output, []byte(out), 0o644); err != nil {
			return err
		}
	} else {
		fmt.Print(out)
	}
	if env.ExitBits != 0 {
		os.Exit(env.ExitBits)
	}
	return nil
}
