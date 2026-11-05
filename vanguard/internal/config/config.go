// Package config loads and validates .vanguard/vanguard.yaml — user-owned
// configuration, parsed strictly (fail-closed): duplicate keys, YAML anchors,
// aliases, and merge keys are rejected; unknown keys are rejected.
package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/vanguard-lint/vanguard/internal/duration"
	"github.com/vanguard-lint/vanguard/internal/pathmatch"
	"gopkg.in/yaml.v3"
)

// DefaultClockSkewTolerance binds the future-observed_at integrity guard.
const DefaultClockSkewTolerance = 5 * time.Minute

// DefaultBindingTimeout binds per-binding execution.
const DefaultBindingTimeout = 600 * time.Second

// Config is the decoded vanguard.yaml.
type Config struct {
	Version            int
	Discovery          []string
	TargetPath         string
	Targets            map[string]string // named targets: name -> local path
	ClockSkewTolerance time.Duration
	Bindings           map[string]Binding
	Formats            map[string]string
	//warnings captured at load
	Warnings []string
}

// Binding is one allowlisted verification command. Raw commands live only
// here — documents reference bindings by name.
type Binding struct {
	Cmd     []string
	CWD     string // "" or "target" (default) | "store"
	Timeout time.Duration
}

type rawConfig struct {
	Version   *int `yaml:"version"`
	Discovery *struct {
		Include []string `yaml:"include"`
	} `yaml:"discovery"`
	Target *struct {
		Path string `yaml:"path"`
	} `yaml:"target"`
	Targets            map[string]string     `yaml:"targets"`
	ClockSkewTolerance string                `yaml:"clock_skew_tolerance"`
	Bindings           map[string]rawBinding `yaml:"bindings"`
	Formats            map[string]string     `yaml:"formats"`
}

type rawBinding struct {
	Cmd     []string `yaml:"cmd"`
	CWD     string   `yaml:"cwd"`
	Timeout string   `yaml:"timeout"`
}

// Load reads, decodes, defaults, and validates the config at path.
// A returned error is fatal (exit bit 1); warnings are advisory.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	return Decode(data)
}

// Decode parses config bytes (exposed for tests).
func Decode(data []byte) (*Config, error) {
	var root yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&root); err != nil && err != io.EOF {
		return nil, fmt.Errorf("vanguard.yaml is not valid YAML: %w", err)
	}
	if err := rejectAnchors(&root); err != nil {
		return nil, err
	}
	var raw rawConfig
	strict := yaml.NewDecoder(bytes.NewReader(data))
	strict.KnownFields(true)
	if err := strict.Decode(&raw); err != nil && err != io.EOF {
		return nil, fmt.Errorf("vanguard.yaml: %w", err)
	}

	cfg := &Config{
		ClockSkewTolerance: DefaultClockSkewTolerance,
		Formats: map[string]string{
			"md":   "markdown",
			"json": "json",
			"yaml": "yaml",
		},
	}
	if raw.Version == nil {
		cfg.Warnings = append(cfg.Warnings, "add version: 1 to vanguard.yaml")
		cfg.Version = 1
	} else {
		cfg.Version = *raw.Version
		if cfg.Version != 1 {
			return nil, fmt.Errorf("unsupported config version %d — upgrade vanguard or pin version: 1", cfg.Version)
		}
	}
	if raw.ClockSkewTolerance != "" {
		d, err := duration.ParseTunable(raw.ClockSkewTolerance)
		if err != nil {
			return nil, fmt.Errorf("clock_skew_tolerance: %w", err)
		}
		cfg.ClockSkewTolerance = d
	}
	if raw.Target != nil {
		cfg.TargetPath = raw.Target.Path
	}
	if raw.Targets != nil {
		cfg.Targets = make(map[string]string, len(raw.Targets))
		for name, path := range raw.Targets {
			if err := validateID(name); err != nil {
				return nil, fmt.Errorf("target name %q: %w", name, err)
			}
			if path == "" {
				return nil, fmt.Errorf("target %q: path must not be empty", name)
			}
			cfg.Targets[name] = path
		}
	}
	if raw.Formats != nil {
		for ext, format := range raw.Formats {
			switch format {
			case "markdown", "json", "yaml", "dsl":
			default:
				return nil, fmt.Errorf("formats.%s: unknown format %q (want markdown, json, yaml, or dsl)", ext, format)
			}
			cfg.Formats[ext] = format
		}
	}
	if raw.Discovery != nil && raw.Discovery.Include != nil {
		cfg.Discovery = raw.Discovery.Include
	} else {
		cfg.Discovery = []string{"**/*.md"}
	}
	for _, g := range cfg.Discovery {
		if err := validateDiscoveryGlob(g); err != nil {
			return nil, err
		}
	}
	if raw.Bindings != nil {
		cfg.Bindings = make(map[string]Binding, len(raw.Bindings))
		for name, rb := range raw.Bindings {
			if err := validateID(name); err != nil {
				return nil, fmt.Errorf("binding name %q: %w", name, err)
			}
			if len(rb.Cmd) == 0 {
				return nil, fmt.Errorf("binding %q: cmd must be a non-empty array — raw commands are never free text", name)
			}
			switch rb.CWD {
			case "", "target", "store":
			default:
				return nil, fmt.Errorf("binding %q: cwd must be target or store", name)
			}
			timeout := DefaultBindingTimeout
			if rb.Timeout != "" {
				d, err := duration.ParseTunable(rb.Timeout)
				if err != nil {
					return nil, fmt.Errorf("binding %q timeout: %w", name, err)
				}
				timeout = d
			}
			cfg.Bindings[name] = Binding{Cmd: rb.Cmd, CWD: rb.CWD, Timeout: timeout}
		}
	}
	return cfg, nil
}

// validateDiscoveryGlob keeps discovery inside docs/: ../ escapes are
// rejected, and the glob must compile under the closed syntax.
func validateDiscoveryGlob(g string) error {
	if g == "" {
		return fmt.Errorf("discovery glob must not be empty")
	}
	if g == ".." || len(g) >= 3 && (g[:3] == "../" || contains(g, "/../")) {
		return fmt.Errorf("discovery glob %q must not escape docs/", g)
	}
	_, err := pathmatch.Expand(g)
	if err != nil {
		return fmt.Errorf("discovery glob: %w", err)
	}
	return nil
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// validateID is the shared id grammar: [a-z0-9][a-z0-9._-]*, max 128 chars,
// ASCII only.
func validateID(id string) error {
	if id == "" || len(id) > 128 {
		return fmt.Errorf("must match [a-z0-9][a-z0-9._-]* with max 128 chars")
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		ok := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
			(i > 0 && (c == '.' || c == '_' || c == '-'))
		if !ok {
			return fmt.Errorf("must match [a-z0-9][a-z0-9._-]* with max 128 chars")
		}
	}
	return nil
}

// rejectAnchors walks the node tree and rejects anchors, aliases, and merge
// keys — identity-hiding constructs make "what did this declare"
// unreviewable, and alias expansion is a YAML-bomb hazard.
func rejectAnchors(n *yaml.Node) error {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.AliasNode {
		return fmt.Errorf("vanguard.yaml: YAML aliases are rejected — declare the value inline")
	}
	if n.Anchor != "" {
		return fmt.Errorf("vanguard.yaml: YAML anchors are rejected — declare the value inline")
	}
	if n.Kind == yaml.ScalarNode && n.Tag == "!!merge" {
		return fmt.Errorf("vanguard.yaml: merge keys are rejected")
	}
	for _, c := range n.Content {
		if err := rejectAnchors(c); err != nil {
			return err
		}
	}
	return nil
}
