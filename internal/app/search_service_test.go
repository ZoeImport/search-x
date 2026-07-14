package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"web-search-backend/internal/cache"
	"web-search-backend/internal/domain"
	"web-search-backend/internal/provider"
)

type countingProvider struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
	result  domain.SearchResponse
	err     error
}

type debugAwareProvider struct {
	calls   atomic.Int32
	started chan bool
	release chan struct{}
}

func (p *debugAwareProvider) Name() domain.ProviderName { return domain.ProviderNameBaidu }
func (p *debugAwareProvider) Search(_ context.Context, request domain.SearchRequest) (domain.SearchResponse, error) {
	p.calls.Add(1)
	p.started <- request.Debug
	<-p.release
	response := domain.SearchResponse{Provider: "baidu", Results: []domain.SearchResult{{Title: "live"}}}
	if request.Debug {
		response.Debug = &domain.Debug{Attempts: []domain.Attempt{{Transport: "desktop_http"}}}
	}
	return response, nil
}

func (p *countingProvider) Name() domain.ProviderName { return domain.ProviderNameBaidu }
func (p *countingProvider) Search(context.Context, domain.SearchRequest) (domain.SearchResponse, error) {
	p.calls.Add(1)
	if p.started != nil {
		select {
		case p.started <- struct{}{}:
		default:
		}
	}
	if p.release != nil {
		<-p.release
	}
	return p.result, p.err
}

func newServiceForTest(t *testing.T, p provider.Provider, now func() time.Time) (*SearchService, *cache.Memory) {
	t.Helper()
	registry := provider.NewRegistry()
	if err := registry.Register(p); err != nil {
		t.Fatal(err)
	}
	store, err := cache.NewMemory(100, 15*time.Minute, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	return NewSearchService(registry, store, now), store
}

func TestSearchServiceCoalescesSameQuery(t *testing.T) {
	p := &countingProvider{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
		result:  domain.SearchResponse{Provider: "baidu", Results: []domain.SearchResult{{Title: "Go"}}},
	}
	service, _ := newServiceForTest(t, p, time.Now)
	request := domain.SearchRequest{Query: "golang", Provider: "baidu", RequestID: "req_1", Limit: 10, Page: 1}

	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.Search(context.Background(), request); err != nil {
				t.Errorf("search: %v", err)
			}
		}()
	}
	<-p.started
	close(p.release)
	wg.Wait()
	if got := p.calls.Load(); got != 1 {
		t.Fatalf("provider calls=%d", got)
	}
}

