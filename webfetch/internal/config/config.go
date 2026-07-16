package config

import (
	"fmt"
	"time"

	"web-search-backend/runtime/env"
	"web-search-backend/runtime/logging"
)

type Config struct {
	Address            string
	RequestTimeout     time.Duration
	MaxRequestTimeout  time.Duration
	HTTPTimeout        time.Duration
	BrowserEnabled     bool
	BrowserTimeout     time.Duration
	BrowserWait        time.Duration
	BrowserSlots       int
	ChromePath         string
	ChromeProfileDir   string
	ChromeHeadless     bool
	ChromeNoSandbox    bool
	FreshTTL           time.Duration
	StaleTTL           time.Duration
	CacheMaxItems      int
	MaxBodyBytes       int64
	MaxRedirects       int
	HostAllowlist      []string
	CacheBypass        bool
	Diagnostics        bool
	LogStoreURLQuery   bool
	RobotsPolicy       string
	UserAgent          string
	CORSAllowedOrigins []string
	TrustedProxies     []string
	Log                logging.Config
}

func Load(paths ...string) (Config, error) {
	config := Config{
		Address: ":8081", RequestTimeout: 20 * time.Second, MaxRequestTimeout: 60 * time.Second, HTTPTimeout: 6 * time.Second,
		BrowserEnabled: true, BrowserTimeout: 12 * time.Second, BrowserWait: 250 * time.Millisecond,
		BrowserSlots: 4, ChromeProfileDir: "./var/chrome-profile", ChromeHeadless: true,
		FreshTTL: 30 * time.Minute, StaleTTL: 24 * time.Hour, CacheMaxItems: 500,
		MaxBodyBytes: 5 << 20, MaxRedirects: 5, RobotsPolicy: "ignore",
		UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/150 Safari/537.36",
		Log:       logging.Config{Level: "info", File: "./log/webfetch-api.log", MaxSizeMB: 100, MaxBackups: 5, MaxAgeDays: 7, Compress: true},
	}
	if len(paths) > 0 && paths[0] != "" {
		if err := applyYAML(&config, paths[0]); err != nil {
			return Config{}, err
		}
	}
	config.Address = env.String("WEBFETCH_ADDR", config.Address)
	config.ChromePath = env.String("WEBFETCH_CHROME_PATH", config.ChromePath)
	config.ChromeProfileDir = env.String("WEBFETCH_BROWSER_PROFILE_DIR", config.ChromeProfileDir)
	config.RobotsPolicy = env.String("WEBFETCH_ROBOTS_POLICY", config.RobotsPolicy)
	config.UserAgent = env.String("WEBFETCH_USER_AGENT", config.UserAgent)
	config.Log.Level = env.String("WEBFETCH_LOG_LEVEL", config.Log.Level)
	config.Log.File = env.String("WEBFETCH_LOG_FILE", config.Log.File)
	config.HostAllowlist = env.CSV("WEBFETCH_HOST_ALLOWLIST", config.HostAllowlist)
	config.CORSAllowedOrigins = env.CSV("WEBFETCH_CORS_ALLOWED_ORIGINS", config.CORSAllowedOrigins)
	config.TrustedProxies = env.CSV("WEBFETCH_TRUSTED_PROXIES", config.TrustedProxies)
	var err error
	if config.RequestTimeout, err = env.PositiveDuration("WEBFETCH_REQUEST_TIMEOUT", config.RequestTimeout); err != nil {
		return Config{}, err
	}
	if config.MaxRequestTimeout, err = env.PositiveDuration("WEBFETCH_MAX_REQUEST_TIMEOUT", config.MaxRequestTimeout); err != nil {
		return Config{}, err
	}
	if config.HTTPTimeout, err = env.PositiveDuration("WEBFETCH_HTTP_TIMEOUT", config.HTTPTimeout); err != nil {
		return Config{}, err
	}
	if config.BrowserTimeout, err = env.PositiveDuration("WEBFETCH_BROWSER_TIMEOUT", config.BrowserTimeout); err != nil {
		return Config{}, err
	}
	if config.BrowserWait, err = env.PositiveDuration("WEBFETCH_BROWSER_WAIT", config.BrowserWait); err != nil {
		return Config{}, err
	}
	if config.FreshTTL, err = env.PositiveDuration("WEBFETCH_CACHE_FRESH_TTL", config.FreshTTL); err != nil {
		return Config{}, err
	}
	if config.StaleTTL, err = env.PositiveDuration("WEBFETCH_CACHE_STALE_TTL", config.StaleTTL); err != nil {
		return Config{}, err
	}
	if config.BrowserSlots, err = env.PositiveInt("WEBFETCH_BROWSER_SLOTS", config.BrowserSlots); err != nil {
		return Config{}, err
	}
	if config.CacheMaxItems, err = env.PositiveInt("WEBFETCH_CACHE_MAX_ITEMS", config.CacheMaxItems); err != nil {
		return Config{}, err
	}
	if config.MaxRedirects, err = env.PositiveInt("WEBFETCH_MAX_REDIRECTS", config.MaxRedirects); err != nil {
		return Config{}, err
	}
	if config.MaxBodyBytes, err = env.PositiveInt64("WEBFETCH_MAX_BODY_BYTES", config.MaxBodyBytes); err != nil {
		return Config{}, err
	}
	if config.Log.MaxSizeMB, err = env.PositiveInt("WEBFETCH_LOG_MAX_SIZE_MB", config.Log.MaxSizeMB); err != nil {
		return Config{}, err
	}
	if config.Log.MaxBackups, err = env.PositiveInt("WEBFETCH_LOG_MAX_BACKUPS", config.Log.MaxBackups); err != nil {
		return Config{}, err
	}
	if config.Log.MaxAgeDays, err = env.PositiveInt("WEBFETCH_LOG_MAX_AGE_DAYS", config.Log.MaxAgeDays); err != nil {
		return Config{}, err
	}
	if config.BrowserEnabled, err = env.Bool("WEBFETCH_BROWSER_ENABLED", config.BrowserEnabled); err != nil {
		return Config{}, err
	}
	if config.ChromeHeadless, err = env.Bool("WEBFETCH_CHROME_HEADLESS", config.ChromeHeadless); err != nil {
		return Config{}, err
	}
	if config.ChromeNoSandbox, err = env.Bool("WEBFETCH_CHROME_NO_SANDBOX", config.ChromeNoSandbox); err != nil {
		return Config{}, err
	}
	if config.CacheBypass, err = env.Bool("WEBFETCH_CACHE_BYPASS", false); err != nil {
		return Config{}, err
	}
	if config.Diagnostics, err = env.Bool("WEBFETCH_DIAGNOSTICS_ENABLED", false); err != nil {
		return Config{}, err
	}
	if config.LogStoreURLQuery, err = env.Bool("WEBFETCH_LOG_STORE_URL_QUERY", false); err != nil {
		return Config{}, err
	}
	if config.Log.Compress, err = env.Bool("WEBFETCH_LOG_COMPRESS", config.Log.Compress); err != nil {
		return Config{}, err
	}
	if config.RobotsPolicy != "ignore" {
		return Config{}, fmt.Errorf("WEBFETCH_ROBOTS_POLICY=%s is not implemented", config.RobotsPolicy)
	}
	if config.StaleTTL <= config.FreshTTL {
		return Config{}, fmt.Errorf("WEBFETCH_CACHE_STALE_TTL must be greater than WEBFETCH_CACHE_FRESH_TTL")
	}
	if config.RequestTimeout < 100*time.Millisecond || config.RequestTimeout > config.MaxRequestTimeout || config.MaxRequestTimeout > 60*time.Second {
		return Config{}, fmt.Errorf("request timeout configuration must satisfy 100ms <= default <= max <= 60s")
	}
	return config, nil
}
