package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains validated search, read, cache, and transport settings.
type Config struct {
	Address           string
	Debug             bool
	DebugToken        string
	DebugDir          string
	DebugPreviewBytes int
	ChromeProfileDir  string
	// BingProfileDir stores Bing's isolated browser profile.
	BingProfileDir string
	// BraveProfileDir stores Brave's isolated browser profile.
	BraveProfileDir string
	ChromePath      string
	ChromeHeadless  bool
	ChromeNoSandbox bool
	DesktopURL      string
	MobileURL       string
	// DuckDuckGoURL is the DuckDuckGo HTML search endpoint.
	DuckDuckGoURL string
	// BingURL is the Bing search endpoint used by the browser transport.
	BingURL string
	// BraveURL is the Brave search endpoint used by the browser transport.
	BraveURL       string
	UserAgent      string
	TotalTimeout   time.Duration
	DesktopTimeout time.Duration
	MobileTimeout  time.Duration
	ChromeTimeout  time.Duration
	// DuckDuckGoTimeout limits one DuckDuckGo HTTP request.
	DuckDuckGoTimeout time.Duration
	// BingTimeout limits one Bing browser request.
	BingTimeout time.Duration
	// BraveTimeout limits one Brave browser request.
	BraveTimeout time.Duration
	// ProviderBrowserSlots bounds tabs per search browser client.
	ProviderBrowserSlots int
	FreshTTL             time.Duration
	StaleTTL             time.Duration
	ProviderRate         float64
	ProviderBurst        int
	JitterMin            time.Duration
	JitterMax            time.Duration
	ClientRate           float64
	ClientBurst          int
	CacheMaxItems        int
	MaxBodyBytes         int64
	TrustedProxies       []string
	// ReadEnabled controls registration of the read API.
	ReadEnabled bool
	// ReadHTTPTimeout limits the direct HTTP read stage.
	ReadHTTPTimeout time.Duration
	// ReadBrowserEnabled enables browser rendering after eligible HTTP read failures.
	ReadBrowserEnabled bool
	// ReadBrowserTimeout limits one browser-rendered read attempt.
	ReadBrowserTimeout time.Duration
	// ReadBrowserWait allows page scripts to replace an initial JavaScript shell.
	ReadBrowserWait time.Duration
	// ReadBrowserSlots bounds simultaneous content-reader browser tabs.
	ReadBrowserSlots int
	// ReadChromeProfileDir stores the isolated content-reader browser profile.
	ReadChromeProfileDir string
	// ReadFreshTTL controls fresh read-document cache entries.
	ReadFreshTTL time.Duration
	// ReadStaleTTL controls stale read-document cache entries.
	ReadStaleTTL time.Duration
	// ReadCacheMaxItems bounds the read-document memory cache.
	ReadCacheMaxItems int
	// ReadMaxBodyBytes bounds decompressed resource bodies.
	ReadMaxBodyBytes int64
	// ReadMaxRedirects bounds HTTP redirect hops.
	ReadMaxRedirects int
	// ReadHostAllowlist permits named local hosts without disabling SSRF checks globally.
	ReadHostAllowlist []string
}

