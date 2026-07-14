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
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("SEARCH_DEBUG", "false")
	t.Setenv("SEARCH_DEBUG_TOKEN", "local-debug-token")
	t.Setenv("SEARCH_ADDR", "127.0.0.1:9090")
	t.Setenv("SEARCH_PROVIDER_RATE", "0.5")
	t.Setenv("SEARCH_TRUSTED_PROXIES", "10.0.0.0/8,192.168.0.0/16")
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Debug || got.DebugToken != "local-debug-token" || got.Address != "127.0.0.1:9090" || got.ProviderRate != 0.5 || len(got.TrustedProxies) != 2 {
		t.Fatalf("config=%#v", got)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	for _, test := range []struct{ key, value, want string }{
		{"SEARCH_TOTAL_TIMEOUT", "bad", "SEARCH_TOTAL_TIMEOUT"},
		{"SEARCH_PROVIDER_BURST", "0", "SEARCH_PROVIDER_BURST"},
		{"SEARCH_DEBUG", "sometimes", "SEARCH_DEBUG"},
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
