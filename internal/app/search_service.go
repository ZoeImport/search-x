package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/singleflight"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/provider"
)

type Cache interface {
	GetFresh(context.Context, string) (domain.SearchResponse, bool)
	GetStale(context.Context, string) (domain.SearchResponse, bool)
	Set(context.Context, string, domain.SearchResponse) error
}

type SearchService struct {
	registry *provider.Registry
	cache    Cache
	now      func() time.Time
	group    singleflight.Group
}

func NewSearchService(registry *provider.Registry, cache Cache, now func() time.Time) *SearchService {
	if now == nil {
		now = time.Now
	}
	return &SearchService{registry: registry, cache: cache, now: now}
}

func (s *SearchService) Search(ctx context.Context, request domain.SearchRequest) (domain.SearchResponse, error) {
	started := s.now()
	normalized, err := normalizeRequest(request)
	if err != nil {
		return domain.SearchResponse{}, err
	}
	selected, ok := s.registry.Get(normalized.Provider)
	if !ok {
		original := fmt.Errorf("provider %q is not registered", normalized.Provider)
		return domain.SearchResponse{}, &domain.SearchError{
			Code: domain.ErrProviderNotFound, Message: "Provider 未注册", Retryable: false, Original: original,
		}
	}
	key := cacheKey(normalized)
	if !normalized.Refresh {
		if cached, ok := s.cache.GetFresh(ctx, key); ok {
			return s.prepareFresh(cached, normalized.Provider, normalized.RequestID, started), nil
		}
	}

	flightKey := key
	if normalized.Debug {
		flightKey += "|debug"
	}
	resultChannel := s.group.DoChan(flightKey, func() (any, error) {
		if !normalized.Refresh {
			if cached, ok := s.cache.GetFresh(ctx, key); ok {
				return cached, nil
			}
		}
		response, providerErr := selected.Search(ctx, normalized)
		if providerErr == nil {
			if response.Meta.RequestedProvider == "" {
				response.Meta.RequestedProvider = normalized.Provider
			}
			if response.Results == nil {
				response.Results = make([]domain.SearchResult, 0)
			}
			if response.Warnings == nil {
				response.Warnings = make([]domain.Warning, 0)
			}
			cacheValue := response
			cacheValue.Debug = nil
			if cacheErr := s.cache.Set(ctx, key, cacheValue); cacheErr != nil {
				response.Warnings = append(response.Warnings, domain.Warning{Code: domain.WarningCodeCacheWriteError, Message: cacheErr.Error()})
			}
			return response, nil
		}
		if stale, ok := s.cache.GetStale(ctx, key); ok {
			return s.prepareStale(stale, normalized.Provider, normalized.RequestID, providerErr, normalized.Debug), nil
		}
		return domain.SearchResponse{}, providerErr
	})

	select {
	case <-ctx.Done():
		return domain.SearchResponse{}, &domain.SearchError{
			Code: domain.ErrUpstreamTimeout, Message: "搜索请求已取消或超时", Retryable: true, Original: ctx.Err(),
		}
	case result := <-resultChannel:
		if result.Err != nil {
			return domain.SearchResponse{}, result.Err
		}
		response, ok := result.Val.(domain.SearchResponse)
		if !ok {
			return domain.SearchResponse{}, fmt.Errorf("singleflight returned %T", result.Val)
		}
		response.Meta.TookMS = s.now().Sub(started).Milliseconds()
		if response.Meta.RequestID == "" || response.Meta.Transport == "fresh_cache" {
			response.Meta.RequestID = normalized.RequestID
		}
		return response, nil
	}
}

func (s *SearchService) prepareFresh(response domain.SearchResponse, requestedProvider domain.ProviderName, requestID string, started time.Time) domain.SearchResponse {
	response.Meta.Transport = "fresh_cache"
	response.Meta.RequestedProvider = requestedProvider
	response.Meta.Cached = true
	response.Meta.Degraded = false
	response.Meta.RequestID = requestID
	response.Meta.TookMS = s.now().Sub(started).Milliseconds()
	if response.Results == nil {
		response.Results = make([]domain.SearchResult, 0)
	}
	if response.Warnings == nil {
		response.Warnings = make([]domain.Warning, 0)
	}
	return response
}

func (s *SearchService) prepareStale(response domain.SearchResponse, requestedProvider domain.ProviderName, requestID string, providerErr error, debug bool) domain.SearchResponse {
	response.Meta.Transport = "stale_cache"
	response.Meta.RequestedProvider = requestedProvider
	response.Meta.Cached = true
	response.Meta.Degraded = true
	response.Meta.RequestID = requestID
	if !response.StoredAt.IsZero() {
		response.Meta.CacheAgeSeconds = int64(s.now().Sub(response.StoredAt).Seconds())
	}
	response.Warnings = append(response.Warnings, domain.Warning{
		Code:    domain.WarningCodeLiveSearchUnavailable,
		Message: fmt.Sprintf("实时 Provider 查询不可用，当前返回旧缓存: %v", providerErr),
	})
	var searchErr *domain.SearchError
	if errors.As(providerErr, &searchErr) {
		response.Meta.FallbackCount = len(searchErr.Attempts)
		if debug {
			response.Debug = &domain.Debug{
				Attempts:     append([]domain.Attempt(nil), searchErr.Attempts...),
				RawArtifacts: append([]string(nil), searchErr.Artifacts...),
			}
		}
	}
	if response.Results == nil {
		response.Results = make([]domain.SearchResult, 0)
	}
	return response
}

func normalizeRequest(request domain.SearchRequest) (domain.SearchRequest, error) {
	request.Query = strings.Join(strings.Fields(request.Query), " ")
	if request.Query == "" {
		original := errors.New("query is required")
		return domain.SearchRequest{}, &domain.SearchError{Code: domain.ErrInvalidRequest, Message: "搜索文本不能为空", Retryable: false, Original: original}
	}
	if utf8.RuneCountInString(request.Query) > 256 {
		original := fmt.Errorf("query exceeds 256 characters")
		return domain.SearchRequest{}, &domain.SearchError{Code: domain.ErrInvalidRequest, Message: "搜索文本超过 256 个字符", Retryable: false, Original: original}
	}
	request.Provider = domain.ProviderName(strings.ToLower(strings.TrimSpace(string(request.Provider))))
	if request.Provider == "" {
		request.Provider = domain.ProviderNameAuto
	}
	if request.Limit == 0 {
		request.Limit = 10
	}
	if request.Page == 0 {
		request.Page = 1
	}
	if request.Limit < 1 || request.Limit > 20 || request.Page < 1 || request.Page > 10 {
		original := fmt.Errorf("invalid pagination: limit=%d page=%d", request.Limit, request.Page)
		return domain.SearchRequest{}, &domain.SearchError{Code: domain.ErrInvalidRequest, Message: "分页参数超出范围", Retryable: false, Original: original}
	}
	return request, nil
}

func cacheKey(request domain.SearchRequest) string {
	return fmt.Sprintf("%s|%s|%d|%d", request.Provider, request.Query, request.Page, request.Limit)
}
