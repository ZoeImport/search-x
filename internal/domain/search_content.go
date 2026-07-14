package domain

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ReadStatus identifies whether one search candidate produced usable content.
type ReadStatus string

const (
	// ReadStatusSuccess indicates that the candidate produced usable content.
	ReadStatusSuccess ReadStatus = "success"
	// ReadStatusFailed indicates that the candidate did not produce usable content.
	ReadStatusFailed ReadStatus = "failed"
)

// SelectionReason identifies why one Provider result set was selected.
type SelectionReason string

const (
	// SelectionReasonExplicit indicates that the caller selected one Provider.
	SelectionReasonExplicit SelectionReason = "explicit_provider"
	// SelectionReasonQuality indicates that the result set won by quality score.
	SelectionReasonQuality SelectionReason = "highest_quality"
)

// ContentOptions configures optional content acquisition for a search request.
type ContentOptions struct {
	// Enabled turns a lightweight search into combined search and content reading.
	Enabled bool `json:"enabled"`
	// CandidateLimit overrides automatic oversampling when non-zero.
	CandidateLimit int `json:"candidate_limit"`
	// Format selects Markdown or plain-text content.
	Format OutputFormat `json:"format"`
	// MaxChars bounds each returned body by Unicode character count.
	MaxChars int `json:"max_chars"`
}

// SearchContentRequest contains one advanced search request.
type SearchContentRequest struct {
	// Query is the caller-provided search text.
	Query string `json:"query"`
	// Provider selects one Provider or automatic quality selection.
	Provider ProviderName `json:"provider"`
	// Limit is the requested result or usable-body count.
	Limit int `json:"limit"`
	// Content configures optional body acquisition.
	Content ContentOptions `json:"content"`
	// Refresh skips fresh search and content caches.
	Refresh bool `json:"refresh"`
	// Debug enables authorized diagnostics and forces refresh.
	Debug bool `json:"debug"`
	// RequestID correlates logs and responses.
	RequestID string `json:"-"`
}

// Normalize applies defaults and validates advanced search parameters.
func (request SearchContentRequest) Normalize() (SearchContentRequest, error) {
	request.Query = strings.Join(strings.Fields(request.Query), " ")
	if request.Query == "" {
		return SearchContentRequest{}, fmt.Errorf("query is required")
	}
	if utf8.RuneCountInString(request.Query) > 256 {
		return SearchContentRequest{}, fmt.Errorf("query exceeds 256 characters")
	}
	request.Provider = ProviderName(strings.ToLower(strings.TrimSpace(string(request.Provider))))
	if request.Provider == "" {
		request.Provider = ProviderNameAuto
	}
	if request.Limit == 0 {
		request.Limit = 5
	}
	if request.Limit < 1 || request.Limit > 10 {
		return SearchContentRequest{}, fmt.Errorf("limit must be between 1 and 10")
	}
	if request.Content.Format == "" {
		request.Content.Format = OutputFormatMarkdown
	}
	if request.Content.Format != OutputFormatMarkdown && request.Content.Format != OutputFormatText {
		return SearchContentRequest{}, fmt.Errorf("unsupported format %q", request.Content.Format)
	}
	if request.Content.MaxChars == 0 {
		request.Content.MaxChars = DefaultReadMaxChars
	}
	if request.Content.MaxChars < MinReadMaxChars || request.Content.MaxChars > MaxReadMaxChars {
		return SearchContentRequest{}, fmt.Errorf("max_chars must be between %d and %d", MinReadMaxChars, MaxReadMaxChars)
	}
	if request.Debug {
		request.Refresh = true
	}
	return request, nil
}

// SearchContentResult is one ranked search result with optional readable content.
type SearchContentResult struct {
	// OriginalRank is the Provider result-set rank before content selection.
	OriginalRank int `json:"original_rank"`
	// SelectedRank is the continuous usable-body rank in this response.
	SelectedRank int `json:"selected_rank,omitempty"`
	// Provider identifies the search source that produced this candidate.
	Provider ProviderName `json:"provider"`
	// Title is the search-result or extracted document title.
	Title string `json:"title"`
	// URL is the candidate URL.
	URL string `json:"url"`
	// Snippet is the Provider summary.
	Snippet string `json:"snippet"`
	// RelevanceScore is the local query-to-result score in the range zero to one.
	RelevanceScore float64 `json:"relevance_score"`
	// ReadStatus identifies the body-read outcome.
	ReadStatus ReadStatus `json:"read_status,omitempty"`
	// ReadTransport identifies how the body was acquired.
	ReadTransport ReadTransport `json:"read_transport,omitempty"`
	// Content is the converted and bounded body.
	Content string `json:"content,omitempty"`
	// ContentFormat identifies the representation of Content.
	ContentFormat OutputFormat `json:"content_format,omitempty"`
	// ContentLength is the Unicode character count of Content.
	ContentLength int `json:"content_length,omitempty"`
	// Truncated reports whether MaxChars shortened Content.
	Truncated bool `json:"truncated"`
}

