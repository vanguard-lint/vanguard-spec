package check

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/term"
)

// renderPretty emits the envelope as a colored human report, learning the
// CLI presentation style of charm.land/lipgloss. It covers the same fields
// as renderText — identical words, richer presentation — and collapses to
// renderText when stdout is not a terminal, so pipes and CI logs stay clean.
// Presentation only: json, yaml, exit bits, and the envelope contract are
// untouched.

// NoColor disables ANSI styling in the pretty renderer: set by the
// --no-color flag, or by the NO_COLOR environment convention (present at
// all). With it, pretty degrades to the plain text rendering — piped and
// CI output stay byte-identical either way.
var NoColor bool

func renderPretty(env *Envelope, e *Engine) string {
	if NoColor || noColorEnv() {
		return renderText(env)
	}
	if !stdoutIsTerminal() {
		return renderText(env)
	}
	var s prettyStyles
	s.init()

	var b strings.Builder
	modeWord := "check"
	switch env.Mode {
	case ModeObserve:
		modeWord = "observe"
	case ModeRange:
		modeWord = "range"
	}
	w := terminalWidth()
	b.WriteString(center(s.title.Render("vanguard"), w) + "\n")
	b.WriteString(center(s.muted.Render(modeWord+" results"), w) + "\n\n")

	switch env.Mode {
	case ModeObserve:
		b.WriteString(center(s.renderObserve(env), w) + "\n")
	case ModeRange:
		b.WriteString(center(s.renderRange(env), w) + "\n")
	default:
		s.renderCheck(env, e, &b, w)
	}
	return b.String()
}

func (s prettyStyles) renderCheck(env *Envelope, e *Engine, b *strings.Builder, w int) {
	// The ledger, mirroring the website's check-results section: a verdict
	// tally above the table, one row per document below it.
	if stats := s.statsLine(env); stats != "" {
		b.WriteString(center(stats, w) + "\n\n")
	}

	// Gather row data first so the layout can adapt to the terminal width.
	type row struct{ doc, watches, attested, verdict string }
	rows := make([]row, 0, len(env.Documents))
	for _, d := range env.Documents {
		watches, attested := "—", "—"
		if e != nil {
			if doc := docByID(e.Docs, d.Doc); doc != nil && len(doc.PathGlobs) > 0 {
				watches = strings.Join(doc.PathGlobs, ", ")
			}
			if entry, ok := e.Lock.Docs[d.Doc]; ok && entry.ObservedAt != "" {
				if at, err := time.Parse(time.RFC3339, entry.ObservedAt); err == nil {
					attested = relTime(e.Now, at)
				}
			}
		}
		verdict := s.pillFor(d.Status)
		if reason := verdictReason(d); reason != "" {
			verdict += "\n" + s.dim.Render(reason)
		}
		rows = append(rows, row{s.docID.Render(d.Doc), s.dim.Render(watches), s.dim.Render(attested), verdict})
	}

	// Responsive: on a narrow terminal drop optional columns — Attested
	// first, then Watches — keeping Document + Verdict always visible.
	showWatches, showAttested := true, true
	if w > 0 {
		tableWidth := func(withWatches, withAttested bool) int {
			total := 0
			add := func(header string, cells []string) {
				m := lipgloss.Width(header)
				for _, c := range cells {
					if lw := lipgloss.Width(c); lw > m {
						m = lw
					}
				}
				total += m + 2 // cell padding
			}
			docCells := make([]string, len(rows))
			for i, r := range rows {
				docCells[i] = r.doc
			}
			add("Document", docCells)
			if withWatches {
				wCells := make([]string, len(rows))
				for i, r := range rows {
					wCells[i] = r.watches
				}
				add("Watches", wCells)
			}
			if withAttested {
				aCells := make([]string, len(rows))
				for i, r := range rows {
					aCells[i] = r.attested
				}
				add("Attested", aCells)
			}
			vCells := make([]string, len(rows))
			for i, r := range rows {
				vCells[i] = r.verdict
			}
			add("Verdict", vCells)
			return total
		}
		if tableWidth(true, true) > w {
			showAttested = false
		}
		if tableWidth(true, false) > w {
			showWatches = false
		}
	}

	headers := []string{"Document"}
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#484848"))).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		BorderColumn(false).
		BorderRow(false).
		BorderHeader(true).
		StyleFunc(func(row, col int) lipgloss.Style {
			return lipgloss.NewStyle().Padding(0, 1)
		})
	if showWatches {
		headers = append(headers, "Watches")
	}
	if showAttested {
		headers = append(headers, "Attested")
	}
	headers = append(headers, "Verdict")
	t.Headers(headers...)
	for _, r := range rows {
		cells := []string{r.doc}
		if showWatches {
			cells = append(cells, r.watches)
		}
		if showAttested {
			cells = append(cells, r.attested)
		}
		cells = append(cells, r.verdict)
		t.Row(cells...)
	}
	tableOut := t.String()
	tablePad := blockPad(tableOut, w)
	b.WriteString(center(tableOut, w) + "\n")

	if len(env.Unwatched) > 0 {
		b.WriteString("\n" + tablePad + s.dim.Render("unwatched: "+strings.Join(env.Unwatched, ", ")))
	}
	if env.LockLost {
		b.WriteString("\n" + tablePad + s.bad.Render("lock lost"))
	}
	for _, h := range env.Hints {
		b.WriteString("\n" + tablePad + s.dim.Render("→ "+h))
	}
	b.WriteString("\n\n" + tablePad + s.exitLine(env) + "\n")
}

