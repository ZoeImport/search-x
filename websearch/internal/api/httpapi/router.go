package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"web-search-backend/runtime/httpx"
	"web-search-backend/runtime/requesttimeout"
	"web-search-backend/websearch/internal/cursor"
	"web-search-backend/websearch/internal/domain"
)

type Searcher interface {
	Search(context.Context, domain.SearchRequest) (domain.SearchResponse, error)
}

type Options struct {
	Searcher              Searcher
	Cursor                *cursor.Codec
	Ready                 func() bool
	Logger                *slog.Logger
	Timeout               time.Duration
	MaxTimeout            time.Duration
	CacheBypass           bool
	AllowRequestProviders bool
	EnabledProviders      []string
	ProviderVisibility    string
	AllowedOrigins        []string
}

type searchPayload struct {
	Query   string `json:"query"`
	Limit   int    `json:"limit,omitempty"`
	Cursor  string `json:"cursor,omitempty"`
	Timeout string `json:"timeout,omitempty"`
	Routing struct {
		Providers []domain.ProviderName `json:"providers,omitempty"`
	} `json:"routing,omitempty"`
	Filters struct {
		Region string `json:"region,omitempty"`
	} `json:"filters,omitempty"`
}

type searchEnvelope struct {
	RequestID string           `json:"request_id"`
	Query     string           `json:"query"`
	Results   []searchResult   `json:"results"`
	Page      pageInfo         `json:"page"`
	Meta      searchMeta       `json:"meta"`
	Warnings  []domain.Warning `json:"warnings"`
	Usage     usage            `json:"usage"`
}

type usage struct {
	Units int `json:"units"`
}

type searchResult struct {
	ID       string              `json:"id"`
	URL      string              `json:"url"`
	Title    string              `json:"title"`
	Snippet  string              `json:"snippet"`
	Rank     int                 `json:"rank"`
	Provider domain.ProviderName `json:"provider,omitempty"`
}
type pageInfo struct {
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}
type searchMeta struct {
	Cached          bool                `json:"cached"`
	CacheAgeSeconds int64               `json:"cache_age_seconds,omitempty"`
	TookMS          int64               `json:"took_ms"`
	Provider        domain.ProviderName `json:"provider,omitempty"`
}

