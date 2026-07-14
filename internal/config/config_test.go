package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	for _, key := range knownEnvironmentVariables() {
		t.Setenv(key, "")
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Address != ":8080" || !got.Debug || got.FreshTTL != 15*time.Minute || got.StaleTTL != 24*time.Hour {
		t.Fatalf("config=%#v", got)
	}
	if got.ProviderRate != 1 || got.ProviderBurst != 3 || got.ClientRate != 5 || got.ClientBurst != 10 {
		t.Fatalf("rates=%#v", got)
	}
	if got.DuckDuckGoURL != "https://html.duckduckgo.com/html/" || got.DuckDuckGoTimeout != 5*time.Second {
		t.Fatalf("duckduckgo=%#v", got)
	}
	if got.BingURL != "https://www.bing.com/search" || got.BingTimeout != 10*time.Second || got.BingProfileDir != "./var/chrome-profile-bing" {
		t.Fatalf("bing=%#v", got)
	}
	if !got.ReadEnabled || !got.ReadBrowserEnabled || got.ReadHTTPTimeout != 6*time.Second || got.ReadBrowserTimeout != 12*time.Second {
		t.Fatalf("read switches=%#v", got)
	}
	if got.ReadBrowserWait != 2*time.Second || got.ReadChromeProfileDir != "./var/chrome-profile-read" {
		t.Fatalf("read browser=%#v", got)
	}
	if got.ReadFreshTTL != 30*time.Minute || got.ReadStaleTTL != 24*time.Hour || got.ReadMaxBodyBytes != 5<<20 || got.ReadMaxRedirects != 5 {
		t.Fatalf("read limits=%#v", got)
	}
	if got.ReadCacheMaxItems != 500 {
		t.Fatalf("read runtime=%#v", got)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("SEARCH_DEBUG", "false")
	t.Setenv("SEARCH_DEBUG_TOKEN", "local-debug-token")
	t.Setenv("SEARCH_ADDR", "127.0.0.1:9090")
	t.Setenv("SEARCH_PROVIDER_RATE", "0.5")
	t.Setenv("SEARCH_TRUSTED_PROXIES", "10.0.0.0/8,192.168.0.0/16")
	t.Setenv("SEARCH_READ_HOST_ALLOWLIST", "demo.internal, docs.internal")
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Debug || got.DebugToken != "local-debug-token" || got.Address != "127.0.0.1:9090" || got.ProviderRate != 0.5 || len(got.TrustedProxies) != 2 {
		t.Fatalf("config=%#v", got)
	}
	if len(got.ReadHostAllowlist) != 2 || got.ReadHostAllowlist[1] != "docs.internal" {
		t.Fatalf("read config=%#v", got)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	for _, test := range []struct{ key, value, want string }{
		{"SEARCH_TOTAL_TIMEOUT", "bad", "SEARCH_TOTAL_TIMEOUT"},
		{"SEARCH_PROVIDER_BURST", "0", "SEARCH_PROVIDER_BURST"},
		{"SEARCH_DEBUG", "sometimes", "SEARCH_DEBUG"},
		{"SEARCH_READ_MAX_REDIRECTS", "0", "SEARCH_READ_MAX_REDIRECTS"},
	} {
		t.Run(test.key, func(t *testing.T) {
			for _, key := range knownEnvironmentVariables() {
				t.Setenv(key, "")
			}
			t.Setenv(test.key, test.value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
