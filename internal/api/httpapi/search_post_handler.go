package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"web-search-backend/internal/domain"
)

// searchPostErrorDebug contains authorized search and content-read diagnostics.
type searchPostErrorDebug struct {
	// SearchAttempts contains Provider transport diagnostics.
	SearchAttempts []domain.Attempt `json:"search_attempts,omitempty"`
	// ReadAttempts contains content-pipeline diagnostics.
	ReadAttempts []domain.ReadAttempt `json:"read_attempts,omitempty"`
	// RawArtifacts contains authorized local artifact paths.
	RawArtifacts []string `json:"raw_artifacts,omitempty"`
}

// searchPostErrorResponse is the advanced-search error payload.
type searchPostErrorResponse struct {
	// Error contains the stable error and optional original detail.
	Error errorBody `json:"error"`
	// Meta contains Provider and request correlation metadata.
	Meta errorMeta `json:"meta"`
	// Debug contains authorized low-level diagnostics.
	Debug *searchPostErrorDebug `json:"debug,omitempty"`
}

func (handler *handler) searchPost(c *gin.Context) {
	var request domain.SearchContentRequest
	if err := decodeStrictJSON(c, &request); err != nil {
		writeSearchPostError(c, &domain.SearchError{
			Code: domain.ErrInvalidRequest, Message: "请求参数错误", Retryable: false, Original: err,
		}, false, domain.ProviderNameAuto)
		return
	}
	effectiveDebug := request.Debug
	if effectiveDebug && !validDebugToken(handler.options.DebugToken, c.GetHeader("X-Debug-Token")) {
		writeSearchPostError(c, &domain.SearchError{
			Code: domain.ErrDebugUnauthorized, Message: "调试访问未授权", Retryable: false,
			Original: errors.New("debug token missing or invalid"),
		}, false, request.Provider)
		return
	}
	if effectiveDebug {
		request.Refresh = true
	}
	request.RequestID = requestID(c)
	normalized, err := request.Normalize()
	if err != nil {
		writeSearchPostError(c, &domain.SearchError{
			Code: domain.ErrInvalidRequest, Message: "请求参数错误", Retryable: false, Original: err,
		}, effectiveDebug, request.Provider)
		return
	}

	ctx, cancel := contextWithTimeout(c, handler.options.ContentTimeout)
	defer cancel()
	if !normalized.Content.Enabled {
		response, searchErr := handler.searcher.Search(ctx, domain.SearchRequest{
			Query: normalized.Query, Provider: normalized.Provider, RequestID: normalized.RequestID,
			Limit: normalized.Limit, Page: 1, Refresh: normalized.Refresh, Debug: normalized.Debug,
		})
		if searchErr != nil {
			writeSearchPostError(c, searchErr, effectiveDebug, normalized.Provider)
			return
		}
		if response.Query == "" {
			response.Query = normalized.Query
		}
		c.JSON(http.StatusOK, prepareSearchHTTPResponse(response, normalized.Provider, normalized.RequestID, effectiveDebug))
		return
	}
	if handler.contentSearcher == nil {
		err = errors.New("content searcher is disabled")
		writeSearchPostError(c, &domain.SearchError{
			Code: domain.ErrProviderUnavailable, Message: "组合搜索服务未启用", Retryable: false, Original: err,
		}, effectiveDebug, normalized.Provider)
		return
	}
	response, searchErr := handler.contentSearcher.Search(ctx, normalized)
	if searchErr != nil {
		writeSearchPostError(c, searchErr, effectiveDebug, normalized.Provider)
		return
	}
	if response.Query == "" {
		response.Query = normalized.Query
	}
	if response.RequestedProvider == "" {
		response.RequestedProvider = normalized.Provider
	}
	if response.Meta.RequestID == "" {
		response.Meta.RequestID = normalized.RequestID
	}
	if response.Results == nil {
		response.Results = make([]domain.SearchContentResult, 0)
	}
	if response.Failures == nil {
		response.Failures = make([]domain.SearchContentFailure, 0)
	}
	if response.Warnings == nil {
		response.Warnings = make([]domain.Warning, 0)
	}
	if !effectiveDebug {
		response.Debug = nil
	}
	c.JSON(http.StatusOK, response)
}

func decodeStrictJSON(c *gin.Context, target any) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("request body must contain one JSON object")
		}
		return err
	}
	return nil
}

func writeSearchPostError(c *gin.Context, err error, debug bool, requestedProvider domain.ProviderName) {
	var searchErr *domain.SearchError
	if !errors.As(err, &searchErr) {
		searchErr = &domain.SearchError{
			Code: domain.ErrProviderUnavailable, Message: "服务内部错误", Retryable: true, Original: err,
		}
	}
	if requestedProvider == "" {
		requestedProvider = domain.ProviderNameAuto
	}
	body := searchPostErrorResponse{
		Error: errorBody{Code: searchErr.Code, Message: searchErr.Message, Retryable: searchErr.Retryable},
		Meta:  errorMeta{Provider: requestedProvider, RequestID: requestID(c)},
	}
	if debug {
		body.Error.OriginalError = searchErr.Error()
		body.Debug = &searchPostErrorDebug{
			SearchAttempts: append([]domain.Attempt(nil), searchErr.Attempts...),
			ReadAttempts:   append([]domain.ReadAttempt(nil), searchErr.ReadAttempts...),
			RawArtifacts:   append([]string(nil), searchErr.Artifacts...),
		}
	}
	c.AbortWithStatusJSON(statusForCode(searchErr.Code), body)
}
