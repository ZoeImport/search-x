package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProviderControls(t *testing.T) {
	t.Setenv("WEBSEARCH_ENABLED_PROVIDERS", "bing,brave")
	t.Setenv("WEBSEARCH_ALLOW_REQUEST_PROVIDERS", "true")
	t.Setenv("WEBSEARCH_RESPONSE_PROVIDER_VISIBILITY", "public")
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !config.AllowRequestProviders || config.ProviderVisibility != "public" || len(config.EnabledProviders) != 2 {
		t.Fatalf("unexpected config %#v", config)
	}
}

func TestLoadYAMLAndEnvironmentPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  address: ':19080'\napi:\n  enabled_providers: [bing, baidu]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEBSEARCH_ADDR", ":29080")
	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Address != ":29080" || config.EnabledProviders[0] != "bing" {
		t.Fatalf("config=%#v", config)
	}
}

func TestLoadYAMLRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  unknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown YAML field error")
	}
}

func TestLoadYAMLCoversAdvancedRuntimeSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := `browser:
  chrome_timeout: 9s
routing:
  jitter_min: 100ms
  jitter_max: 300ms
  profile_manifest_root: ./manifests
providers:
  baidu:
    mobile_timeout: 3s
    session_min_interval: 2s
    session_jitter_max: 0s
    captcha_cooldown: 10m
    rate_limit_cooldown: 2m
    fallback_reserve: 0s
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.ChromeTimeout.String() != "9s" || config.MobileTimeout.String() != "3s" || config.JitterMin.String() != "100ms" || config.BaiduSessionMaxJitter != 0 || config.BaiduFallbackReserve != 0 || config.ProfileManifestRoot != "./manifests" {
		t.Fatalf("config=%#v", config)
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
