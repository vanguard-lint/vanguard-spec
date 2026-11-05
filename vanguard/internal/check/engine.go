package check

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/vanguard-lint/vanguard/internal/config"
	"github.com/vanguard-lint/vanguard/internal/docmodel"
	"github.com/vanguard-lint/vanguard/internal/hashnorm"
	"github.com/vanguard-lint/vanguard/internal/lockfile"
	"github.com/vanguard-lint/vanguard/internal/provider"
	"github.com/vanguard-lint/vanguard/internal/structure"
)

// Engine wires the store: config, discovery, lock, and providers.
type Engine struct {
	StoreDir  string // path to .vanguard
	DocsRoot  string // path to .vanguard/docs
	TargetDir string // primary target root (watch globs resolve here)
	Cfg       *config.Config
	Docs      []docmodel.Doc
	Warnings  []docmodel.Warning
	Lock      *lockfile.Lock
	LockPath  string
	LockRaw   []byte
	LockSeen  bool // the lockfile exists on disk
	Prov      provider.Provider
	// StoreProv is the repository that contains the store — opened at the
	// store's parent. It answers the document-side observe preconditions
	// (committed, clean); nil when the store sits outside any repository.
	StoreProv provider.Provider
	// targetProviders holds one provider per named target (0.4.0); the
	// primary target stays in Prov. Target names are validated at load:
	// documents and lock entries can only reference configured names.
	targetProviders map[string]provider.Provider
	targetDirs      map[string]string
	Now             time.Time
	// StructureViolations maps a document id to the findings of the schema
	// it declared. Empty when every declared schema is satisfied.
	StructureViolations map[string][]structure.Violation
}

// Options tune one engine run.
type Options struct {
	Now             time.Time
	MaxRecoveryWalk int
}

