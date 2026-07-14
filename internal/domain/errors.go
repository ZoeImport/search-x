package domain

type ErrorCode string

const (
	ErrInvalidRequest      ErrorCode = "invalid_request"
	ErrDebugUnauthorized   ErrorCode = "debug_unauthorized"
	ErrProviderNotFound    ErrorCode = "provider_not_found"
	ErrRateLimited         ErrorCode = "rate_limited"
	ErrUpstreamChanged     ErrorCode = "upstream_changed"
	ErrCaptchaRequired     ErrorCode = "captcha_required"
	ErrProviderUnavailable ErrorCode = "provider_unavailable"
	ErrUpstreamTimeout     ErrorCode = "upstream_timeout"
)

type SearchError struct {
	Code      ErrorCode
	Message   string
	Retryable bool
	Original  error
	Attempts  []Attempt
	Artifacts []string
}

func (e *SearchError) Error() string {
	if e == nil {
		return ""
	}
	if e.Original != nil {
		return e.Original.Error()
	}
	return e.Message
}

func (e *SearchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Original
}