// Load reads environment overrides and rejects invalid configuration.
func Load() (Config, error) {
	config := Config{
		Address:              ":8080",
		Debug:                true,
		DebugDir:             "./var/debug",
		DebugPreviewBytes:    32 * 1024,
		ChromeProfileDir:     "./var/chrome-profile",
		BingProfileDir:       "./var/chrome-profile-bing",
		BraveProfileDir:      "./var/chrome-profile-brave",
		ChromeHeadless:       true,
		DesktopURL:           "https://www.baidu.com/s",
		MobileURL:            "https://m.baidu.com/s",
		DuckDuckGoURL:        "https://html.duckduckgo.com/html/",
		BingURL:              "https://www.bing.com/search",
		BraveURL:             "https://search.brave.com/search",
		UserAgent:            "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0 Safari/537.36",
		TotalTimeout:         20 * time.Second,
		DesktopTimeout:       4 * time.Second,
		MobileTimeout:        4 * time.Second,
		ChromeTimeout:        10 * time.Second,
		DuckDuckGoTimeout:    5 * time.Second,
		BingTimeout:          10 * time.Second,
		BraveTimeout:         10 * time.Second,
		ProviderBrowserSlots: 2,
		FreshTTL:             15 * time.Minute,
		StaleTTL:             24 * time.Hour,
		ProviderRate:         1,
		ProviderBurst:        3,
		JitterMin:            200 * time.Millisecond,
		JitterMax:            800 * time.Millisecond,
		ClientRate:           5,
		ClientBurst:          10,
		CacheMaxItems:        1000,
		MaxBodyBytes:         4 << 20,
		ReadEnabled:          true,
		ReadHTTPTimeout:      6 * time.Second,
		ReadBrowserEnabled:   true,
		ReadBrowserTimeout:   12 * time.Second,
		ReadBrowserWait:      2 * time.Second,
		ReadBrowserSlots:     3,
		ReadChromeProfileDir: "./var/chrome-profile-read",
		ReadFreshTTL:         30 * time.Minute,
		ReadStaleTTL:         24 * time.Hour,
		ReadCacheMaxItems:    500,
		ReadMaxBodyBytes:     5 << 20,
		ReadMaxRedirects:     5,
	}

	stringValues := []struct {
		key    string
		target *string
	}{
		{"SEARCH_ADDR", &config.Address},
		{"SEARCH_DEBUG_TOKEN", &config.DebugToken},
		{"SEARCH_DEBUG_DIR", &config.DebugDir},
		{"SEARCH_CHROME_PROFILE_DIR", &config.ChromeProfileDir},
		{"SEARCH_BING_PROFILE_DIR", &config.BingProfileDir},
		{"SEARCH_BRAVE_PROFILE_DIR", &config.BraveProfileDir},
		{"SEARCH_CHROME_PATH", &config.ChromePath},
		{"SEARCH_READ_CHROME_PROFILE_DIR", &config.ReadChromeProfileDir},
		{"SEARCH_DESKTOP_URL", &config.DesktopURL},
		{"SEARCH_MOBILE_URL", &config.MobileURL},
		{"SEARCH_DUCKDUCKGO_URL", &config.DuckDuckGoURL},
		{"SEARCH_BING_URL", &config.BingURL},
		{"SEARCH_BRAVE_URL", &config.BraveURL},
		{"SEARCH_USER_AGENT", &config.UserAgent},
	}
	for _, item := range stringValues {
		if value := os.Getenv(item.key); value != "" {
			*item.target = strings.TrimSpace(value)
		}
	}

	for _, item := range []struct {
		key    string
		target *bool
	}{
		{"SEARCH_DEBUG", &config.Debug},
		{"SEARCH_CHROME_HEADLESS", &config.ChromeHeadless},
		{"SEARCH_CHROME_NO_SANDBOX", &config.ChromeNoSandbox},
		{"SEARCH_READ_ENABLED", &config.ReadEnabled},
		{"SEARCH_READ_BROWSER_ENABLED", &config.ReadBrowserEnabled},
	} {
		if err := parseBoolEnv(item.key, item.target); err != nil {
			return Config{}, err
		}
	}

	for _, item := range []struct {
		key    string
		target *time.Duration
	}{
		{"SEARCH_TOTAL_TIMEOUT", &config.TotalTimeout},
		{"SEARCH_DESKTOP_TIMEOUT", &config.DesktopTimeout},
		{"SEARCH_MOBILE_TIMEOUT", &config.MobileTimeout},
		{"SEARCH_CHROME_TIMEOUT", &config.ChromeTimeout},
		{"SEARCH_DUCKDUCKGO_TIMEOUT", &config.DuckDuckGoTimeout},
		{"SEARCH_BING_TIMEOUT", &config.BingTimeout},
		{"SEARCH_BRAVE_TIMEOUT", &config.BraveTimeout},
		{"SEARCH_READ_HTTP_TIMEOUT", &config.ReadHTTPTimeout},
		{"SEARCH_READ_BROWSER_TIMEOUT", &config.ReadBrowserTimeout},
		{"SEARCH_READ_BROWSER_WAIT", &config.ReadBrowserWait},
		{"SEARCH_READ_FRESH_TTL", &config.ReadFreshTTL},
		{"SEARCH_READ_STALE_TTL", &config.ReadStaleTTL},
		{"SEARCH_FRESH_TTL", &config.FreshTTL},
		{"SEARCH_STALE_TTL", &config.StaleTTL},
		{"SEARCH_JITTER_MIN", &config.JitterMin},
		{"SEARCH_JITTER_MAX", &config.JitterMax},
	} {
		if err := parseDurationEnv(item.key, item.target); err != nil {
			return Config{}, err
		}
	}

	for _, item := range []struct {
		key    string
		target *float64
	}{
		{"SEARCH_PROVIDER_RATE", &config.ProviderRate},
		{"SEARCH_CLIENT_RATE", &config.ClientRate},
	} {
		if err := parsePositiveFloatEnv(item.key, item.target); err != nil {
			return Config{}, err
		}
	}

	for _, item := range []struct {
		key    string
		target *int
	}{
		{"SEARCH_PROVIDER_BURST", &config.ProviderBurst},
		{"SEARCH_CLIENT_BURST", &config.ClientBurst},
		{"SEARCH_CACHE_MAX_ITEMS", &config.CacheMaxItems},
		{"SEARCH_DEBUG_PREVIEW_BYTES", &config.DebugPreviewBytes},
		{"SEARCH_READ_CACHE_MAX_ITEMS", &config.ReadCacheMaxItems},
		{"SEARCH_READ_MAX_REDIRECTS", &config.ReadMaxRedirects},
		{"SEARCH_PROVIDER_BROWSER_SLOTS", &config.ProviderBrowserSlots},
		{"SEARCH_READ_BROWSER_SLOTS", &config.ReadBrowserSlots},
	} {
		if err := parsePositiveIntEnv(item.key, item.target); err != nil {
			return Config{}, err
		}
	}
	if value := os.Getenv("SEARCH_MAX_BODY_BYTES"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("SEARCH_MAX_BODY_BYTES must be a positive integer: %q", value)
		}
		config.MaxBodyBytes = parsed
	}
	if value := os.Getenv("SEARCH_READ_MAX_BODY_BYTES"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("SEARCH_READ_MAX_BODY_BYTES must be a positive integer: %q", value)
		}
		config.ReadMaxBodyBytes = parsed
	}
	if value := os.Getenv("SEARCH_TRUSTED_PROXIES"); value != "" {
		for _, proxy := range strings.Split(value, ",") {
			if proxy = strings.TrimSpace(proxy); proxy != "" {
				config.TrustedProxies = append(config.TrustedProxies, proxy)
			}
		}
	}
	if value := os.Getenv("SEARCH_READ_HOST_ALLOWLIST"); value != "" {
		for _, host := range strings.Split(value, ",") {
			if host = strings.ToLower(strings.TrimSpace(host)); host != "" {
				config.ReadHostAllowlist = append(config.ReadHostAllowlist, host)
			}
		}
	}

	if config.Address == "" || config.DebugDir == "" || config.ChromeProfileDir == "" || config.BingProfileDir == "" || config.BraveProfileDir == "" || config.ReadChromeProfileDir == "" {
		return Config{}, fmt.Errorf("search address, debug directory, and browser profiles must not be empty")
	}
	if config.StaleTTL <= config.FreshTTL {
		return Config{}, fmt.Errorf("SEARCH_STALE_TTL must be greater than SEARCH_FRESH_TTL")
	}
	if config.JitterMax < config.JitterMin {
		return Config{}, fmt.Errorf("SEARCH_JITTER_MAX must be greater than or equal to SEARCH_JITTER_MIN")
	}
	if config.ReadStaleTTL <= config.ReadFreshTTL {
		return Config{}, fmt.Errorf("SEARCH_READ_STALE_TTL must be greater than SEARCH_READ_FRESH_TTL")
	}
	return config, nil
}

