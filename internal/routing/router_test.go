package routing

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/profilepool"
)

type routeProfile struct {
	id       string
	provider domain.ProviderName
	response domain.SearchResponse
	err      error
}

func (profile *routeProfile) ID() string                    { return profile.id }
func (profile *routeProfile) Provider() domain.ProviderName { return profile.provider }
func (*routeProfile) Capacity() int                         { return 1 }
func (*routeProfile) Close() error                          { return nil }
func (profile *routeProfile) Search(context.Context, domain.SearchRequest) (domain.SearchResponse, error) {
	return profile.response, profile.err
}

func TestRouterUsesFirstProviderWithImmediatelyAvailableLease(t *testing.T) {
	baidu := routePool(t, &routeProfile{id: "baidu-1", provider: domain.ProviderNameBaidu, response: response(domain.ProviderNameBaidu)})
	bing := routePool(t, &routeProfile{id: "bing-1", provider: domain.ProviderNameBing, response: response(domain.ProviderNameBing)})
	held, _ := baidu.TryAcquire("held")
	router := mustRouter(t, baidu, bing)
	got, err := router.Search(context.Background(), domain.SearchRequest{RequestID: "request"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != domain.ProviderNameBing || got.Meta.ProfileID != "bing-1" {
		t.Fatalf("response=%+v", got)
	}
	held.Release(profilepool.Result{Succeeded: true})
}

func TestRouterReroutesRetryableFailure(t *testing.T) {
	baidu := routePool(t, &routeProfile{id: "baidu-1", provider: domain.ProviderNameBaidu, err: &domain.SearchError{
		Code: domain.ErrUpstreamTimeout, Message: "timeout", Retryable: true,
	}})
	bing := routePool(t, &routeProfile{id: "bing-1", provider: domain.ProviderNameBing, response: response(domain.ProviderNameBing)})
	got, err := mustRouter(t, baidu, bing).Search(context.Background(), domain.SearchRequest{RequestID: "request"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != domain.ProviderNameBing || got.Meta.ProviderFallbackCount != 1 || got.Meta.RouteReason != domain.RouteReasonRetryReroute {
		t.Fatalf("response=%+v", got)
	}
}

func TestRouterAnnotatesAuthorizedDebugAttempts(t *testing.T) {
	debugResponse := response(domain.ProviderNameBaidu)
	debugResponse.Debug = &domain.Debug{Attempts: []domain.Attempt{{Transport: domain.TransportNameDesktopHTTP}}}
	pool := routePool(t, &routeProfile{id: "baidu-debug", provider: domain.ProviderNameBaidu, response: debugResponse})
	got, err := mustRouter(t, pool).Search(context.Background(), domain.SearchRequest{RequestID: "debug", Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	attempt := got.Debug.Attempts[0]
	if attempt.Provider != domain.ProviderNameBaidu || attempt.ProfileID != "baidu-debug" || attempt.LeaseID == "" || attempt.RouteRound != 1 {
		t.Fatalf("attempt=%+v", attempt)
	}
}

func TestExplicitProviderWaitsOnlyForItsPool(t *testing.T) {
	pool := routePool(t, &routeProfile{id: "baidu-1", provider: domain.ProviderNameBaidu, response: response(domain.ProviderNameBaidu)})
	held, _ := pool.TryAcquire("held")
	provider := NewPooledProvider(pool, 10*time.Millisecond, nil)
	_, err := provider.Search(context.Background(), domain.SearchRequest{RequestID: "request"})
	var searchErr *domain.SearchError
	if !errors.As(err, &searchErr) || searchErr.Code != domain.ErrProviderBusy {
		t.Fatalf("err=%v", err)
	}
	held.Release(profilepool.Result{Succeeded: true})
}

func TestRouterRejectsWhenAutoDispatchQueueIsFull(t *testing.T) {
	pool := routePool(t, &routeProfile{id: "baidu-1", provider: domain.ProviderNameBaidu, response: response(domain.ProviderNameBaidu)})
	held, _ := pool.TryAcquire("held")
	router, err := New([]Pool{pool}, Config{AutoWait: time.Second, AutoQueueMax: 1, MinimumAttemptBudget: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan error, 1)
	go func() {
		_, err := router.Search(context.Background(), domain.SearchRequest{RequestID: "waiting"})
		waiting <- err
	}()
	deadline := time.Now().Add(time.Second)
	for router.QueueDepth() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	_, err = router.Search(context.Background(), domain.SearchRequest{RequestID: "rejected"})
	var searchErr *domain.SearchError
	if !errors.As(err, &searchErr) || searchErr.Code != domain.ErrSearchQueueFull {
		t.Fatalf("err=%v", err)
	}
	held.Release(profilepool.Result{Succeeded: true})
	if err := <-waiting; err != nil {
		t.Fatal(err)
	}
}

func TestRouterAutoWinsProductionPathAfterThreeExplicitLeases(t *testing.T) {
	pool := routePool(t, &routeProfile{id: "fair", provider: domain.ProviderNameBaidu, response: response(domain.ProviderNameBaidu)})
	explicit := NewPooledProvider(pool, time.Second, nil)
	for index := 0; index < 3; index++ {
		if _, err := explicit.Search(context.Background(), domain.SearchRequest{RequestID: fmt.Sprintf("explicit-%d", index)}); err != nil {
			t.Fatal(err)
		}
	}
	held, _ := pool.TryAcquire("held")
	router := mustRouter(t, pool)
	autoDone := make(chan error, 1)
	go func() {
		_, err := router.Search(context.Background(), domain.SearchRequest{RequestID: "auto"})
		autoDone <- err
	}()
	deadline := time.Now().Add(time.Second)
	for pool.AutoWaiters() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	explicitDone := make(chan error, 1)
	go func() {
		_, err := explicit.Search(context.Background(), domain.SearchRequest{RequestID: "explicit-four"})
		explicitDone <- err
	}()
	held.Release(profilepool.Result{Succeeded: true})
	select {
	case err := <-autoDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-explicitDone:
		t.Fatal("explicit won production race")
	case <-time.After(time.Second):
		t.Fatal("auto timed out")
	}
	select {
	case err := <-explicitDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("explicit did not resume")
	}
}

func routePool(t *testing.T, profiles ...profilepool.Profile) *profilepool.Pool {
	t.Helper()
	pool, err := profilepool.New(profiles, profilepool.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func mustRouter(t *testing.T, pools ...Pool) *Router {
	t.Helper()
	router, err := New(pools, Config{AutoWait: 10 * time.Millisecond, MinimumAttemptBudget: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func response(provider domain.ProviderName) domain.SearchResponse {
	return domain.SearchResponse{Provider: provider, Results: []domain.SearchResult{{Title: fmt.Sprintf("%s result", provider), URL: "https://example.com"}}}
}
