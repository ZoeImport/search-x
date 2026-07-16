package config

import (
	"fmt"
	"slices"
	"time"

	"web-search-backend/runtime/env"
	"web-search-backend/runtime/logging"
)

type Config struct {
	Address                   string
	TotalTimeout              time.Duration
	MaxRequestTimeout         time.Duration
	CursorSecret              string
	CursorTTL                 time.Duration
	CacheBypass               bool
	AllowRequestProviders     bool
	EnabledProviders          []string
	ProviderVisibility        string
	CORSAllowedOrigins        []string
	Debug                     bool
	DebugDir                  string
	DebugPreviewBytes         int
	LogStoreQuery             bool
	LogQueryPreviewChars      int
	Log                       logging.Config
	ChromePath                string
	ChromeHeadless            bool
	ChromeNoSandbox           bool
	ChromeProfileDir          string
	BingProfileDir            string
	BraveProfileDir           string
	ProviderBrowserSlots      int
	DesktopURL                string
	MobileURL                 string
	DuckDuckGoURL             string
	BingURL                   string
	BraveURL                  string
	UserAgent                 string
	DesktopTimeout            time.Duration
	MobileTimeout             time.Duration
	ChromeTimeout             time.Duration
	DuckDuckGoTimeout         time.Duration
	BingTimeout               time.Duration
	BraveTimeout              time.Duration
	BaiduProfileCount         int
	BaiduProfileCapacity      int
	BingProfileCount          int
	BingProfileCapacity       int
	BraveProfileCount         int
	BraveProfileCapacity      int
	DuckDuckGoProfileCount    int
	DuckDuckGoProfileCapacity int
	AutoRouteWait             time.Duration
	ExplicitRouteWait         time.Duration
	MaxProviderAttempts       int
	MinimumAttemptBudget      time.Duration
	ProfileManifestRoot       string
	GlobalInflightMax         int
	AutoQueueMax              int
	CapacityRouterEnabled     bool
	ProfilePoolEnabled        bool
	FreshTTL                  time.Duration
	StaleTTL                  time.Duration
	ProviderRate              float64
	ProviderBurst             int
	JitterMin                 time.Duration
	JitterMax                 time.Duration
	BaiduSessionMinInterval   time.Duration
	BaiduSessionMaxJitter     time.Duration
	BaiduCaptchaCooldown      time.Duration
	BaiduRateLimitCooldown    time.Duration
	BaiduFallbackReserve      time.Duration
	CacheMaxItems             int
	MaxBodyBytes              int64
}

