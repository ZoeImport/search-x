package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
	Server struct {
		Address           *string  `yaml:"address"`
		DefaultTimeout    *string  `yaml:"default_timeout"`
		MaxTimeout        *string  `yaml:"max_timeout"`
		CORSAllowedOrigin []string `yaml:"cors_allowed_origins"`
	} `yaml:"server"`
	API struct {
		CursorKey             *string  `yaml:"cursor_key"`
		CursorTTL             *string  `yaml:"cursor_ttl"`
		EnabledProviders      []string `yaml:"enabled_providers"`
		AllowRequestProviders *bool    `yaml:"allow_request_providers"`
		ProviderVisibility    *string  `yaml:"provider_visibility"`
		CacheBypass           *bool    `yaml:"cache_bypass"`
		APIKey                *string  `yaml:"api_key"`
	} `yaml:"api"`
	Cache struct {
		FreshTTL *string `yaml:"fresh_ttl"`
		StaleTTL *string `yaml:"stale_ttl"`
		MaxItems *int    `yaml:"max_items"`
	} `yaml:"cache"`
	Browser struct {
		Path          *string `yaml:"path"`
		Headless      *bool   `yaml:"headless"`
		NoSandbox     *bool   `yaml:"no_sandbox"`
		Slots         *int    `yaml:"slots"`
		ChromeTimeout *string `yaml:"chrome_timeout"`
		UserAgent     *string `yaml:"user_agent"`
		MaxBodyByte   *int64  `yaml:"max_body_bytes"`
	} `yaml:"browser"`
	Routing struct {
		AutoWait             *string  `yaml:"auto_wait"`
		ExplicitWait         *string  `yaml:"explicit_wait"`
		MaxProviderAttempts  *int     `yaml:"max_provider_attempts"`
		MinimumAttemptBudget *string  `yaml:"minimum_attempt_budget"`
		GlobalInflightMax    *int     `yaml:"global_inflight_max"`
		AutoQueueMax         *int     `yaml:"auto_queue_max"`
		CapacityRouter       *bool    `yaml:"capacity_router_enabled"`
		ProfilePool          *bool    `yaml:"profile_pool_enabled"`
		ProviderRate         *float64 `yaml:"provider_rate"`
		ProviderBurst        *int     `yaml:"provider_burst"`
		JitterMin            *string  `yaml:"jitter_min"`
		JitterMax            *string  `yaml:"jitter_max"`
		ProfileManifestRoot  *string  `yaml:"profile_manifest_root"`
	} `yaml:"routing"`
	Providers struct {
		Baidu      providerYAML `yaml:"baidu"`
		Bing       providerYAML `yaml:"bing"`
		Brave      providerYAML `yaml:"brave"`
		DuckDuckGo providerYAML `yaml:"duckduckgo"`
	} `yaml:"providers"`
	Observability struct {
		DiagnosticsEnabled *bool   `yaml:"diagnostics_enabled"`
		DebugDir           *string `yaml:"debug_dir"`
		DebugPreviewBytes  *int    `yaml:"debug_preview_bytes"`
		StoreQuery         *bool   `yaml:"store_query"`
		QueryPreviewChars  *int    `yaml:"query_preview_chars"`
		LogLevel           *string `yaml:"log_level"`
		LogFile            *string `yaml:"log_file"`
		LogMaxSizeMB       *int    `yaml:"log_max_size_mb"`
		LogMaxBackups      *int    `yaml:"log_max_backups"`
		LogMaxAgeDays      *int    `yaml:"log_max_age_days"`
		LogCompress        *bool   `yaml:"log_compress"`
	} `yaml:"observability"`
}

type providerYAML struct {
	URL                *string `yaml:"url"`
	MobileURL          *string `yaml:"mobile_url"`
	Timeout            *string `yaml:"timeout"`
	MobileTimeout      *string `yaml:"mobile_timeout"`
	SessionMinInterval *string `yaml:"session_min_interval"`
	SessionJitterMax   *string `yaml:"session_jitter_max"`
	CaptchaCooldown    *string `yaml:"captcha_cooldown"`
	RateLimitCooldown  *string `yaml:"rate_limit_cooldown"`
	FallbackReserve    *string `yaml:"fallback_reserve"`
	ProfileDir         *string `yaml:"profile_dir"`
	ProfileCount       *int    `yaml:"profile_count"`
	ProfileCapacity    *int    `yaml:"profile_capacity"`
}