// Load reads the store at storeDir. Fatal problems (missing config, corrupt
// lock, malformed documents) return an error — the fatal bit.
func Load(storeDir string, opts Options) (*Engine, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	if opts.MaxRecoveryWalk == 0 {
		opts.MaxRecoveryWalk = 1000
	}
	e := &Engine{
		StoreDir: storeDir,
		DocsRoot: filepath.Join(storeDir, "docs"),
		Now:      opts.Now,
	}

	cfg, err := config.Load(filepath.Join(storeDir, "vanguard.yaml"))
	if err != nil {
		return nil, err
	}
	e.Cfg = cfg

	// Target resolution: config target.path (relative to the store's
	// parent) → the store's containing directory.
	e.TargetDir = filepath.Dir(storeDir)
	if cfg.TargetPath != "" {
		if filepath.IsAbs(cfg.TargetPath) {
			e.TargetDir = cfg.TargetPath
		} else {
			e.TargetDir = filepath.Join(filepath.Dir(storeDir), cfg.TargetPath)
		}
	}

	storeName := filepath.Base(storeDir)
	storeRoot := filepath.Dir(storeDir)
	if p, err := provider.OpenGit(e.TargetDir, storeName); err == nil {
		e.Prov = p
	} else {
		e.Prov = provider.OpenHash(e.TargetDir, storeName)
	}

	// Named targets (0.4.0): one provider per declared target. A declared
	// target pointing at nothing is a config error — never a silent fall
	// into hash mode over an empty root.
	if len(cfg.Targets) > 0 {
		e.targetProviders = make(map[string]provider.Provider, len(cfg.Targets))
		e.targetDirs = make(map[string]string, len(cfg.Targets))
		for name, tpath := range cfg.Targets {
			dir := tpath
			if !filepath.IsAbs(tpath) {
				dir = filepath.Join(storeRoot, tpath)
			}
			if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
				return nil, fmt.Errorf("target %q: path %s does not exist or is not a directory — a declared target pointing at nothing is a config error", name, tpath)
			}
			e.targetDirs[name] = dir
			if p, err := provider.OpenGit(dir, storeName); err == nil {
				e.targetProviders[name] = p
			} else {
				e.targetProviders[name] = provider.OpenHash(dir, storeName)
			}
		}
	}

	// Store provider: the repository that contains the store, rooted
	// exactly at the store's parent (no upward detection — same rule as
	// targets). Nil when the store sits outside any repository; the
	// document-committed precondition is then skipped, as in 0.3.0.
	if sp, err := provider.OpenGit(storeRoot, storeName); err == nil {
		e.StoreProv = sp
	}

	docs, warnings, err := docmodel.Discover(e.DocsRoot, cfg.Discovery, cfg.Formats)
	if err != nil {
		return nil, err
	}
	e.Docs = docs
	e.Warnings = warnings

	// Target binding validation: a document naming an unknown target would
	// attest against nothing — fatal at discovery, fail-closed.
	for i := range e.Docs {
		t := e.Docs[i].Target
		if t != "" && len(cfg.Targets) == 0 {
			return nil, fmt.Errorf("document %q names target %q — vanguard.yaml declares no targets: map", e.Docs[i].ID, t)
		}
		if t != "" {
			if _, ok := cfg.Targets[t]; !ok {
				return nil, fmt.Errorf("document %q names unknown target %q — declared targets: %v", e.Docs[i].ID, t, sortedTargetNames(cfg.Targets))
			}
		}
	}

	// Structure enforcement: every document declares its binding. A missing
	// `schema:` field is fatal — silence would be the lie G3 forbids. The
	// literal value `none` is the explicit opt-out; any other name must
	// resolve to .vanguard/schemas/<name>.schema.json, and a missing or
	// corrupt schema file is fatal here — load-time is the fail-closed gate.
	structureViolations := map[string][]structure.Violation{}
	schemasDir := filepath.Join(storeDir, "schemas")
	profiles := map[string]*structure.Profile{}
	for i := range e.Docs {
		d := &e.Docs[i]
		if d.Schema == "" {
			return nil, fmt.Errorf("document %q (%s) declares no schema — every store document must bind one; declare `schema: none` to opt out explicitly", d.ID, d.RelPath)
		}
		if d.Schema == "none" {
			continue
		}
		prof, cached := profiles[d.Schema]
		if !cached {
			prof, err = structure.Load(schemasDir, d.Schema)
			if err != nil {
				return nil, err
			}
			profiles[d.Schema] = prof
		}
		if v := structure.Validate(d.RelPath, d.Raw, prof, d.Format); len(v) > 0 {
			structureViolations[d.ID] = v
		}
	}
	e.StructureViolations = structureViolations

	e.LockPath = filepath.Join(storeDir, "vanguard.lock")
	lock, raw, err := lockfile.Load(e.LockPath)
	if err != nil {
		if os.IsNotExist(err) {
			e.Lock = &lockfile.Lock{Version: lockfile.Version, Docs: map[string]lockfile.Entry{}}
			e.LockSeen = false
		} else {
			return nil, err // corrupt lock: fatal, fails closed
		}
	} else {
		e.Lock = lock
		e.LockRaw = raw
		e.LockSeen = true
		// A lock entry naming an unconfigured target means the lock outlived
		// a target rename — fatal, never a silent mis-attestation.
		for id, entry := range e.Lock.Docs {
			if entry.Target == "" {
				continue
			}
			if _, ok := cfg.Targets[entry.Target]; !ok {
				return nil, fmt.Errorf("lock entry %q names unknown target %q — a target rename invalidates the lock; re-observe after fixing vanguard.yaml", id, entry.Target)
			}
		}
	}
	return e, nil
}

// ProvFor returns the provider of one document's target: the primary when
// the document binds no name. Names are load-validated — a miss is an
// internal error, not a user-facing state.
func (e *Engine) ProvFor(target string) provider.Provider {
	if target == "" {
		return e.Prov
	}
	return e.targetProviders[target]
}

// TargetDirFor returns the filesystem root of one target by name.
func (e *Engine) TargetDirFor(target string) string {
	if target == "" {
		return e.TargetDir
	}
	if dir, ok := e.targetDirs[target]; ok {
		return dir
	}
	return e.TargetDir
}

func sortedTargetNames(m map[string]string) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ContentHash returns the normalized full-document hash (frontmatter
// included — narrowing watches must force a re-observe).
func (e *Engine) ContentHash(d *docmodel.Doc) string {
	return hashnorm.Sum(d.Raw)
}

// lockLost reports the lost-lock signature: lockfile absent while every
// discovered document reports unobserved. Unwatched documents carry no
// attestation to lose — with the lock absent they are unobserved too — so
// they count toward the signature. Judged over the full discovery set;
// all mode only.
func (e *Engine) lockLost(unobservedCount, unwatchedCount int) bool {
	return !e.LockSeen && len(e.Docs) > 0 && unobservedCount+unwatchedCount == len(e.Docs)
}

func docByID(docs []docmodel.Doc, id string) *docmodel.Doc {
	for i := range docs {
		if docs[i].ID == id {
			return &docs[i]
		}
	}
	return nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func fmtHint(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