// SearchContentFailure records one candidate that did not produce usable content.
type SearchContentFailure struct {
	// OriginalRank is the candidate's Provider rank.
	OriginalRank int `json:"original_rank"`
	// Provider identifies the candidate source.
	Provider ProviderName `json:"provider"`
	// URL is the failed candidate URL.
	URL string `json:"url"`
	// Code is the stable failure category.
	Code ErrorCode `json:"code"`
	// Retryable reports whether a later attempt may succeed.
	Retryable bool `json:"retryable"`
	// OriginalError contains low-level details only in authorized debug responses.
	OriginalError string `json:"original_error,omitempty"`
}

// ProviderQuality records one Provider result-set quality observation.
type ProviderQuality struct {
	// Provider identifies the evaluated search source.
	Provider ProviderName `json:"provider"`
	// Score is the whole result-set quality score.
	Score float64 `json:"score"`
	// ResultCount is the number of valid results evaluated.
	ResultCount int `json:"result_count"`
	// UniqueDomainCount is the number of distinct registrable domains.
	UniqueDomainCount int `json:"unique_domain_count"`
}

// ProviderFailure records one failed Provider selection attempt.
type ProviderFailure struct {
	// Provider identifies the failed search source.
	Provider ProviderName `json:"provider"`
	// Code is the stable search error category.
	Code ErrorCode `json:"code"`
	// Retryable reports whether a later attempt may succeed.
	Retryable bool `json:"retryable"`
	// OriginalError contains low-level details only in authorized debug responses.
	OriginalError string `json:"original_error,omitempty"`
}

// ProviderSelection describes an explicit or quality-based Provider choice.
type ProviderSelection struct {
	// RequestedProvider preserves the caller's Provider mode.
	RequestedProvider ProviderName `json:"requested_provider"`
	// SelectedProvider identifies the winning result set.
	SelectedProvider ProviderName `json:"selected_provider"`
	// Reason identifies the deterministic selection rule used.
	Reason SelectionReason `json:"reason"`
	// Qualities contains successful Provider quality observations.
	Qualities []ProviderQuality `json:"qualities"`
	// Failures contains failed Provider observations.
	Failures []ProviderFailure `json:"failures"`
}

// SearchContentMeta contains combined search and read timing and status.
type SearchContentMeta struct {
	// Partial reports that fewer usable bodies were returned than requested.
	Partial bool `json:"partial"`
	// SearchTookMS records Provider selection duration.
	SearchTookMS int64 `json:"search_took_ms"`
	// ReadTookMS records candidate reading duration.
	ReadTookMS int64 `json:"read_took_ms"`
	// TookMS records total combined request duration.
	TookMS int64 `json:"took_ms"`
	// RequestID correlates logs and responses.
	RequestID string `json:"request_id"`
}

// SearchContentDebug contains authorized combined-search diagnostics.
type SearchContentDebug struct {
	// Selection contains Provider scores and failures.
	Selection ProviderSelection `json:"selection"`
	// ReadAttempts contains diagnostics grouped by candidate URL.
	ReadAttempts map[string][]ReadAttempt `json:"read_attempts,omitempty"`
}

// SearchContentResponse is the successful combined-search payload.
type SearchContentResponse struct {
	// Query is the normalized search text.
	Query string `json:"query"`
	// RequestedProvider preserves the caller's Provider mode.
	RequestedProvider ProviderName `json:"requested_provider"`
	// SelectedProvider identifies the Provider result set used.
	SelectedProvider ProviderName `json:"selected_provider"`
	// CandidateCount is the number of unique candidates scheduled.
	CandidateCount int `json:"candidate_count"`
	// ReadableCount is the number of usable bodies returned.
	ReadableCount int `json:"readable_count"`
	// Results contains usable bodies in original-rank order.
	Results []SearchContentResult `json:"results"`
	// Failures contains candidates that did not produce usable bodies.
	Failures []SearchContentFailure `json:"failures"`
	// Meta contains timing and partial-result state.
	Meta SearchContentMeta `json:"meta"`
	// Warnings contains stable non-fatal diagnostics.
	Warnings []Warning `json:"warnings"`
	// Debug contains authorized low-level diagnostics.
	Debug *SearchContentDebug `json:"debug,omitempty"`
}
