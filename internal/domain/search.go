package domain

import "time"

type SearchRequest struct {
	Query     string
	Provider  string
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
	Transport       string              `json:"transport"`
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
	Transport       string `json:"transport"`
	Cached          bool   `json:"cached"`
	Degraded        bool   `json:"degraded"`
	FallbackCount   int    `json:"fallback_count"`
	TookMS          int64  `json:"took_ms"`
	RequestID       string `json:"request_id"`
	CacheAgeSeconds int64  `json:"cache_age_seconds,omitempty"`
}

type Debug struct {
	Attempts     []Attempt `json:"attempts"`
	RawArtifacts []string  `json:"raw_artifacts,omitempty"`
}

type SearchResponse struct {
	Query    string         `json:"query"`
	Provider string         `json:"provider"`
	Results  []SearchResult `json:"results"`
	Meta     Meta           `json:"meta"`
	Warnings []Warning      `json:"warnings"`
	Debug    *Debug         `json:"debug,omitempty"`
	StoredAt time.Time      `json:"-"`
}
