// Package check implements the two-phase check engine, the observe
// attestation engine, and the output contract. All modes emit one envelope,
// identical in json and yaml; exit bits compose by OR:
// 1 fatal, 2 stale, 4 dirty, 8 gaps (--strict), 16 binding_failed,
// 32 affected_in_range.
package check

// SchemaVersion marks the output contract version.
const SchemaVersion = 1

// Exit bits.
const (
	BitFatal           = 1
	BitStale           = 2
	BitDirty           = 4
	BitGaps            = 8 // only under --strict
	BitBindingFailed   = 16
	BitAffectedInRange = 32
)

// Closed value sets.
const (
	ModeAll     = "all"
	ModeDoc     = "doc"
	ModePath    = "path"
	ModeRange   = "range"
	ModeObserve = "observe"

	StatusFresh      = "fresh"
	StatusStale      = "stale"
	StatusDirty      = "dirty"
	StatusUnobserved = "unobserved"
)

// Document is one per-document envelope entry.
type Document struct {
	Doc                string      `json:"doc" yaml:"doc"`
	Status             string      `json:"status,omitempty" yaml:"status,omitempty"`
	TriggeredWatches   []string    `json:"triggered_watches,omitempty" yaml:"triggered_watches,omitempty"`
	ChangedPaths       []string    `json:"changed_paths,omitempty" yaml:"changed_paths,omitempty"`
	IntroducingCommits []string    `json:"introducing_commits,omitempty" yaml:"introducing_commits,omitempty"`
	DocumentChanged    bool        `json:"document_changed,omitempty" yaml:"document_changed,omitempty"`
	AnchorOrphaned     bool        `json:"anchor_orphaned,omitempty" yaml:"anchor_orphaned,omitempty"`
	AnchorRecovered    bool        `json:"anchor_recovered,omitempty" yaml:"anchor_recovered,omitempty"`
	Violations         []Violation `json:"violations,omitempty" yaml:"violations,omitempty"`
	Bindings           []Binding   `json:"bindings,omitempty" yaml:"bindings,omitempty"`
	Hints              []string    `json:"hints,omitempty" yaml:"hints,omitempty"`
	// Observe-only fields.
	ObservedAt string `json:"observed_at,omitempty" yaml:"observed_at,omitempty"`
	Anchor     string `json:"anchor,omitempty" yaml:"anchor,omitempty"`
}

// Violation is a structure-phase failure with its source location (the where
// of G8).
type Violation struct {
	Rule    string `json:"rule" yaml:"rule"`
	File    string `json:"file" yaml:"file"`
	Line    int    `json:"line" yaml:"line"`
	Message string `json:"message" yaml:"message"`
}

// Binding is one binding reference or outcome. Status is exactly
// pending | pass | failed.
type Binding struct {
	Name     string `json:"name" yaml:"name"`
	Status   string `json:"status" yaml:"status"`
	ExitCode int    `json:"exit_code,omitempty" yaml:"exit_code,omitempty"`
	Command  string `json:"command,omitempty" yaml:"command,omitempty"`
	Stderr   string `json:"stderr,omitempty" yaml:"stderr,omitempty"`
}

// Envelope is the machine-readable output contract, identical in json and
// yaml. documents[] is sorted by doc id bytes; so are the store-level arrays.
type Envelope struct {
	Mode            string     `json:"mode" yaml:"mode"`
	Schema          int        `json:"schema" yaml:"schema"`
	ExitBits        int        `json:"exit_bits" yaml:"exit_bits"`
	Documents       []Document `json:"documents" yaml:"documents"`
	Unwatched       []string   `json:"unwatched,omitempty" yaml:"unwatched,omitempty"`
	Unobserved      []string   `json:"unobserved,omitempty" yaml:"unobserved,omitempty"`
	AffectedInRange []string   `json:"affected_in_range,omitempty" yaml:"affected_in_range,omitempty"`
	LockLost        bool       `json:"lock_lost,omitempty" yaml:"lock_lost,omitempty"`
	Hints           []string   `json:"hints,omitempty" yaml:"hints,omitempty"`
	// Observe-only.
	Lock LockReport `json:"lock" yaml:"lock"`
}

// LockReport describes the observe write.
type LockReport struct {
	Changed bool   `json:"changed" yaml:"changed"`
	Path    string `json:"path" yaml:"path"`
}

// Refused is a per-document observe refusal with the precondition that
// failed.
type Refused struct {
	Doc       string   `json:"doc" yaml:"doc"`
	Reason    string   `json:"reason" yaml:"reason"`
	Offending []string `json:"offending,omitempty" yaml:"offending,omitempty"`
	Hints     []string `json:"hints,omitempty" yaml:"hints,omitempty"`
}
