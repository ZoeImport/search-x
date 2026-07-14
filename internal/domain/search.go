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
	// TransportNameFreshCache identifies a fresh cache response.
	TransportNameFreshCache TransportName = "fresh_cache"
	// TransportNameStaleCache identifies a stale cache response.
	TransportNameStaleCache TransportName = "stale_cache"
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
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Attempt struct {
	Provider        ProviderName        `json:"provider,omitempty"`
	Transport       TransportName       `json:"transport"`
	RequestURL      string              `json:"request_url,omitempty"`
	HTTPStatus      int                 `json:"http_status,omitempty"`
	FinalURL        string              `json:"final_url,omitempty"`
	ElapsedMS       int64               `json:"elapsed_ms"`
	Classification  string              `json:"classification"`
	ParserError     string              `json:"parser_error,omitempty"`
	OriginalError   string              `json:"original_error,omitempty"`
	ResponseHeaders map[string][]string `json:"response_headers,omitempty"`
	BodyPreview     string              `json:"body_preview,omitempty"`
	BodySHA256      string              `json:"body_sha256,omitempty"`
}

type Meta struct {
	RequestedProvider     ProviderName  `json:"requested_provider,omitempty"`
	Transport             TransportName `json:"transport"`
	Cached                bool          `json:"cached"`
	Degraded              bool          `json:"degraded"`
	FallbackCount         int           `json:"fallback_count"`
	ProviderFallbackCount int           `json:"provider_fallback_count"`
	TookMS                int64         `json:"took_ms"`
	RequestID             string        `json:"request_id"`
	CacheAgeSeconds       int64         `json:"cache_age_seconds,omitempty"`
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