func applyYAML(config *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var file yamlConfig
	if err := decoder.Decode(&file); err != nil {
		return fmt.Errorf("decode config %s: %w", path, err)
	}
	var extra interface{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode config %s: multiple YAML documents are not allowed", path)
	}
	setString := func(target *string, value *string) {
		if value != nil {
			*target = *value
		}
	}
	setBool := func(target *bool, value *bool) {
		if value != nil {
			*target = *value
		}
	}
	setInt := func(target *int, value *int) {
		if value != nil {
			*target = *value
		}
	}
	setInt64 := func(target *int64, value *int64) {
		if value != nil {
			*target = *value
		}
	}
	setDuration := func(name string, target *time.Duration, value *string) error {
		if value == nil {
			return nil
		}
		parsed, err := time.ParseDuration(*value)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("%s must be a positive duration", name)
		}
		*target = parsed
		return nil
	}
	setNonNegativeDuration := func(name string, target *time.Duration, value *string) error {
		if value == nil {
			return nil
		}
		parsed, err := time.ParseDuration(*value)
		if err != nil || parsed < 0 {
			return fmt.Errorf("%s must be a non-negative duration", name)
		}
		*target = parsed
		return nil
	}
	setString(&config.Address, file.Server.Address)
	setString(&config.CursorSecret, file.API.CursorKey)
	setString(&config.ProviderVisibility, file.API.ProviderVisibility)
	setString(&config.APIKey, file.API.APIKey)
	setBool(&config.AllowRequestProviders, file.API.AllowRequestProviders)
	setBool(&config.CacheBypass, file.API.CacheBypass)
	if file.API.EnabledProviders != nil {
		config.EnabledProviders = append([]string(nil), file.API.EnabledProviders...)
	}
	if file.Server.CORSAllowedOrigin != nil {
		config.CORSAllowedOrigins = append([]string(nil), file.Server.CORSAllowedOrigin...)
	}
	for _, item := range []struct {
		name   string
		target *time.Duration
		value  *string
	}{
		{"server.default_timeout", &config.TotalTimeout, file.Server.DefaultTimeout}, {"server.max_timeout", &config.MaxRequestTimeout, file.Server.MaxTimeout}, {"api.cursor_ttl", &config.CursorTTL, file.API.CursorTTL},
		{"cache.fresh_ttl", &config.FreshTTL, file.Cache.FreshTTL}, {"cache.stale_ttl", &config.StaleTTL, file.Cache.StaleTTL}, {"routing.auto_wait", &config.AutoRouteWait, file.Routing.AutoWait}, {"routing.explicit_wait", &config.ExplicitRouteWait, file.Routing.ExplicitWait}, {"routing.minimum_attempt_budget", &config.MinimumAttemptBudget, file.Routing.MinimumAttemptBudget},
		{"routing.jitter_min", &config.JitterMin, file.Routing.JitterMin}, {"routing.jitter_max", &config.JitterMax, file.Routing.JitterMax}, {"browser.chrome_timeout", &config.ChromeTimeout, file.Browser.ChromeTimeout},
		{"providers.baidu.timeout", &config.DesktopTimeout, file.Providers.Baidu.Timeout}, {"providers.bing.timeout", &config.BingTimeout, file.Providers.Bing.Timeout}, {"providers.brave.timeout", &config.BraveTimeout, file.Providers.Brave.Timeout}, {"providers.duckduckgo.timeout", &config.DuckDuckGoTimeout, file.Providers.DuckDuckGo.Timeout},
		{"providers.baidu.mobile_timeout", &config.MobileTimeout, file.Providers.Baidu.MobileTimeout}, {"providers.baidu.session_min_interval", &config.BaiduSessionMinInterval, file.Providers.Baidu.SessionMinInterval}, {"providers.baidu.captcha_cooldown", &config.BaiduCaptchaCooldown, file.Providers.Baidu.CaptchaCooldown}, {"providers.baidu.rate_limit_cooldown", &config.BaiduRateLimitCooldown, file.Providers.Baidu.RateLimitCooldown},
	} {
		if err := setDuration(item.name, item.target, item.value); err != nil {
			return err
		}
	}
	for _, item := range []struct {
		name   string
		target *time.Duration
		value  *string
	}{
		{"providers.baidu.session_jitter_max", &config.BaiduSessionMaxJitter, file.Providers.Baidu.SessionJitterMax},
		{"providers.baidu.fallback_reserve", &config.BaiduFallbackReserve, file.Providers.Baidu.FallbackReserve},
	} {
		if err := setNonNegativeDuration(item.name, item.target, item.value); err != nil {
			return err
		}
	}
	setInt(&config.CacheMaxItems, file.Cache.MaxItems)
	setString(&config.ChromePath, file.Browser.Path)
	setBool(&config.ChromeHeadless, file.Browser.Headless)
	setBool(&config.ChromeNoSandbox, file.Browser.NoSandbox)
	setInt(&config.ProviderBrowserSlots, file.Browser.Slots)
	setString(&config.UserAgent, file.Browser.UserAgent)
	setInt64(&config.MaxBodyBytes, file.Browser.MaxBodyByte)
	setInt(&config.MaxProviderAttempts, file.Routing.MaxProviderAttempts)
	setInt(&config.GlobalInflightMax, file.Routing.GlobalInflightMax)
	setInt(&config.AutoQueueMax, file.Routing.AutoQueueMax)
	setBool(&config.CapacityRouterEnabled, file.Routing.CapacityRouter)
	setBool(&config.ProfilePoolEnabled, file.Routing.ProfilePool)
	if file.Routing.ProviderRate != nil {
		config.ProviderRate = *file.Routing.ProviderRate
	}
	setInt(&config.ProviderBurst, file.Routing.ProviderBurst)
	setString(&config.ProfileManifestRoot, file.Routing.ProfileManifestRoot)
	applyProvider := func(file providerYAML, url, profile *string, count, capacity *int) {
		setString(url, file.URL)
		setString(profile, file.ProfileDir)
		setInt(count, file.ProfileCount)
		setInt(capacity, file.ProfileCapacity)
	}
	applyProvider(file.Providers.Baidu, &config.DesktopURL, &config.ChromeProfileDir, &config.BaiduProfileCount, &config.BaiduProfileCapacity)
	setString(&config.MobileURL, file.Providers.Baidu.MobileURL)
	applyProvider(file.Providers.Bing, &config.BingURL, &config.BingProfileDir, &config.BingProfileCount, &config.BingProfileCapacity)
	applyProvider(file.Providers.Brave, &config.BraveURL, &config.BraveProfileDir, &config.BraveProfileCount, &config.BraveProfileCapacity)
	setString(&config.DuckDuckGoURL, file.Providers.DuckDuckGo.URL)
	setInt(&config.DuckDuckGoProfileCount, file.Providers.DuckDuckGo.ProfileCount)
	setInt(&config.DuckDuckGoProfileCapacity, file.Providers.DuckDuckGo.ProfileCapacity)
	setBool(&config.Debug, file.Observability.DiagnosticsEnabled)
	setString(&config.DebugDir, file.Observability.DebugDir)
	setInt(&config.DebugPreviewBytes, file.Observability.DebugPreviewBytes)
	setBool(&config.LogStoreQuery, file.Observability.StoreQuery)
	setInt(&config.LogQueryPreviewChars, file.Observability.QueryPreviewChars)
	setString(&config.Log.Level, file.Observability.LogLevel)
	setString(&config.Log.File, file.Observability.LogFile)
	setInt(&config.Log.MaxSizeMB, file.Observability.LogMaxSizeMB)
	setInt(&config.Log.MaxBackups, file.Observability.LogMaxBackups)
	setInt(&config.Log.MaxAgeDays, file.Observability.LogMaxAgeDays)
	setBool(&config.Log.Compress, file.Observability.LogCompress)
	return nil
}