func TestSearchServiceReturnsStaleWithCurrentOriginalError(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	upstream := &domain.SearchError{
		Code:      domain.ErrCaptchaRequired,
		Message:   "captcha",
		Retryable: true,
		Original:  errors.New("status=200 final_url=https://wappass.baidu.com"),
		Attempts:  []domain.Attempt{{Transport: "desktop_http", Classification: "captcha", OriginalError: "raw captcha"}},
		Artifacts: []string{"desktop_http.html"},
	}
	p := &countingProvider{err: upstream}
	service, store := newServiceForTest(t, p, func() time.Time { return now })
	request := domain.SearchRequest{Query: "golang", Provider: "baidu", RequestID: "req_live", Limit: 10, Page: 1, Debug: true}
	if err := store.Set(context.Background(), cacheKey(request), domain.SearchResponse{Query: "golang", Provider: "baidu", Results: []domain.SearchResult{{Title: "cached"}}}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(16 * time.Minute)

	got, err := service.Search(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Meta.Degraded || !got.Meta.Cached || got.Meta.Transport != "stale_cache" || got.Meta.RequestID != "req_live" {
		t.Fatalf("meta=%#v", got.Meta)
	}
	if got.Debug == nil || len(got.Debug.Attempts) != 1 || got.Debug.Attempts[0].OriginalError != "raw captcha" {
		t.Fatalf("debug=%#v", got.Debug)
	}
	if len(got.Warnings) == 0 || got.Warnings[len(got.Warnings)-1].Code != "live_search_unavailable" {
		t.Fatalf("warnings=%#v", got.Warnings)
	}
	if strings.Contains(got.Warnings[len(got.Warnings)-1].Message, "百度") || !strings.Contains(got.Warnings[len(got.Warnings)-1].Message, "Provider") {
		t.Fatalf("warning=%+v", got.Warnings[len(got.Warnings)-1])
	}
}

func TestSearchServiceRefreshBypassesFreshCache(t *testing.T) {
	p := &countingProvider{result: domain.SearchResponse{
		Provider: "baidu", Results: []domain.SearchResult{{Title: "live"}},
	}}
	service, store := newServiceForTest(t, p, time.Now)
	request := domain.SearchRequest{Query: "golang", Provider: "baidu", Limit: 10, Page: 1, Refresh: true}
	if err := store.Set(context.Background(), cacheKey(request), domain.SearchResponse{
		Provider: "baidu", Results: []domain.SearchResult{{Title: "cached"}},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := service.Search(context.Background(), request)
	if err != nil || p.calls.Load() != 1 || len(got.Results) != 1 || got.Results[0].Title != "live" {
		t.Fatalf("got=%#v calls=%d err=%v", got, p.calls.Load(), err)
	}
}

func TestSearchServiceDoesNotCacheDebug(t *testing.T) {
	p := &countingProvider{result: domain.SearchResponse{
		Provider: "baidu",
		Results:  []domain.SearchResult{{Title: "live"}},
		Debug:    &domain.Debug{Attempts: []domain.Attempt{{Transport: "desktop_http"}}},
	}}
	service, _ := newServiceForTest(t, p, time.Now)
	debugRequest := domain.SearchRequest{Query: "golang", Provider: "baidu", Limit: 10, Page: 1, Refresh: true, Debug: true}
	first, err := service.Search(context.Background(), debugRequest)
	if err != nil || first.Debug == nil {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	plainRequest := debugRequest
	plainRequest.Refresh = false
	plainRequest.Debug = false
	second, err := service.Search(context.Background(), plainRequest)
	if err != nil || second.Debug != nil || second.Meta.Transport != "fresh_cache" {
		t.Fatalf("second=%#v err=%v", second, err)
	}
}

func TestSearchServiceStaleHidesDebugWhenRequestDisabled(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	p := &countingProvider{err: &domain.SearchError{
		Code: domain.ErrCaptchaRequired, Message: "captcha", Retryable: true,
		Attempts: []domain.Attempt{{Transport: "desktop_http", OriginalError: "secret"}},
	}}
	service, store := newServiceForTest(t, p, func() time.Time { return now })
	request := domain.SearchRequest{Query: "golang", Provider: "baidu", Limit: 10, Page: 1, Refresh: true, Debug: false}
	if err := store.Set(context.Background(), cacheKey(request), domain.SearchResponse{
		Provider: "baidu", Results: []domain.SearchResult{{Title: "cached"}},
	}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(16 * time.Minute)
	got, err := service.Search(context.Background(), request)
	if err != nil || got.Debug != nil || !got.Meta.Degraded {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestSearchServiceDoesNotSharePlainFlightWithDebugRequest(t *testing.T) {
	p := &debugAwareProvider{started: make(chan bool, 2), release: make(chan struct{})}
	service, _ := newServiceForTest(t, p, time.Now)
	type result struct {
		response domain.SearchResponse
		err      error
	}
	plainResult := make(chan result, 1)
	debugResult := make(chan result, 1)
	go func() {
		response, err := service.Search(context.Background(), domain.SearchRequest{
			Query: "golang", Provider: "baidu", Limit: 10, Page: 1, Refresh: true, Debug: false,
		})
		plainResult <- result{response: response, err: err}
	}()
	if debug := <-p.started; debug {
		t.Fatal("first provider call unexpectedly enabled debug")
	}
	go func() {
		response, err := service.Search(context.Background(), domain.SearchRequest{
			Query: "golang", Provider: "baidu", Limit: 10, Page: 1, Refresh: true, Debug: true,
		})
		debugResult <- result{response: response, err: err}
	}()
	secondStarted := false
	select {
	case debug := <-p.started:
		secondStarted = debug
	case <-time.After(200 * time.Millisecond):
	}
	close(p.release)
	plain := <-plainResult
	debug := <-debugResult
	if !secondStarted || p.calls.Load() != 2 || plain.err != nil || debug.err != nil || debug.response.Debug == nil {
		t.Fatalf("second_started=%v calls=%d plain=%#v debug=%#v", secondStarted, p.calls.Load(), plain, debug)
	}
}

func TestSearchServiceValidatesAndFindsProvider(t *testing.T) {
	registry := provider.NewRegistry()
	store, _ := cache.NewMemory(10, time.Minute, time.Hour, time.Now)
	service := NewSearchService(registry, store, time.Now)
	for _, request := range []domain.SearchRequest{
		{Query: "", Provider: "baidu", Limit: 10, Page: 1},
		{Query: "q", Provider: "missing", Limit: 10, Page: 1},
	} {
		_, err := service.Search(context.Background(), request)
		var searchErr *domain.SearchError
		if !errors.As(err, &searchErr) {
			t.Fatalf("request=%#v error=%v", request, err)
		}
	}
}
