package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"web-search-backend/internal/domain"
)

type searchQuery struct {
	Q        string `form:"q" binding:"required,min=1,max=256"`
	Provider string `form:"provider"`
	Limit    int    `form:"limit" binding:"omitempty,min=1,max=20"`
	Page     int    `form:"page" binding:"omitempty,min=1,max=10"`
	Refresh  bool   `form:"refresh"`
	Debug    *bool  `form:"debug"`
}

type errorBody struct {
	Code          domain.ErrorCode `json:"code"`
	Message       string           `json:"message"`
	Retryable     bool             `json:"retryable"`
	OriginalError string           `json:"original_error,omitempty"`
}

type errorMeta struct {
	Provider  domain.ProviderName `json:"provider"`
	RequestID string              `json:"request_id"`
}

type errorResponse struct {
	Error errorBody     `json:"error"`
	Meta  errorMeta     `json:"meta"`
	Debug *domain.Debug `json:"debug,omitempty"`
}

func (h *handler) search(c *gin.Context) {
	var query searchQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		writeError(c, &domain.SearchError{
			Code: domain.ErrInvalidRequest, Message: "请求参数错误", Retryable: false, Original: err,
		}, false)
		return
	}
	query.Q = strings.TrimSpace(query.Q)
	if query.Provider == "" {
		query.Provider = string(domain.ProviderNameAuto)
	}
	if query.Limit == 0 {
		query.Limit = 10
	}
	if query.Page == 0 {
		query.Page = 1
	}
	effectiveDebug := false
	if query.Debug != nil {
		effectiveDebug = *query.Debug
		if effectiveDebug {
			if !validDebugToken(h.options.DebugToken, c.GetHeader("X-Debug-Token")) {
				writeError(c, &domain.SearchError{
					Code: domain.ErrDebugUnauthorized, Message: "调试访问未授权", Retryable: false,
					Original: errors.New("debug token missing or invalid"),
				}, false)
				return
			}
			query.Refresh = true
		}
	}

	ctx, cancel := contextWithTimeout(c, h.options.TotalTimeout)
	defer cancel()
	request := domain.SearchRequest{
		Query: query.Q, Provider: domain.ProviderName(query.Provider), RequestID: requestID(c), Limit: query.Limit, Page: query.Page,
		Refresh: query.Refresh, Debug: effectiveDebug,
	}
	response, err := h.searcher.Search(ctx, request)
	if err != nil {
		writeError(c, err, effectiveDebug, domain.ProviderName(query.Provider))
		return
	}
	if response.Query == "" {
		response.Query = query.Q
	}
	response = prepareSearchHTTPResponse(response, domain.ProviderName(query.Provider), requestID(c), effectiveDebug)
	c.JSON(http.StatusOK, response)
}

func prepareSearchHTTPResponse(response domain.SearchResponse, requestedProvider domain.ProviderName, requestIDValue string, debug bool) domain.SearchResponse {
	if response.Provider == "" {
		response.Provider = requestedProvider
	}
	if response.Meta.RequestID == "" {
		response.Meta.RequestID = requestIDValue
	}
	if response.Results == nil {
		response.Results = make([]domain.SearchResult, 0)
	}
	if response.Warnings == nil {
		response.Warnings = make([]domain.Warning, 0)
	}
	if !debug {
		response.Debug = nil
	}
	return response
}

func validDebugToken(configured, presented string) bool {
	if configured == "" || presented == "" {
		return false
	}
	expectedHash := sha256.Sum256([]byte(configured))
	presentedHash := sha256.Sum256([]byte(presented))
	return subtle.ConstantTimeCompare(expectedHash[:], presentedHash[:]) == 1
}

func writeError(c *gin.Context, err error, debug bool, requestedProviders ...domain.ProviderName) {
	var searchErr *domain.SearchError
	if !errors.As(err, &searchErr) {
		searchErr = &domain.SearchError{
			Code: domain.ErrProviderUnavailable, Message: "服务内部错误", Retryable: true, Original: err,
		}
	}
	requestedProvider := domain.ProviderNameAuto
	if len(requestedProviders) > 0 && requestedProviders[0] != "" {
		requestedProvider = requestedProviders[0]
	}
	body := errorResponse{
		Error: errorBody{Code: searchErr.Code, Message: searchErr.Message, Retryable: searchErr.Retryable},
		Meta:  errorMeta{Provider: requestedProvider, RequestID: requestID(c)},
	}
	if debug {
		body.Error.OriginalError = searchErr.Error()
		body.Debug = &domain.Debug{
			Attempts:     append([]domain.Attempt(nil), searchErr.Attempts...),
			RawArtifacts: append([]string(nil), searchErr.Artifacts...),
		}
	}
	c.AbortWithStatusJSON(statusForCode(searchErr.Code), body)
}

func statusForCode(code domain.ErrorCode) int {
	switch code {
	case domain.ErrInvalidRequest:
		return http.StatusBadRequest
	case domain.ErrDebugUnauthorized, domain.ErrUnsafeURL:
		return http.StatusForbidden
	case domain.ErrUnsupportedContentType:
		return http.StatusUnsupportedMediaType
	case domain.ErrContentTooLarge:
		return http.StatusRequestEntityTooLarge
	case domain.ErrExtractionFailed:
		return http.StatusUnprocessableEntity
	case domain.ErrInsufficientReadableResults:
		return http.StatusUnprocessableEntity
	case domain.ErrProviderNotFound:
		return http.StatusNotFound
	case domain.ErrRateLimited:
		return http.StatusTooManyRequests
	case domain.ErrUpstreamChanged:
		return http.StatusBadGateway
	case domain.ErrCaptchaRequired, domain.ErrProviderUnavailable:
		return http.StatusServiceUnavailable
	case domain.ErrUpstreamTimeout, domain.ErrFetchTimeout:
		return http.StatusGatewayTimeout
	case domain.ErrFetchFailed:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}