func (s prettyStyles) renderObserve(env *Envelope) string {
	var b strings.Builder
	for _, d := range env.Documents {
		fmt.Fprintf(&b, "%s  %s %s\n", s.observed.Render("observed"),
			s.docID.Render(d.Doc), s.dim.Render("@ "+short(d.Anchor)))
	}
	for _, r := range env.Refused {
		fmt.Fprintf(&b, "%s  %s\n", s.refused.Render("refused"), s.docID.Render(r.Doc))
		fmt.Fprintf(&b, "     %s\n", s.dim.Render("reason: "+r.Reason))
		if len(r.Offending) > 0 {
			fmt.Fprintf(&b, "     %s\n", s.dim.Render("offending: "+strings.Join(r.Offending, ", ")))
		}
		for _, h := range r.Hints {
			fmt.Fprintf(&b, "     %s\n", s.dim.Render("→ "+h))
		}
	}
	for _, p := range env.Pruned {
		fmt.Fprintf(&b, "%s  %s\n", s.dim.Render("pruned"), s.dim.Render(p))
	}
	for _, h := range env.Hints {
		b.WriteString(s.dim.Render("→ "+h) + "\n")
	}
	lockWord := "unchanged"
	if env.Lock.Changed {
		lockWord = "written"
	}
	b.WriteString("\n" + s.dim.Render("lock "+lockWord+": "+env.Lock.Path))
	b.WriteString("\n\n" + s.exitLine(env))
	return b.String()
}

func (s prettyStyles) renderRange(env *Envelope) string {
	var b strings.Builder
	for _, id := range env.AffectedInRange {
		fmt.Fprintf(&b, "%s  %s\n", s.warn.Render("affected"), s.docID.Render(id))
	}
	for _, d := range env.Documents {
		fmt.Fprintf(&b, "%s  %s\n", s.warn.Render("affected"), s.docID.Render(d.Doc))
		if len(d.ChangedPaths) > 0 {
			fmt.Fprintf(&b, "     %s\n", s.dim.Render("paths: "+strings.Join(d.ChangedPaths, ", ")))
		}
		if len(d.IntroducingCommits) > 0 {
			fmt.Fprintf(&b, "     %s\n", s.dim.Render("commits: "+strings.Join(d.IntroducingCommits, ", ")))
		}
	}
	for _, h := range env.Hints {
		b.WriteString(s.dim.Render("→ "+h) + "\n")
	}
	b.WriteString("\n" + s.exitLine(env))
	return b.String()
}

