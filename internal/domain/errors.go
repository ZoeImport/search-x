package domain

// ErrorCode identifies a stable API error category.
type ErrorCode string

const (
	// ErrInvalidRequest indicates invalid caller input.
	ErrInvalidRequest ErrorCode = "invalid_request"
	// ErrDebugUnauthorized indicates missing or invalid debug authorization.
	ErrDebugUnauthorized ErrorCode = "debug_unauthorized"
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
	// ErrUpstreamTimeout indicates that upstream search exceeded its deadline.
	ErrUpstreamTimeout ErrorCode = "upstream_timeout"
	// ErrInsufficientReadableResults indicates that no candidate produced usable content.
	ErrInsufficientReadableResults ErrorCode = "insufficient_readable_results"
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
	// ReadAttempts contains combined-search content diagnostics.
	ReadAttempts []ReadAttempt
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
