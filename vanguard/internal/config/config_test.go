package config

import "testing"

func TestDefaults(t *testing.T) {
	cfg, err := Decode([]byte("version: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != 1 {
		t.Fatalf("version = %d", cfg.Version)
	}
	if len(cfg.Discovery) != 1 || cfg.Discovery[0] != "**/*.md" {
		t.Fatalf("default discovery = %v", cfg.Discovery)
	}
	if cfg.ClockSkewTolerance.String() != "5m0s" {
		t.Fatalf("default skew tolerance = %v", cfg.ClockSkewTolerance)
	}
	if cfg.Formats["md"] != "markdown" {
		t.Fatalf("default formats = %v", cfg.Formats)
	}
}

func TestMissingVersionIsAdvisory(t *testing.T) {
	cfg, err := Decode([]byte("discovery:\n  include: [\"specs/*.md\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) == 0 {
		t.Fatal("missing version must warn")
	}
	if cfg.Version != 1 {
		t.Fatalf("missing version assumes 1, got %d", cfg.Version)
	}
}

func TestUnknownMajorVersionFails(t *testing.T) {
	if _, err := Decode([]byte("version: 2\n")); err == nil {
		t.Fatal("version 2 must fail closed")
	}
}

func TestBindings(t *testing.T) {
	cfg, err := Decode([]byte(
		"version: 1\n" +
			"bindings:\n" +
			"  refund-suite:\n" +
			"    cmd: [go, test, ./payments/...]\n" +
			"    cwd: store\n" +
			"    timeout: 30s\n"))
	if err != nil {
		t.Fatal(err)
	}
	b, ok := cfg.Bindings["refund-suite"]
	if !ok {
		t.Fatal("binding missing")
	}
	if len(b.Cmd) != 3 || b.Cmd[0] != "go" || b.Cmd[2] != "./payments/..." {
		t.Fatalf("cmd = %v", b.Cmd)
	}
	if b.CWD != "store" {
		t.Fatalf("cwd = %q", b.CWD)
	}
	if b.Timeout.String() != "30s" {
		t.Fatalf("timeout = %v", b.Timeout)
	}
}

func TestBindingRejections(t *testing.T) {
	cases := map[string]string{
		"empty cmd":     "version: 1\nbindings:\n  x:\n    cmd: []\n",
		"bad cwd":       "version: 1\nbindings:\n  x:\n    cmd: [go]\n    cwd: docs\n",
		"bad name":      "version: 1\nbindings:\n  \"Bad Name\":\n    cmd: [go]\n",
		"bad timeout":   "version: 1\nbindings:\n  x:\n    cmd: [go]\n    timeout: 5minutes\n",
		"unknown field": "version: 1\nbindings:\n  x:\n    cmd: [go]\n    shell: bash\n",
	}
	for name, doc := range cases {
		if _, err := Decode([]byte(doc)); err == nil {
			t.Fatalf("%s: must fail", name)
		}
	}
}

func TestAnchorsAndAliasesRejected(t *testing.T) {
	anchorDoc := "version: 1\ndiscovery: &d\n  include: [\"**/*.md\"]\nother: *d\n"
	// "other" is an unknown key too; the alias check must fire regardless of order.
	if _, err := Decode([]byte(anchorDoc)); err == nil {
		t.Fatal("aliases must be rejected")
	}
}

func TestDiscoveryCannotEscapeDocs(t *testing.T) {
	for _, g := range []string{"../outside/**", "specs/../../etc/**"} {
		if _, err := Decode([]byte("version: 1\ndiscovery:\n  include: [\"" + g + "\"]\n")); err == nil {
			t.Fatalf("discovery glob %q must be rejected", g)
		}
	}
}

func TestUnknownFormatRejected(t *testing.T) {
	if _, err := Decode([]byte("version: 1\nformats:\n  md: prose\n")); err == nil {
		t.Fatal("unknown format must fail")
	}
}

func TestFormatOverridesMerge(t *testing.T) {
	cfg, err := Decode([]byte("version: 1\nformats:\n  mdx: markdown\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Formats["md"] != "markdown" || cfg.Formats["mdx"] != "markdown" {
		t.Fatalf("formats = %v", cfg.Formats)
	}
}

func TestIDGrammar(t *testing.T) {
	good := []string{"a", "payments-spec", "glossary.core", "x_1-y", "0abc"}
	for _, id := range good {
		if err := validateID(id); err != nil {
			t.Fatalf("validateID(%q): %v", id, err)
		}
	}
	bad := []string{"", "_lead", "-lead", ".lead", "Has-Upper", "has space", "a/b", "toolong-" + string(make([]byte, 120))}
	for _, id := range bad {
		if err := validateID(id); err == nil {
			t.Fatalf("validateID(%q) must fail", id)
		}
	}
}
