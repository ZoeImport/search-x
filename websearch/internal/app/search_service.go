package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/singleflight"

	"web-search-backend/websearch/internal/domain"
	"web-search-backend/websearch/internal/provider"
	"web-search-backend/websearch/internal/searchtrace"
)

type Cache interface {
	GetFresh(context.Context, string) (domain.SearchResponse, bool)
	GetStale(context.Context, string) (domain.SearchResponse, bool)
	Set(context.Context, string, domain.SearchResponse) error
}

type SearchService struct {
	registry  *provider.Registry
	cache     Cache
	now       func() time.Time
	group     singleflight.Group
	tracer    *searchtrace.Manager
	liveSlots chan struct{}
	queueMax  int64
	queued    atomic.Int64
}

func (s *SearchService) SetTracer(tracer *searchtrace.Manager) { s.tracer = tracer }

func (s *SearchService) ConfigureLiveGuard(inflightMax, queueMax int) {
	if inflightMax > 0 {
		s.liveSlots = make(chan struct{}, inflightMax)
	}
	if queueMax > 0 {
		s.queueMax = int64(queueMax)
	}
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
	ctx = domain.WithRequestScope(ctx, domain.RequestScope{TraceID: normalized.RequestID, RequestID: normalized.RequestID})
	if err := s.trace(ctx, normalized, "request_started", nil); err != nil {
		return domain.SearchResponse{}, err
	}
	selected, requestedProvider, err := s.selectProvider(normalized)
	if err != nil {
		original := err
		return domain.SearchResponse{}, &domain.SearchError{
			Code: domain.ErrProviderNotFound, Message: "Provider 未注册", Retryable: false, Original: original,
		}
	}
	key := cacheKey(normalized)
	if !normalized.Refresh {
		if cached, ok := s.cache.GetFresh(ctx, key); ok {
			_ = s.trace(ctx, normalized, "cache_hit", map[string]any{"cache_state": "fresh"})
			return s.prepareFresh(cached, requestedProvider, normalized.RequestID, started), nil
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
		release, acquireErr := s.acquireLive(ctx)
		if acquireErr != nil {
			return domain.SearchResponse{}, acquireErr
		}
		defer release()
		response, providerErr := selected.Search(ctx, normalized)
		if providerErr == nil {
			if response.Meta.RequestedProvider == "" {
				response.Meta.RequestedProvider = requestedProvider
			}
			if response.Results == nil {
				response.Results = make([]domain.SearchResult, 0)
			}
			annotateResultProviders(&response)
			if response.Warnings == nil {
				response.Warnings = make([]domain.Warning, 0)
			}
			cacheValue := response
			cacheValue.Debug = nil
			if cacheErr := s.cache.Set(ctx, key, cacheValue); cacheErr != nil {
				response.Warnings = append(response.Warnings, domain.Warning{Code: domain.WarningCodeCacheWriteError, Message: "搜索结果缓存写入失败，本次仍返回实时结果"})
			}
			return response, nil
		}
		if stale, ok := s.cache.GetStale(ctx, key); ok {
			return s.prepareStale(stale, requestedProvider, normalized.RequestID, providerErr, normalized.Debug), nil
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
			_ = s.trace(ctx, normalized, "request_failed", map[string]any{"error": result.Err.Error()})
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
		if err := s.trace(ctx, normalized, "request_finished", map[string]any{"selected_provider": response.Provider, "result_count": len(response.Results), "cached": response.Meta.Cached, "total_ms": response.Meta.TookMS}); err != nil {
			return domain.SearchResponse{}, err
		}
		return response, nil
	}
}

func (s *SearchService) selectProvider(request domain.SearchRequest) (provider.Provider, domain.ProviderName, error) {
	if request.Provider != "" && request.Provider != domain.ProviderNameAuto {
		selected, ok := s.registry.Get(request.Provider)
		if !ok {
			return nil, request.Provider, fmt.Errorf("provider %q is not registered", request.Provider)
		}
		return selected, request.Provider, nil
	}
	if len(request.Providers) == 0 {
		selected, ok := s.registry.Get(domain.ProviderNameAuto)
		if !ok {
			return nil, domain.ProviderNameAuto, fmt.Errorf("provider %q is not registered", domain.ProviderNameAuto)
		}
		return selected, domain.ProviderNameAuto, nil
	}
	members := make([]provider.Provider, 0, len(request.Providers))
	for _, name := range request.Providers {
		member, ok := s.registry.Get(name)
		if !ok {
			return nil, domain.ProviderNameAuto, fmt.Errorf("provider %q is not registered", name)
		}
		members = append(members, member)
	}
	chain, err := provider.NewChain(domain.ProviderNameAuto, members...)
	return chain, domain.ProviderNameAuto, err
}

func (s *SearchService) acquireLive(ctx context.Context) (func(), error) {
	if s.liveSlots == nil {
		return func() {}, nil
	}
	select {
	case s.liveSlots <- struct{}{}:
		return func() { <-s.liveSlots }, nil
	default:
	}
	queued := s.queued.Add(1)
	if queued > s.queueMax {
		s.queued.Add(-1)
		return nil, &domain.SearchError{Code: domain.ErrSearchQueueFull, Message: "实时搜索队列已满", Retryable: true}
	}
	select {
	case s.liveSlots <- struct{}{}:
		s.queued.Add(-1)
		return func() { <-s.liveSlots }, nil
	case <-ctx.Done():
		s.queued.Add(-1)
		return nil, &domain.SearchError{Code: domain.ErrUpstreamTimeout, Message: "等待实时搜索执行槽超时", Retryable: true, Original: ctx.Err()}
	}
}

func (s *SearchService) trace(ctx context.Context, request domain.SearchRequest, eventType string, fields map[string]any) error {
	if s.tracer == nil {
		return nil
	}
	hash, length, preview, stored := s.tracer.QueryMetadata(request.Query)
	err := s.tracer.Append(ctx, searchtrace.Event{TraceID: request.RequestID, RequestID: request.RequestID, Type: eventType, RequestedProvider: string(request.Provider), QueryHash: hash, QueryLength: length, QueryPreview: preview, Query: stored, Fields: fields})
	if err == nil {
		return nil
	}
	return nil
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
	annotateResultProviders(&response)
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
		Message: "实时 Provider 查询不可用，当前返回旧缓存",
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
	annotateResultProviders(&response)
	return response
}

func annotateResultProviders(response *domain.SearchResponse) {
	if response == nil {
		return
	}
	for index := range response.Results {
		if response.Results[index].Provider == "" {
			response.Results[index].Provider = response.Provider
		}
	}
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
	for index := range request.Providers {
		request.Providers[index] = domain.ProviderName(strings.ToLower(strings.TrimSpace(string(request.Providers[index]))))
	}
	if request.Provider == "" && len(request.Providers) == 0 {
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
	region, err := domain.NormalizeRegion(request.Region)
	if err != nil {
		return domain.SearchRequest{}, &domain.SearchError{Code: domain.ErrInvalidRequest, Message: "地区参数格式错误", Retryable: false, Original: err}
	}
	request.Region = region
	return request, nil
}

func cacheKey(request domain.SearchRequest) string {
	providers := make([]string, len(request.Providers))
	for index, name := range request.Providers {
		providers[index] = string(name)
	}
	return fmt.Sprintf("%s|%s|%s|%d|%d|%s", request.Provider, strings.Join(providers, ","), request.Query, request.Page, request.Limit, request.Region)
}
