package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRejectsUnimplementedRobotsPolicy(t *testing.T) {
	t.Setenv("WEBFETCH_ROBOTS_POLICY", "respect")
	if _, err := Load(); err == nil {
		t.Fatal("expected unimplemented robots policy error")
	}
}

func TestLoadYAMLAndEnvironmentPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  address: ':19081'\nhttp:\n  max_redirects: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEBFETCH_ADDR", ":29081")
	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Address != ":29081" || config.MaxRedirects != 3 {
		t.Fatalf("config=%#v", config)
	}
}

func TestLoadYAMLRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("browser:\n  unknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown YAML field error")
	}
}

func TestLoadYAMLRejectsMultipleDocuments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server: {}\n---\nserver: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected multiple YAML document error")
	}
}
