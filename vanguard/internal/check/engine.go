package check

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/vanguard-lint/vanguard/internal/config"
	"github.com/vanguard-lint/vanguard/internal/docmodel"
	"github.com/vanguard-lint/vanguard/internal/hashnorm"
	"github.com/vanguard-lint/vanguard/internal/lockfile"
	"github.com/vanguard-lint/vanguard/internal/provider"
)

// Engine wires the store: config, discovery, lock, and provider.
type Engine struct {
	StoreDir  string // path to .vanguard
	DocsRoot  string // path to .vanguard/docs
	TargetDir string // target root (watch globs resolve here)
	Cfg       *config.Config
	Docs      []docmodel.Doc
	Warnings  []docmodel.Warning
	Lock      *lockfile.Lock
	LockPath  string
	LockRaw   []byte
	LockSeen  bool // the lockfile exists on disk
	Prov      provider.Provider
	Now       time.Time
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
	if p, err := provider.OpenGit(e.TargetDir, storeName); err == nil {
		e.Prov = p
	} else {
		e.Prov = provider.OpenHash(e.TargetDir, storeName)
	}

	docs, warnings, err := docmodel.Discover(e.DocsRoot, cfg.Discovery, cfg.Formats)
	if err != nil {
		return nil, err
	}
	e.Docs = docs
	e.Warnings = warnings

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
	}
	return e, nil
}

// ContentHash returns the normalized full-document hash (frontmatter
// included — narrowing watches must force a re-observe).
func (e *Engine) ContentHash(d *docmodel.Doc) string {
	return hashnorm.Sum(d.Raw)
}

// lockLost reports the lost-lock signature: lockfile absent while every
// discovered document reports unobserved. Judged over the full discovery
// set; all mode only.
func (e *Engine) lockLost(unobservedCount int) bool {
	return !e.LockSeen && len(e.Docs) > 0 && unobservedCount == len(e.Docs)
}

func docByID(docs []docmodel.Doc, id string) *docmodel.Doc {
	for i := range docs {
		if docs[i].ID == id {
			return &docs[i]
		}
	}
	return nil
}

func sortedIDs(m map[string]lockfile.Entry) []string {
	out := make([]string, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	sortStrings(out)
	return out
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
