package domain

// ErrorCode is a stable machine-readable read failure code.
type ErrorCode string

const (
	// ErrInvalidRequest indicates invalid caller input.
	ErrInvalidRequest ErrorCode = "invalid_request"
	// ErrCaptchaRequired indicates that the target requires human verification.
	// TODO(read/challenge): add a pluggable challenge-handler interface when
	// captcha handling is intentionally introduced. Phase one only reports it.
	ErrCaptchaRequired ErrorCode = "captcha_required"
)
