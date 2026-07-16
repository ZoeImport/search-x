package domain

// ErrorCode identifies a stable API error category.
type ErrorCode string

const (
	// ErrInvalidRequest indicates invalid caller input.
	ErrInvalidRequest ErrorCode = "invalid_request"
	// ErrProviderNotFound indicates an unregistered Provider name.
	ErrProviderNotFound ErrorCode = "provider_not_found"
	// ErrRateLimited indicates an upstream or local request limit.
	ErrRateLimited ErrorCode = "rate_limited"
	// ErrUpstreamChanged indicates an unexpected upstream response structure.
	ErrUpstreamChanged ErrorCode = "upstream_changed"
	// ErrCaptchaRequired indicates an upstream human-verification challenge.
	ErrCaptchaRequired ErrorCode = "captcha_required"
	// ErrProviderUnavailable indicates a Provider transport or service failure.
	ErrProviderUnavailable ErrorCode = "provider_unavailable"
	// ErrProviderBusy indicates an explicit Provider has no lease before its wait budget expires.
	ErrProviderBusy ErrorCode = "provider_busy"
	// ErrProviderCapacityExhausted indicates automatic routing found no Provider capacity.
	ErrProviderCapacityExhausted ErrorCode = "provider_capacity_exhausted"
	// ErrSearchQueueFull indicates the bounded live-search queue rejected the request.
	ErrSearchQueueFull ErrorCode = "search_queue_full"
	// ErrUpstreamTimeout indicates that upstream search exceeded its deadline.
	ErrUpstreamTimeout ErrorCode = "upstream_timeout"
)

// SearchError contains a stable search failure and authorized diagnostics.
type SearchError struct {
	// Code is the stable API error category.
	Code ErrorCode
	// Message is the caller-facing safe description.
	Message string
	// Retryable reports whether a later request may succeed.
	Retryable bool
	// Original retains the lowest-level error for authorized debugging.
	Original error
	// Attempts contains search Provider diagnostics.
	Attempts []Attempt
	// Artifacts contains authorized local debug artifact paths.
	Artifacts []string
}

// Error returns the lowest-level error when available, otherwise the stable message.
func (e *SearchError) Error() string {
	if e == nil {
		return ""
	}
	if e.Original != nil {
		return e.Original.Error()
	}
	return e.Message
}

// Unwrap returns the underlying search error.
func (e *SearchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Original
}
