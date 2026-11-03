package check

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Render emits the envelope in the requested format. json and yaml are
// byte-stable machine formats; text is the human report.
func Render(env *Envelope, format string) (string, error) {
	switch format {
	case "json":
		out, err := json.MarshalIndent(env, "", "  ")
		if err != nil {
			return "", err
		}
		return string(out) + "\n", nil
	case "yaml":
		out, err := yaml.Marshal(env)
		if err != nil {
			return "", err
		}
		return string(out), nil
	case "text", "":
		return renderText(env), nil
	default:
		return "", usageError("unknown format %q — want text, json, or yaml", format)
	}
}

func renderText(env *Envelope) string {
	var b strings.Builder
	switch env.Mode {
	case ModeObserve:
		fmt.Fprintf(&b, "Vanguard observe — %d attested", len(env.Documents))
		if len(env.Documents) > 0 {
			for _, d := range env.Documents {
				fmt.Fprintf(&b, "\n OBSERVED  %s  @ %s", d.Doc, short(d.Anchor))
			}
		}
		if len(env.Hints) > 0 {
			for _, h := range env.Hints {
				fmt.Fprintf(&b, "\n %s", h)
			}
		}
	case ModeRange:
		fmt.Fprintf(&b, "Vanguard range — %d affected", len(env.AffectedInRange))
		for _, d := range env.Documents {
			fmt.Fprintf(&b, "\n AFFECTED  %s", d.Doc)
			fmt.Fprintf(&b, "\n           paths: %s", strings.Join(d.ChangedPaths, ", "))
			fmt.Fprintf(&b, "\n           commits: %s", strings.Join(d.IntroducingCommits, ", "))
		}
	default:
		fmt.Fprintf(&b, "Vanguard check — %d documents", len(env.Documents))
		for _, d := range env.Documents {
			fmt.Fprintf(&b, "\n %-9s %s", strings.ToUpper(d.Status), d.Doc)
			if d.DocumentChanged {
				b.WriteString("  (document changed)")
			}
			for _, w := range d.TriggeredWatches {
				fmt.Fprintf(&b, "  [%s]", w)
			}
			for _, v := range d.Violations {
				fmt.Fprintf(&b, "\n         %s: %s", v.Rule, v.Message)
			}
			for _, h := range d.Hints {
				fmt.Fprintf(&b, "\n         → %s", h)
			}
		}
		if len(env.Unobserved) > 0 {
			fmt.Fprintf(&b, "\n UNOBSERVED  %s", strings.Join(env.Unobserved, ", "))
		}
		if len(env.Unwatched) > 0 {
			fmt.Fprintf(&b, "\n UNWATCHED  %s", strings.Join(env.Unwatched, ", "))
		}
		if env.LockLost {
			b.WriteString("\n LOCK LOST")
		}
		for _, h := range env.Hints {
			fmt.Fprintf(&b, "\n → %s", h)
		}
	}
	b.WriteString("\n")
	return b.String()
}
