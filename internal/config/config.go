package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address           string
	Debug             bool
	DebugToken        string
	DebugDir          string
	DebugPreviewBytes int
	ChromeProfileDir  string
	ChromePath        string
	ChromeHeadless    bool
	ChromeNoSandbox   bool
	DesktopURL        string
	MobileURL         string
	UserAgent         string
	TotalTimeout      time.Duration
	DesktopTimeout    time.Duration
	MobileTimeout     time.Duration
	ChromeTimeout     time.Duration
	FreshTTL          time.Duration
	StaleTTL          time.Duration
	ProviderRate      float64
	ProviderBurst     int
	JitterMin         time.Duration
	JitterMax         time.Duration
	ClientRate        float64
	ClientBurst       int
	CacheMaxItems     int
	MaxBodyBytes      int64
	TrustedProxies    []string
}

func Load() (Config, error) {
	config := Config{
		Address:           ":8080",
		Debug:             true,
		DebugDir:          "./var/debug",
		DebugPreviewBytes: 32 * 1024,
		ChromeProfileDir:  "./var/chrome-profile",
		ChromeHeadless:    true,
		DesktopURL:        "https://www.baidu.com/s",
		MobileURL:         "https://m.baidu.com/s",
		UserAgent:         "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0 Safari/537.36",
		TotalTimeout:      20 * time.Second,
		DesktopTimeout:    4 * time.Second,
		MobileTimeout:     4 * time.Second,
		ChromeTimeout:     10 * time.Second,
		FreshTTL:          15 * time.Minute,
		StaleTTL:          24 * time.Hour,
		ProviderRate:      1,
		ProviderBurst:     3,
		JitterMin:         200 * time.Millisecond,
		JitterMax:         800 * time.Millisecond,
		ClientRate:        5,
		ClientBurst:       10,
		CacheMaxItems:     1000,
		MaxBodyBytes:      4 << 20,
	}

	stringValues := []struct {
		key    string
		target *string
	}{
		{"SEARCH_ADDR", &config.Address},
		{"SEARCH_DEBUG_TOKEN", &config.DebugToken},
		{"SEARCH_DEBUG_DIR", &config.DebugDir},
		{"SEARCH_CHROME_PROFILE_DIR", &config.ChromeProfileDir},
		{"SEARCH_CHROME_PATH", &config.ChromePath},
		{"SEARCH_DESKTOP_URL", &config.DesktopURL},
		{"SEARCH_MOBILE_URL", &config.MobileURL},
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
	if value := os.Getenv("SEARCH_TRUSTED_PROXIES"); value != "" {
		for _, proxy := range strings.Split(value, ",") {
			if proxy = strings.TrimSpace(proxy); proxy != "" {
				config.TrustedProxies = append(config.TrustedProxies, proxy)
			}
		}
	}

	if config.Address == "" || config.DebugDir == "" || config.ChromeProfileDir == "" {
		return Config{}, fmt.Errorf("SEARCH_ADDR, SEARCH_DEBUG_DIR and SEARCH_CHROME_PROFILE_DIR must not be empty")
	}
	if config.StaleTTL <= config.FreshTTL {
		return Config{}, fmt.Errorf("SEARCH_STALE_TTL must be greater than SEARCH_FRESH_TTL")
	}
	if config.JitterMax < config.JitterMin {
		return Config{}, fmt.Errorf("SEARCH_JITTER_MAX must be greater than or equal to SEARCH_JITTER_MIN")
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
		"SEARCH_DESKTOP_URL", "SEARCH_MOBILE_URL", "SEARCH_USER_AGENT",
		"SEARCH_TOTAL_TIMEOUT", "SEARCH_DESKTOP_TIMEOUT", "SEARCH_MOBILE_TIMEOUT", "SEARCH_CHROME_TIMEOUT",
		"SEARCH_FRESH_TTL", "SEARCH_STALE_TTL", "SEARCH_PROVIDER_RATE", "SEARCH_PROVIDER_BURST",
		"SEARCH_JITTER_MIN", "SEARCH_JITTER_MAX",
		"SEARCH_CLIENT_RATE", "SEARCH_CLIENT_BURST", "SEARCH_CACHE_MAX_ITEMS", "SEARCH_MAX_BODY_BYTES",
		"SEARCH_TRUSTED_PROXIES",
	}
}