// statsLine is the verdict tally rendered above the ledger — the terminal
// sibling of the website's verdict pills. Borderless by design.
func (s prettyStyles) statsLine(env *Envelope) string {
	counts := map[string]int{}
	for _, d := range env.Documents {
		counts[d.Status]++
	}
	var parts []string
	for _, st := range []string{StatusFresh, StatusStale, StatusDirty, StatusUnobserved} {
		if c := counts[st]; c > 0 {
			parts = append(parts, s.tagStyle(st).Render(fmt.Sprintf("%d %s", c, st)))
		}
	}
	if env.LockLost {
		parts = append(parts, s.lockLost.Render("lock lost"))
	}
	return strings.Join(parts, "  ")
}

func (s prettyStyles) exitLine(env *Envelope) string {
	if env.ExitBits == 0 {
		return s.ok.Render("exit 0")
	}
	return s.bad.Render(fmt.Sprintf("exit bits: %d", env.ExitBits))
}

// tagStyle maps a verdict to its severity tag, matching the website CSS.
func (s prettyStyles) tagStyle(status string) lipgloss.Style {
	switch status {
	case StatusFresh:
		return s.fresh
	case StatusStale:
		return s.stale
	case StatusDirty:
		return s.dirty
	case StatusUnobserved:
		return s.unobserved
	default:
		return s.lockLost
	}
}

func (s prettyStyles) pillFor(status string) string {
	switch status {
	case StatusFresh:
		return s.fresh.Render("✓ fresh")
	case StatusStale:
		return s.stale.Render("✗ stale")
	case StatusDirty:
		return s.dirty.Render("● dirty")
	case StatusUnobserved:
		return s.unobserved.Render("○ unobserved")
	default:
		return s.lockLost.Render("✗ invalid")
	}
}

// verdictReason is the one-line detail under the verdict pill. Structural
// violations lead — they are the engine's fail-closed truth about the doc;
// the freshness phrasing only appears when the axis verdicts are clean.
func verdictReason(d Document) string {
	if len(d.Violations) > 0 {
		r := d.Violations[0].Rule + ": " + d.Violations[0].Message
		if len(d.Violations) > 1 {
			r += fmt.Sprintf(" (+%d more)", len(d.Violations)-1)
		}
		return r
	}
	switch d.Status {
	case StatusFresh:
		if d.AnchorOrphaned {
			return "fresh, but the attested anchor is unreachable — re-run observe to re-anchor"
		}
		if d.AnchorRecovered {
			return "attested anchor is gone; a baseline was recovered — re-run observe to re-anchor"
		}
		return "baseline attested"
	case StatusUnobserved:
		return "never attested"
	case StatusDirty:
		return "uncommitted churn in scope"
	case StatusStale:
		if d.DocumentChanged {
			return "document changed"
		}
		if containsString(d.TriggeredWatches, "review_after") {
			return "review overdue"
		}
		if d.AnchorOrphaned {
			return "attested anchor unreachable — attribution unavailable"
		}
		if len(d.IntroducingCommits) > 0 {
			return "scope changed in " + short(d.IntroducingCommits[0])
		}
		// No commit attribution: the baseline itself moved — a kind
		// mismatch (globs or hash scheme changed) or an orphaned anchor.
		// Say what the engine said instead of blaming phantom commits.
		for _, h := range d.Hints {
			if strings.Contains(h, "scope-hash scheme changed") || strings.Contains(h, "globs changed") {
				return "globs or hash scheme changed — re-run observe"
			}
		}
		return "scope changed"
	default:
		if d.Status != "" {
			return d.Status
		}
		return ""
	}
}

// relTime renders the attestation age the way the website ledger does.
func relTime(now, at time.Time) string {
	d := now.Sub(at)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%d months ago", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%d years ago", int(d.Hours()/(24*365)))
	}
}

// prettyStyles holds the presentation palette. The accent is the vanguard
// brand red from the site header assets.
type prettyStyles struct {
	title, dim, muted, docID, ok, bad, warn lipgloss.Style
	fresh, stale, dirty, unobserved         lipgloss.Style
	lockLost, observed, refused             lipgloss.Style
}

