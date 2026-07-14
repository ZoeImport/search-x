package httpapi

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"web-search-backend/internal/domain"
)

type readPayload struct {
	URL      string              `json:"url" binding:"required,max=2048"`
	Format   domain.OutputFormat `json:"format"`
	MaxChars int                 `json:"max_chars"`
	Refresh  bool                `json:"refresh"`
	Debug    *bool               `json:"debug"`
}

type readErrorResponse struct {
	Error errorBody         `json:"error"`
	Meta  readErrorMeta     `json:"meta"`
	Debug *domain.ReadDebug `json:"debug,omitempty"`
}

type readErrorMeta struct {
	RequestID string `json:"request_id"`
}

func (h *handler) read(c *gin.Context) {
	var payload readPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeReadError(c, &domain.ReadError{
			Code: domain.ErrInvalidRequest, Message: "正文读取参数错误", Retryable: false, Original: err,
		}, false)
		return
	}
	effectiveDebug := false
	if payload.Debug != nil {
		effectiveDebug = *payload.Debug
		if effectiveDebug && !validDebugToken(h.options.DebugToken, c.GetHeader("X-Debug-Token")) {
			writeReadError(c, &domain.ReadError{
				Code: domain.ErrDebugUnauthorized, Message: "调试访问未授权", Retryable: false,
				Original: errors.New("debug token missing or invalid"),
			}, false)
			return
		}
	}
	request := domain.ReadRequest{
		URL: payload.URL, Format: payload.Format, MaxChars: payload.MaxChars,
		Refresh: payload.Refresh, Debug: effectiveDebug, RequestID: requestID(c),
	}
	if effectiveDebug {
		request.Refresh = true
	}
	ctx, cancel := contextWithTimeout(c, h.options.TotalTimeout)
	defer cancel()
	response, err := h.reader.Read(ctx, request)
	if err != nil {
		h.logReadFailure(c, payload.URL, err)
		writeReadError(c, err, effectiveDebug)
		return
	}
	if response.Meta.RequestID == "" {
		response.Meta.RequestID = request.RequestID
	}
	if response.Warnings == nil {
		response.Warnings = make([]domain.ReadWarning, 0)
	}
	if !effectiveDebug {
		response.Debug = nil
	}
	c.JSON(http.StatusOK, response)
}

func (h *handler) logReadFailure(c *gin.Context, rawURL string, err error) {
	code := domain.ErrFetchFailed
	var readErr *domain.ReadError
	if errors.As(err, &readErr) && readErr.Code != "" {
		code = readErr.Code
	}
	h.options.Logger.Warn("read request failed",
		"request_id", requestID(c),
		"url", redactedReadURL(rawURL),
		"code", code,
		"error", err,
	)
}

func redactedReadURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid-url>"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func writeReadError(c *gin.Context, err error, debug bool) {
	var readErr *domain.ReadError
	if !errors.As(err, &readErr) {
		readErr = &domain.ReadError{
			Code: domain.ErrFetchFailed, Message: "正文读取服务内部错误", Retryable: true, Original: err,
		}
	}
	body := readErrorResponse{
		Error: errorBody{Code: readErr.Code, Message: readErr.Message, Retryable: readErr.Retryable},
		Meta:  readErrorMeta{RequestID: requestID(c)},
	}
	if debug {
		body.Error.OriginalError = readErr.Error()
		body.Debug = &domain.ReadDebug{
			Attempts:     append([]domain.ReadAttempt(nil), readErr.Attempts...),
			RawArtifacts: append([]string(nil), readErr.Artifacts...),
		}
	}
	c.AbortWithStatusJSON(statusForCode(readErr.Code), body)
}