func New(options Options) (*gin.Engine, error) {
	if options.Searcher == nil || options.Cursor == nil || options.Ready == nil || options.Logger == nil {
		return nil, errors.New("searcher, cursor, readiness check, and logger are required")
	}
	if options.Timeout <= 0 {
		options.Timeout = 20 * time.Second
	}
	if options.MaxTimeout <= 0 {
		options.MaxTimeout = 60 * time.Second
	}
	enabled := stringSet(options.EnabledProviders)
	router := gin.New()
	router.Use(gin.Recovery(), requestID(), cors(options.AllowedOrigins))
	router.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	router.GET("/readyz", func(c *gin.Context) {
		if !options.Ready() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	router.POST("/v1/websearch", func(c *gin.Context) {
		var payload searchPayload
		if err := decodeStrict(c.Request.Body, &payload); err != nil {
			writeProblem(c, http.StatusBadRequest, "invalid_request", "Invalid request", "The request body is invalid.", false, "")
			return
		}
		payload.Query = strings.Join(strings.Fields(payload.Query), " ")
		if payload.Query == "" || len([]rune(payload.Query)) > 256 {
			writeProblem(c, http.StatusBadRequest, "invalid_request", "Invalid query", "query must contain between 1 and 256 characters.", false, "query")
			return
		}
		if payload.Limit == 0 {
			payload.Limit = 10
		}
		if payload.Limit < 1 || payload.Limit > 20 {
			writeProblem(c, http.StatusBadRequest, "invalid_request", "Invalid limit", "limit must be between 1 and 20.", false, "limit")
			return
		}
		if len(payload.Cursor) > 4096 {
			writeProblem(c, http.StatusBadRequest, "invalid_request", "Invalid cursor", "cursor must not exceed 4096 characters.", false, "cursor")
			return
		}
		requestTimeout, timeoutErr := requesttimeout.Parse(payload.Timeout, options.Timeout, options.MaxTimeout)
		if timeoutErr != nil {
			writeProblem(c, http.StatusBadRequest, "invalid_request", "Invalid timeout", timeoutErr.Error(), false, "timeout")
			return
		}
		providers, providerErr := validateProviders(payload.Routing.Providers, options.EnabledProviders)
		if providerErr != nil {
			writeProblem(c, http.StatusBadRequest, "invalid_request", "Invalid providers", providerErr.Error(), false, "routing.providers")
			return
		}
		if len(payload.Routing.Providers) > 0 && !options.AllowRequestProviders {
			writeProblem(c, http.StatusForbidden, "provider_selection_forbidden", "Provider selection forbidden", "Request provider selection is disabled.", false, "routing.providers")
			return
		}
		provider, page := domain.ProviderName(""), 1
		if payload.Cursor != "" {
			state, err := options.Cursor.Decode(payload.Cursor)
			if err != nil {
				code := "invalid_cursor"
				if errors.Is(err, cursor.ErrExpired) {
					code = "cursor_expired"
				}
				writeProblem(c, http.StatusBadRequest, code, "Invalid cursor", "The pagination cursor is invalid or expired.", false, "cursor")
				return
			}
			cursorProviders := providerNames(state.Providers)
			if len(payload.Routing.Providers) == 0 {
				providers = cursorProviders
			}
			if state.QueryHash != cursor.QueryHash(payload.Query) || state.Limit != payload.Limit || !equalProviders(providers, cursorProviders) {
				writeProblem(c, http.StatusBadRequest, "cursor_mismatch", "Cursor mismatch", "query, limit, and routing providers must match the first page.", false, "cursor")
				return
			}
			provider, page = domain.ProviderName(state.Provider), state.ProviderPage
		}
		if _, ok := enabled[string(provider)]; provider != "" && !ok {
			writeProblem(c, http.StatusServiceUnavailable, "provider_unavailable", "Provider unavailable", "The provider pinned by this cursor is not enabled.", true, "cursor")
			return
		}
		requestID := requestIDValue(c)
		request := domain.SearchRequest{Query: payload.Query, Providers: providers, Provider: provider, Region: payload.Filters.Region, Limit: payload.Limit, Page: page, Refresh: options.CacheBypass, RequestID: requestID}
		ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
		defer cancel()
		response, err := options.Searcher.Search(ctx, request)
		if err != nil {
			options.Logger.Warn("search failed", "request_id", requestID, "query_hash", cursor.QueryHash(payload.Query), "provider", provider, "error", err)
			if payload.Cursor != "" {
				writeProblem(c, http.StatusServiceUnavailable, "pagination_source_unavailable", "Pagination source unavailable", "The provider selected for this cursor is temporarily unavailable.", true, "cursor")
				return
			}
			writeSearchProblem(c, err)
			return
		}
		showProvider := options.ProviderVisibility == "public"
		results := make([]searchResult, 0, len(response.Results))
		for _, item := range response.Results {
			result := searchResult{ID: resultID(item.URL), URL: item.URL, Title: item.Title, Snippet: item.Snippet, Rank: item.Rank}
			if showProvider {
				result.Provider = item.Provider
			}
			results = append(results, result)
		}
		pageResponse := pageInfo{}
		if len(results) == payload.Limit && page < 10 {
			token, encodeErr := options.Cursor.Encode(cursor.State{QueryHash: cursor.QueryHash(payload.Query), Provider: string(response.Provider), Providers: providerStrings(providers), ProviderPage: page + 1, Limit: payload.Limit})
			if encodeErr != nil {
				writeProblem(c, http.StatusInternalServerError, "cursor_encoding_failed", "Pagination unavailable", "The next page could not be created.", true, "")
				return
			}
			pageResponse.NextCursor, pageResponse.HasMore = token, true
		}
		meta := searchMeta{Cached: response.Meta.Cached, CacheAgeSeconds: response.Meta.CacheAgeSeconds, TookMS: response.Meta.TookMS}
		if showProvider {
			meta.Provider = response.Provider
		}
		options.Logger.Info("search completed", "request_id", requestID, "query_hash", cursor.QueryHash(payload.Query), "query_length", len([]rune(payload.Query)), "provider", response.Provider, "result_count", len(results), "cached", response.Meta.Cached, "took_ms", response.Meta.TookMS)
		warnings := response.Warnings
		if !showProvider {
			warnings = hideProviderWarnings(warnings)
		}
		c.JSON(http.StatusOK, searchEnvelope{RequestID: requestID, Query: payload.Query, Results: results, Page: pageResponse, Meta: meta, Warnings: warnings, Usage: usage{Units: 1}})
	})
	return router, nil
}

func writeSearchProblem(c *gin.Context, err error) {
	var searchErr *domain.SearchError
	if !errors.As(err, &searchErr) {
		writeProblem(c, http.StatusBadGateway, "upstream_failed", "Search failed", "Search providers are currently unavailable.", true, "")
		return
	}
	status, code := http.StatusBadGateway, string(searchErr.Code)
	switch searchErr.Code {
	case domain.ErrInvalidRequest:
		status = http.StatusBadRequest
	case domain.ErrProviderNotFound:
		status = http.StatusServiceUnavailable
	case domain.ErrSearchQueueFull, domain.ErrRateLimited:
		status = http.StatusTooManyRequests
	case domain.ErrUpstreamTimeout:
		status, code = http.StatusGatewayTimeout, "deadline_exceeded"
	}
	writeProblem(c, status, code, "Search failed", searchErr.Message, searchErr.Retryable, "")
}

func writeProblem(c *gin.Context, status int, code, title, detail string, retryable bool, parameter string) {
	c.Header("Content-Type", "application/problem+json")
	c.AbortWithStatusJSON(status, httpx.Problem{Type: "https://api.example.com/problems/" + strings.ReplaceAll(code, "_", "-"), Title: title, Status: status, Code: code, Detail: detail, RequestID: requestIDValue(c), Retryable: retryable, Parameter: parameter})
}
func decodeStrict(reader io.Reader, target any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request must contain one JSON value")
	}
	return nil
}
func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[strings.TrimSpace(value)] = struct{}{}
	}
	return result
}
func resultID(rawURL string) string { return "res_" + cursor.QueryHash(rawURL)[:16] }