func Load(paths ...string) (Config, error) {
	config := Config{
		Address: ":8080", TotalTimeout: 20 * time.Second, MaxRequestTimeout: 60 * time.Second, CursorSecret: "local-development-only", CursorTTL: 15 * time.Minute,
		AllowRequestProviders: true, EnabledProviders: []string{"baidu", "bing", "brave", "duckduckgo"}, ProviderVisibility: "public",
		DebugDir: "./var/debug", DebugPreviewBytes: 32 << 10, LogQueryPreviewChars: 32,
		Log:            logging.Config{Level: "info", File: "./log/websearch-api.log", MaxSizeMB: 100, MaxBackups: 5, MaxAgeDays: 7, Compress: true},
		ChromeHeadless: true, ChromeProfileDir: "./var/chrome-profile", BingProfileDir: "./var/chrome-profile-bing", BraveProfileDir: "./var/chrome-profile-brave", ProviderBrowserSlots: 2,
		DesktopURL: "https://www.baidu.com/s", MobileURL: "https://m.baidu.com/s", DuckDuckGoURL: "https://html.duckduckgo.com/html/", BingURL: "https://www.bing.com/search", BraveURL: "https://search.brave.com/search",
		UserAgent:      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/150 Safari/537.36",
		DesktopTimeout: 4 * time.Second, MobileTimeout: 4 * time.Second, ChromeTimeout: 10 * time.Second, DuckDuckGoTimeout: 5 * time.Second, BingTimeout: 10 * time.Second, BraveTimeout: 10 * time.Second,
		BaiduProfileCount: 6, BaiduProfileCapacity: 1, BingProfileCount: 5, BingProfileCapacity: 2, BraveProfileCount: 3, BraveProfileCapacity: 1, DuckDuckGoProfileCount: 4, DuckDuckGoProfileCapacity: 1,
		AutoRouteWait: 2 * time.Second, ExplicitRouteWait: 5 * time.Second, MaxProviderAttempts: 3, MinimumAttemptBudget: 3 * time.Second,
		ProfileManifestRoot: "./var/provider-manifests", GlobalInflightMax: 100, AutoQueueMax: 100, CapacityRouterEnabled: true, ProfilePoolEnabled: true,
		FreshTTL: 15 * time.Minute, StaleTTL: 24 * time.Hour, ProviderRate: 1, ProviderBurst: 3, JitterMin: 200 * time.Millisecond, JitterMax: 800 * time.Millisecond,
		BaiduSessionMinInterval: 3 * time.Second, BaiduSessionMaxJitter: 2 * time.Second, BaiduCaptchaCooldown: 30 * time.Minute, BaiduRateLimitCooldown: 5 * time.Minute, BaiduFallbackReserve: 5 * time.Second,
		CacheMaxItems: 1000, MaxBodyBytes: 4 << 20,
	}
	if len(paths) > 0 && paths[0] != "" {
		if err := applyYAML(&config, paths[0]); err != nil {
			return Config{}, err
		}
	}
	config.Address = env.String("WEBSEARCH_ADDR", config.Address)
	config.CursorSecret = env.String("WEBSEARCH_CURSOR_KEY", config.CursorSecret)
	config.ProviderVisibility = env.String("WEBSEARCH_RESPONSE_PROVIDER_VISIBILITY", config.ProviderVisibility)
	config.DebugDir = env.String("WEBSEARCH_DEBUG_DIR", config.DebugDir)
	config.ChromePath = env.String("WEBSEARCH_CHROME_PATH", config.ChromePath)
	config.ChromeProfileDir = env.String("WEBSEARCH_CHROME_PROFILE_DIR", config.ChromeProfileDir)
	config.BingProfileDir = env.String("WEBSEARCH_BING_PROFILE_DIR", config.BingProfileDir)
	config.BraveProfileDir = env.String("WEBSEARCH_BRAVE_PROFILE_DIR", config.BraveProfileDir)
	config.DesktopURL = env.String("WEBSEARCH_BAIDU_DESKTOP_URL", config.DesktopURL)
	config.MobileURL = env.String("WEBSEARCH_BAIDU_MOBILE_URL", config.MobileURL)
	config.DuckDuckGoURL = env.String("WEBSEARCH_DUCKDUCKGO_URL", config.DuckDuckGoURL)
	config.BingURL = env.String("WEBSEARCH_BING_URL", config.BingURL)
	config.BraveURL = env.String("WEBSEARCH_BRAVE_URL", config.BraveURL)
	config.UserAgent = env.String("WEBSEARCH_USER_AGENT", config.UserAgent)
	config.ProfileManifestRoot = env.String("WEBSEARCH_PROFILE_MANIFEST_ROOT", config.ProfileManifestRoot)
	config.Log.Level = env.String("WEBSEARCH_LOG_LEVEL", config.Log.Level)
	config.Log.File = env.String("WEBSEARCH_LOG_FILE", config.Log.File)
	config.EnabledProviders = env.CSV("WEBSEARCH_ENABLED_PROVIDERS", config.EnabledProviders)
	config.CORSAllowedOrigins = env.CSV("WEBSEARCH_CORS_ALLOWED_ORIGINS", config.CORSAllowedOrigins)
	var err error
	for _, item := range []struct {
		key    string
		target *time.Duration
	}{{"WEBSEARCH_REQUEST_TIMEOUT", &config.TotalTimeout}, {"WEBSEARCH_MAX_REQUEST_TIMEOUT", &config.MaxRequestTimeout}, {"WEBSEARCH_CURSOR_TTL", &config.CursorTTL}, {"WEBSEARCH_DESKTOP_TIMEOUT", &config.DesktopTimeout}, {"WEBSEARCH_MOBILE_TIMEOUT", &config.MobileTimeout}, {"WEBSEARCH_CHROME_TIMEOUT", &config.ChromeTimeout}, {"WEBSEARCH_DUCKDUCKGO_TIMEOUT", &config.DuckDuckGoTimeout}, {"WEBSEARCH_BING_TIMEOUT", &config.BingTimeout}, {"WEBSEARCH_BRAVE_TIMEOUT", &config.BraveTimeout}, {"WEBSEARCH_AUTO_ROUTE_WAIT", &config.AutoRouteWait}, {"WEBSEARCH_EXPLICIT_ROUTE_WAIT", &config.ExplicitRouteWait}, {"WEBSEARCH_MINIMUM_ATTEMPT_BUDGET", &config.MinimumAttemptBudget}, {"WEBSEARCH_CACHE_FRESH_TTL", &config.FreshTTL}, {"WEBSEARCH_CACHE_STALE_TTL", &config.StaleTTL}, {"WEBSEARCH_JITTER_MIN", &config.JitterMin}, {"WEBSEARCH_JITTER_MAX", &config.JitterMax}, {"WEBSEARCH_BAIDU_SESSION_MIN_INTERVAL", &config.BaiduSessionMinInterval}, {"WEBSEARCH_BAIDU_CAPTCHA_COOLDOWN", &config.BaiduCaptchaCooldown}, {"WEBSEARCH_BAIDU_RATE_LIMIT_COOLDOWN", &config.BaiduRateLimitCooldown}} {
		if *item.target, err = env.PositiveDuration(item.key, *item.target); err != nil {
			return Config{}, err
		}
	}
	for _, item := range []struct {
		key    string
		target *time.Duration
	}{{"WEBSEARCH_BAIDU_SESSION_JITTER_MAX", &config.BaiduSessionMaxJitter}, {"WEBSEARCH_BAIDU_FALLBACK_RESERVE", &config.BaiduFallbackReserve}} {
		if *item.target, err = env.NonNegativeDuration(item.key, *item.target); err != nil {
			return Config{}, err
		}
	}
	for _, item := range []struct {
		key    string
		target *int
	}{{"WEBSEARCH_DEBUG_PREVIEW_BYTES", &config.DebugPreviewBytes}, {"WEBSEARCH_LOG_QUERY_PREVIEW_CHARS", &config.LogQueryPreviewChars}, {"WEBSEARCH_LOG_MAX_SIZE_MB", &config.Log.MaxSizeMB}, {"WEBSEARCH_LOG_MAX_BACKUPS", &config.Log.MaxBackups}, {"WEBSEARCH_LOG_MAX_AGE_DAYS", &config.Log.MaxAgeDays}, {"WEBSEARCH_PROVIDER_BROWSER_SLOTS", &config.ProviderBrowserSlots}, {"WEBSEARCH_BAIDU_PROFILE_COUNT", &config.BaiduProfileCount}, {"WEBSEARCH_BAIDU_PROFILE_CAPACITY", &config.BaiduProfileCapacity}, {"WEBSEARCH_BING_PROFILE_COUNT", &config.BingProfileCount}, {"WEBSEARCH_BING_PROFILE_CAPACITY", &config.BingProfileCapacity}, {"WEBSEARCH_BRAVE_PROFILE_COUNT", &config.BraveProfileCount}, {"WEBSEARCH_BRAVE_PROFILE_CAPACITY", &config.BraveProfileCapacity}, {"WEBSEARCH_DUCKDUCKGO_PROFILE_COUNT", &config.DuckDuckGoProfileCount}, {"WEBSEARCH_DUCKDUCKGO_PROFILE_CAPACITY", &config.DuckDuckGoProfileCapacity}, {"WEBSEARCH_MAX_PROVIDER_ATTEMPTS", &config.MaxProviderAttempts}, {"WEBSEARCH_GLOBAL_INFLIGHT_MAX", &config.GlobalInflightMax}, {"WEBSEARCH_AUTO_QUEUE_MAX", &config.AutoQueueMax}, {"WEBSEARCH_PROVIDER_BURST", &config.ProviderBurst}, {"WEBSEARCH_CACHE_MAX_ITEMS", &config.CacheMaxItems}} {
		if *item.target, err = env.PositiveInt(item.key, *item.target); err != nil {
			return Config{}, err
		}
	}
	for _, item := range []struct {
		key    string
		target *bool
	}{{"WEBSEARCH_CACHE_BYPASS", &config.CacheBypass}, {"WEBSEARCH_ALLOW_REQUEST_PROVIDERS", &config.AllowRequestProviders}, {"WEBSEARCH_DIAGNOSTICS_ENABLED", &config.Debug}, {"WEBSEARCH_LOG_STORE_QUERY", &config.LogStoreQuery}, {"WEBSEARCH_LOG_COMPRESS", &config.Log.Compress}, {"WEBSEARCH_CHROME_HEADLESS", &config.ChromeHeadless}, {"WEBSEARCH_CHROME_NO_SANDBOX", &config.ChromeNoSandbox}, {"WEBSEARCH_CAPACITY_ROUTER_ENABLED", &config.CapacityRouterEnabled}, {"WEBSEARCH_PROFILE_POOL_ENABLED", &config.ProfilePoolEnabled}} {
		if *item.target, err = env.Bool(item.key, *item.target); err != nil {
			return Config{}, err
		}
	}
	if config.ProviderRate, err = env.PositiveFloat("WEBSEARCH_PROVIDER_RATE", config.ProviderRate); err != nil {
		return Config{}, err
	}
	if config.MaxBodyBytes, err = env.PositiveInt64("WEBSEARCH_MAX_BODY_BYTES", config.MaxBodyBytes); err != nil {
		return Config{}, err
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) Validate() error {
	if config.ProviderVisibility != "hidden" && config.ProviderVisibility != "public" {
		return fmt.Errorf("WEBSEARCH_RESPONSE_PROVIDER_VISIBILITY must be hidden or public")
	}
	if config.TotalTimeout < 100*time.Millisecond || config.TotalTimeout > config.MaxRequestTimeout || config.MaxRequestTimeout > 60*time.Second {
		return fmt.Errorf("request timeout configuration must satisfy 100ms <= default <= max <= 60s")
	}
	if config.StaleTTL <= config.FreshTTL {
		return fmt.Errorf("WEBSEARCH_CACHE_STALE_TTL must be greater than WEBSEARCH_CACHE_FRESH_TTL")
	}
	if config.JitterMax < config.JitterMin {
		return fmt.Errorf("WEBSEARCH_JITTER_MAX must be greater than or equal to WEBSEARCH_JITTER_MIN")
	}
	valid := []string{"baidu", "bing", "brave", "duckduckgo"}
	if len(config.EnabledProviders) == 0 {
		return fmt.Errorf("WEBSEARCH_ENABLED_PROVIDERS must not be empty")
	}
	seen := make(map[string]struct{}, len(config.EnabledProviders))
	for _, provider := range config.EnabledProviders {
		if !slices.Contains(valid, provider) {
			return fmt.Errorf("unsupported enabled provider %q", provider)
		}
		if _, exists := seen[provider]; exists {
			return fmt.Errorf("enabled provider %q is duplicated", provider)
		}
		seen[provider] = struct{}{}
	}
	return nil
}