func parseBoolEnv(key string, target *bool) error {
	value := os.Getenv(key)
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("%s must be a boolean: %q", key, value)
	}
	*target = parsed
	return nil
}

func parseDurationEnv(key string, target *time.Duration) error {
	value := os.Getenv(key)
	if value == "" {
		return nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fmt.Errorf("%s must be a positive duration: %q", key, value)
	}
	*target = parsed
	return nil
}

func parsePositiveFloatEnv(key string, target *float64) error {
	value := os.Getenv(key)
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		return fmt.Errorf("%s must be a positive number: %q", key, value)
	}
	*target = parsed
	return nil
}

func parsePositiveIntEnv(key string, target *int) error {
	value := os.Getenv(key)
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fmt.Errorf("%s must be a positive integer: %q", key, value)
	}
	*target = parsed
	return nil
}

func knownEnvironmentVariables() []string {
	return []string{
		"SEARCH_ADDR", "SEARCH_DEBUG", "SEARCH_DEBUG_TOKEN", "SEARCH_DEBUG_DIR", "SEARCH_DEBUG_PREVIEW_BYTES",
		"SEARCH_CHROME_PROFILE_DIR", "SEARCH_CHROME_PATH", "SEARCH_CHROME_HEADLESS", "SEARCH_CHROME_NO_SANDBOX",
		"SEARCH_BING_PROFILE_DIR", "SEARCH_BRAVE_PROFILE_DIR", "SEARCH_DESKTOP_URL", "SEARCH_MOBILE_URL", "SEARCH_DUCKDUCKGO_URL", "SEARCH_BING_URL", "SEARCH_BRAVE_URL", "SEARCH_USER_AGENT",
		"SEARCH_TOTAL_TIMEOUT", "SEARCH_DESKTOP_TIMEOUT", "SEARCH_MOBILE_TIMEOUT", "SEARCH_CHROME_TIMEOUT", "SEARCH_DUCKDUCKGO_TIMEOUT", "SEARCH_BING_TIMEOUT", "SEARCH_BRAVE_TIMEOUT",
		"SEARCH_FRESH_TTL", "SEARCH_STALE_TTL", "SEARCH_PROVIDER_RATE", "SEARCH_PROVIDER_BURST",
		"SEARCH_JITTER_MIN", "SEARCH_JITTER_MAX",
		"SEARCH_CLIENT_RATE", "SEARCH_CLIENT_BURST", "SEARCH_CACHE_MAX_ITEMS", "SEARCH_MAX_BODY_BYTES",
		"SEARCH_TRUSTED_PROXIES",
		"SEARCH_READ_ENABLED", "SEARCH_READ_HTTP_TIMEOUT", "SEARCH_READ_BROWSER_ENABLED", "SEARCH_READ_BROWSER_TIMEOUT", "SEARCH_READ_BROWSER_WAIT", "SEARCH_READ_CHROME_PROFILE_DIR",
		"SEARCH_READ_FRESH_TTL", "SEARCH_READ_STALE_TTL",
		"SEARCH_READ_CACHE_MAX_ITEMS", "SEARCH_READ_MAX_BODY_BYTES", "SEARCH_READ_MAX_REDIRECTS",
		"SEARCH_READ_HOST_ALLOWLIST", "SEARCH_PROVIDER_BROWSER_SLOTS", "SEARCH_READ_BROWSER_SLOTS",
	}
}