func (s *prettyStyles) init() {
	// Palette learned from the opencode TUI's default theme
	// (sst/opencode, packages/tui/src/theme/assets/v2/opencode.json,
	// dark variant): green.200 success, red.200 error, #f5a742 warning,
	// purple.200 accent, neutral greys, #484848 border. Tags carry the
	// text color only — no backgrounds, no opacity. The title keeps the
	// vanguard brand red from the site header assets.
	s.title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E74539"))
	s.dim = lipgloss.NewStyle().Foreground(lipgloss.Color("#808080"))
	s.muted = lipgloss.NewStyle().Foreground(lipgloss.Color("#b5b5b5"))
	s.docID = lipgloss.NewStyle().Foreground(lipgloss.Color("#eeeeee")).Bold(true)
	s.ok = lipgloss.NewStyle().Foreground(lipgloss.Color("#7fd88f"))
	s.bad = lipgloss.NewStyle().Foreground(lipgloss.Color("#e06c75"))
	s.warn = lipgloss.NewStyle().Foreground(lipgloss.Color("#f5a742"))
	s.fresh = lipgloss.NewStyle().Foreground(lipgloss.Color("#7fd88f")).Bold(true)
	s.stale = lipgloss.NewStyle().Foreground(lipgloss.Color("#e06c75")).Bold(true)
	s.dirty = lipgloss.NewStyle().Foreground(lipgloss.Color("#f5a742")).Bold(true)
	s.unobserved = lipgloss.NewStyle().Foreground(lipgloss.Color("#9d7cd8")).Bold(true)
	s.lockLost = lipgloss.NewStyle().Foreground(lipgloss.Color("#e06c75")).Bold(true)
	s.observed = lipgloss.NewStyle().Foreground(lipgloss.Color("#7fd88f")).Bold(true)
	s.refused = lipgloss.NewStyle().Foreground(lipgloss.Color("#e06c75")).Bold(true)
}

// terminalWidth reports the output width: the COLUMNS override first (the
// standard escape hatch), then the terminal itself; 0 when neither is
// available (pipes, CI) — layout then falls back to flush-left.
func terminalWidth() int {
	if cs := os.Getenv("COLUMNS"); cs != "" {
		if n, err := strconv.Atoi(cs); err == nil && n > 0 {
			return n
		}
	}
	if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil {
		return w
	}
	return 0
}

// center pads every non-empty line of a block so the block sits at the
// horizontal middle of a terminal w columns wide. The shift is derived from
// the block's widest line and applied uniformly, so multi-line blocks keep
// their internal alignment. A w of 0 degrades to the plain two-space indent.
func center(block string, w int) string {
	lines := strings.Split(block, "\n")
	pad := blockPad(block, w)
	for i, line := range lines {
		if line != "" {
			lines[i] = pad + line
		}
	}
	return strings.Join(lines, "\n")
}

// blockPad returns the left padding center() would give a block — callers
// reuse it to align sibling lines with a block's left edge (the footer
// lines under the ledger).
func blockPad(block string, w int) string {
	maxw := 0
	for _, line := range strings.Split(block, "\n") {
		if lw := lipgloss.Width(line); lw > maxw {
			maxw = lw
		}
	}
	shift := 2
	if w > 0 && maxw < w {
		shift += (w - maxw) / 2
	}
	return strings.Repeat(" ", shift)
}

// stdoutIsTerminal reports whether stdout is a character device; pretty
// output collapses to plain text for pipes, redirects, and CI. Setting
// CLICOLOR_FORCE to a non-zero value forces the styled path back on —
// the standard convention for recordings, screenshots, and CI badges.
func stdoutIsTerminal() bool {
	if f := os.Getenv("CLICOLOR_FORCE"); f != "" && f != "0" {
		return true
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// noColorEnv reports the NO_COLOR environment convention: present at all
// (even empty) disables ANSI styling.
func noColorEnv() bool {
	_, ok := os.LookupEnv("NO_COLOR")
	return ok
}

// RenderTable renders a check envelope as the document ledger — one row per
// document with its watch globs, attestation age, and verdict, mirroring the
// website's check-results table. It draws on engine state (declared globs,
// lock attestation instants) that the envelope intentionally omits, so the
// json/yaml contract stays untouched. Observe and range modes keep the
// compact layout. Falls back to the plain text report off-terminal.
func (e *Engine) RenderTable(env *Envelope) string {
	return renderPretty(env, e)
}
