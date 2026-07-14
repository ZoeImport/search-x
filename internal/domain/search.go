package domain

import "time"

// ProviderName identifies a registered search source or strategy.
type ProviderName string

const (
	// ProviderNameAuto selects the configured provider chain.
	ProviderNameAuto ProviderName = "auto"
	// ProviderNameBaidu selects Baidu search only.
	ProviderNameBaidu ProviderName = "baidu"
	// ProviderNameDuckDuckGo selects DuckDuckGo search only.
	ProviderNameDuckDuckGo ProviderName = "duckduckgo"
	// ProviderNameBing selects Bing search only.
	ProviderNameBing ProviderName = "bing"
	// ProviderNameBrave selects Brave search only.
	ProviderNameBrave ProviderName = "brave"
)

// TransportName identifies the concrete transport used by a provider.
type TransportName string

const (
	// TransportNameDesktopHTTP identifies Baidu desktop HTTP.
	TransportNameDesktopHTTP TransportName = "desktop_http"
	// TransportNameMobileHTTP identifies Baidu mobile HTTP.
	TransportNameMobileHTTP TransportName = "mobile_http"
	// TransportNameChromedp identifies the Baidu Chromedp transport.
	TransportNameChromedp TransportName = "chromedp"
	// TransportNameDuckDuckGoHTTP identifies DuckDuckGo HTML HTTP.
	TransportNameDuckDuckGoHTTP TransportName = "duckduckgo_http"
	// TransportNameBingChromedp identifies Bing browser search.
	TransportNameBingChromedp TransportName = "bing_chromedp"
	// TransportNameBraveChromedp identifies Brave browser search.
	TransportNameBraveChromedp TransportName = "brave_chromedp"
	// TransportNameFreshCache identifies a fresh cache response.
	TransportNameFreshCache TransportName = "fresh_cache"
	// TransportNameStaleCache identifies a stale cache response.
	TransportNameStaleCache TransportName = "stale_cache"
)

// Classification identifies the outcome of one upstream search attempt.
type Classification string

const (
	// ClassificationNormal indicates a normal result page.
	ClassificationNormal Classification = "normal"
	// ClassificationEmpty indicates a valid page without results.
	ClassificationEmpty Classification = "empty"
	// ClassificationCaptcha indicates a security verification page.
	ClassificationCaptcha Classification = "captcha"
	// ClassificationRateLimited indicates an upstream rate limit.
	ClassificationRateLimited Classification = "rate_limited"
	// ClassificationBlocked indicates an upstream access block.
	ClassificationBlocked Classification = "blocked"
	// ClassificationParseChanged indicates unexpected upstream markup.
	ClassificationParseChanged Classification = "parse_changed"
	// ClassificationTimeout indicates a context or upstream timeout.
	ClassificationTimeout Classification = "timeout"
	// ClassificationNetworkError indicates a transport-level network error.
	ClassificationNetworkError Classification = "network_error"
)

// WarningCode identifies a stable non-fatal search warning.
type WarningCode string

const (
	// WarningCodeArtifactSaveError indicates a debug artifact persistence failure.
	WarningCodeArtifactSaveError WarningCode = "artifact_save_error"
	// WarningCodeCacheWriteError indicates a successful response could not be cached.
	WarningCodeCacheWriteError WarningCode = "cache_write_error"
	// WarningCodeLiveSearchUnavailable indicates stale cache was returned after a live failure.
	WarningCodeLiveSearchUnavailable WarningCode = "live_search_unavailable"
	// WarningCodeProviderFallback indicates a later provider produced the response.
	WarningCodeProviderFallback WarningCode = "provider_fallback"
	// WarningCodeRedirectURLUnresolved indicates a search-engine redirect URL was preserved.
	WarningCodeRedirectURLUnresolved WarningCode = "redirect_url_unresolved"
	// WarningCodePartialReadableResults indicates fewer usable bodies than requested.
	WarningCodePartialReadableResults WarningCode = "partial_readable_results"
)

type SearchRequest struct {
	Query     string
	Provider  ProviderName
	RequestID string
	Limit     int
	Page      int
	Refresh   bool
	Debug     bool
}

type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Rank    int    `json:"rank"`
	// Provider identifies the search source that produced this result.
	Provider ProviderName `json:"provider"`
}

type Warning struct {
	Code    WarningCode `json:"code"`
	Message string      `json:"message"`
}

type Attempt struct {
	// Provider identifies the provider that made this upstream attempt.
	Provider        ProviderName        `json:"provider,omitempty"`
	Transport       TransportName       `json:"transport"`
	HeaderProfile   string              `json:"header_profile,omitempty"`
	RequestURL      string              `json:"request_url,omitempty"`
	HTTPStatus      int                 `json:"http_status,omitempty"`
	FinalURL        string              `json:"final_url,omitempty"`
	ElapsedMS       int64               `json:"elapsed_ms"`
	Classification  Classification      `json:"classification"`
	ParserError     string              `json:"parser_error,omitempty"`
	OriginalError   string              `json:"original_error,omitempty"`
	ResponseHeaders map[string][]string `json:"response_headers,omitempty"`
	BodyPreview     string              `json:"body_preview,omitempty"`
	BodySHA256      string              `json:"body_sha256,omitempty"`
}

type Meta struct {
	// RequestedProvider preserves the provider selected by the caller.
	RequestedProvider ProviderName  `json:"requested_provider,omitempty"`
	Transport         TransportName `json:"transport"`
	Cached            bool          `json:"cached"`
	Degraded          bool          `json:"degraded"`
	FallbackCount     int           `json:"fallback_count"`
	// ProviderFallbackCount records cross-provider fallback transitions.
	ProviderFallbackCount int    `json:"provider_fallback_count"`
	TookMS                int64  `json:"took_ms"`
	RequestID             string `json:"request_id"`
	CacheAgeSeconds       int64  `json:"cache_age_seconds,omitempty"`
}

type Debug struct {
	Attempts     []Attempt `json:"attempts"`
	RawArtifacts []string  `json:"raw_artifacts,omitempty"`
}

type SearchResponse struct {
	Query    string         `json:"query"`
	Provider ProviderName   `json:"provider"`
	Results  []SearchResult `json:"results"`
	Meta     Meta           `json:"meta"`
	Warnings []Warning      `json:"warnings"`
	Debug    *Debug         `json:"debug,omitempty"`
	StoredAt time.Time      `json:"-"`
}
