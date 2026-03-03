package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFrom(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `environments:
  fx-ci:
    project_id: "test-project"
    cluster: "test-cluster"
    root_url: "https://test.example.com"
    key_path: "~/keys/test.json"
  staging:
    project_id: "staging-project"
    cluster: "staging-cluster"
    root_url: "https://staging.example.com"
    key_path: "/absolute/path/key.json"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	if len(cfg.Environments) != 2 {
		t.Fatalf("expected 2 environments, got %d", len(cfg.Environments))
	}

	fxci := cfg.Environments["fx-ci"]
	if fxci.ProjectID != "test-project" {
		t.Errorf("expected project_id=test-project, got %s", fxci.ProjectID)
	}
	if fxci.Cluster != "test-cluster" {
		t.Errorf("expected cluster=test-cluster, got %s", fxci.Cluster)
	}
	if fxci.RootURL != "https://test.example.com" {
		t.Errorf("expected root_url=https://test.example.com, got %s", fxci.RootURL)
	}

	// key_path with ~ should be expanded
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, "keys/test.json")
	if fxci.KeyPath != expected {
		t.Errorf("expected key_path=%s, got %s", expected, fxci.KeyPath)
	}

	// absolute key_path should not be changed
	staging := cfg.Environments["staging"]
	if staging.KeyPath != "/absolute/path/key.json" {
		t.Errorf("expected absolute key_path unchanged, got %s", staging.KeyPath)
	}
}

func TestLoadFromMissing(t *testing.T) {
	_, err := LoadFrom("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestEnvNames(t *testing.T) {
	cfg := &Config{
		Environments: map[string]Environment{
			"beta":  {},
			"alpha": {},
			"gamma": {},
		},
	}
	names := cfg.EnvNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "alpha" || names[1] != "beta" || names[2] != "gamma" {
		t.Errorf("expected sorted names, got %v", names)
	}
}

func TestUniqueRootURLs(t *testing.T) {
	cfg := &Config{
		Environments: map[string]Environment{
			"a": {RootURL: "https://one.com"},
			"b": {RootURL: "https://two.com"},
			"c": {RootURL: "https://one.com"}, // duplicate
		},
	}
	urls := cfg.UniqueRootURLs()
	if len(urls) != 2 {
		t.Fatalf("expected 2 unique URLs, got %d", len(urls))
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	tests := []struct {
		input    string
		expected string
	}{
		{"~/foo/bar", filepath.Join(home, "foo/bar")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"~", "~"}, // just ~ without / is not expanded
	}
	for _, tt := range tests {
		got := expandHome(tt.input)
		if got != tt.expected {
			t.Errorf("expandHome(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