func validateProviders(requested []domain.ProviderName, defaults []string) ([]domain.ProviderName, error) {
	if len(requested) == 0 {
		return providerNames(defaults), nil
	}
	enabled, seen := stringSet(defaults), map[string]struct{}{}
	result := make([]domain.ProviderName, 0, len(requested))
	for _, raw := range requested {
		name := strings.ToLower(strings.TrimSpace(string(raw)))
		if _, ok := enabled[name]; !ok {
			return nil, fmt.Errorf("provider %q is not enabled", name)
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("provider %q is duplicated", name)
		}
		seen[name] = struct{}{}
		result = append(result, domain.ProviderName(name))
	}
	return result, nil
}
func providerNames(values []string) []domain.ProviderName {
	result := make([]domain.ProviderName, len(values))
	for i, value := range values {
		result[i] = domain.ProviderName(value)
	}
	return result
}
func providerStrings(values []domain.ProviderName) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}
func equalProviders(left, right []domain.ProviderName) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
func hideProviderWarnings(values []domain.Warning) []domain.Warning {
	result := append([]domain.Warning(nil), values...)
	for i := range result {
		if result[i].Code == domain.WarningCodeProviderFallback {
			result[i].Message = "A search provider failed; a fallback provider was used"
			continue
		}
		replacer := strings.NewReplacer("baidu", "provider", "Baidu", "Provider", "bing", "provider", "Bing", "Provider", "brave", "provider", "Brave", "Provider", "duckduckgo", "provider", "DuckDuckGo", "Provider")
		result[i].Message = replacer.Replace(result[i].Message)
	}
	return result
}
