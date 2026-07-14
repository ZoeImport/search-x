// Package routing provides capacity-aware provider routing.
package routing

import (
	"context"
	"errors"
	"time"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/profilepool"
	"web-search-backend/internal/searchtrace"
)

// Pool is the provider-profile capacity boundary required by the router.
type Pool interface {
	Provider() domain.ProviderName
	TryAcquire(string) (*profilepool.Lease, bool)
	TryAcquireAuto(string) (*profilepool.Lease, bool)
	Acquire(context.Context, string) (*profilepool.Lease, error)
	AcquireAuto(context.Context, string) (*profilepool.Lease, error)
	WaitAvailable(context.Context) error
	Snapshot() profilepool.ProviderSnapshot
}

// PooledProvider exposes one explicit Provider backed by a profile pool.
type PooledProvider struct {
	pool   Pool
	wait   time.Duration
	now    func() time.Time
	tracer *searchtrace.Manager
}

func (provider *PooledProvider) SetTracer(tracer *searchtrace.Manager) { provider.tracer = tracer }

func NewPooledProvider(pool Pool, wait time.Duration, now func() time.Time) *PooledProvider {
	if wait <= 0 {
		wait = 5 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &PooledProvider{pool: pool, wait: wait, now: now}
}

func (provider *PooledProvider) Name() domain.ProviderName { return provider.pool.Provider() }

func (provider *PooledProvider) Search(ctx context.Context, request domain.SearchRequest) (domain.SearchResponse, error) {
	started := provider.now()
	waitCtx, cancel := context.WithTimeout(ctx, provider.wait)
	defer cancel()
	lease, err := provider.pool.Acquire(waitCtx, request.RequestID)
	if err != nil {
		return domain.SearchResponse{}, &domain.SearchError{Code: domain.ErrProviderBusy, Message: "指定 Provider 当前繁忙", Retryable: true, Original: err}
	}
	leaseID := request.RequestID + "-" + lease.ProfileID() + "-explicit"
	if provider.tracer != nil {
		if traceErr := provider.tracer.Append(ctx, searchtrace.Event{TraceID: request.RequestID, RequestID: request.RequestID, Type: "route_decision", RequestedProvider: string(request.Provider), Provider: string(provider.Name()), ProfileID: lease.ProfileID(), Fields: map[string]any{"route_reason": domain.RouteReasonExplicit}}); traceErr != nil {
			lease.Release(profilepool.Result{Classification: domain.ClassificationNetworkError, FinishedAt: provider.now()})
			return domain.SearchResponse{}, traceError(traceErr)
		}
		event := searchtrace.Event{TraceID: request.RequestID, RequestID: request.RequestID, Type: "lease_acquired", RequestedProvider: string(request.Provider), Provider: string(provider.Name()), ProfileID: lease.ProfileID(), LeaseID: leaseID}
		if traceErr := provider.tracer.Append(ctx, event); traceErr != nil {
			lease.Release(profilepool.Result{Classification: domain.ClassificationNetworkError, FinishedAt: provider.now()})
			return domain.SearchResponse{}, traceError(traceErr)
		}
	}
	response, searchErr := lease.Search(ctx, request)
	if releaseErr := lease.Release(resultFor(response, searchErr)); releaseErr != nil {
		return domain.SearchResponse{}, traceError(releaseErr)
	}
	if provider.tracer != nil {
		result := resultFor(response, searchErr)
		if traceErr := provider.tracer.Append(ctx, searchtrace.Event{TraceID: request.RequestID, RequestID: request.RequestID, Type: "provider_attempt", RequestedProvider: string(request.Provider), Provider: string(provider.Name()), ProfileID: lease.ProfileID(), LeaseID: leaseID, Classification: string(result.Classification), Fields: map[string]any{"result_count": len(response.Results)}}); traceErr != nil {
			return domain.SearchResponse{}, traceError(traceErr)
		}
		if traceErr := provider.tracer.Append(ctx, searchtrace.Event{TraceID: request.RequestID, RequestID: request.RequestID, Type: "lease_released", RequestedProvider: string(request.Provider), Provider: string(provider.Name()), ProfileID: lease.ProfileID(), LeaseID: leaseID, Classification: string(result.Classification)}); traceErr != nil {
			return domain.SearchResponse{}, traceError(traceErr)
		}
	}
	if searchErr != nil {
		return domain.SearchResponse{}, searchErr
	}
	annotateDebugAttempts(&response, provider.Name(), lease.ProfileID(), leaseID, 1)
	response.Provider = provider.Name()
	response.Meta.SelectedProvider = provider.Name()
	response.Meta.RouteReason = domain.RouteReasonExplicit
	response.Meta.ProfileID = lease.ProfileID()
	response.Meta.ProviderQueueMS = provider.now().Sub(started).Milliseconds()
	return response, nil
}

func resultFor(response domain.SearchResponse, err error) profilepool.Result {
	result := profilepool.Result{Succeeded: err == nil && len(response.Results) > 0, FinishedAt: time.Now()}
	if err == nil {
		if len(response.Results) == 0 {
			result.Classification = domain.ClassificationEmpty
		} else {
			result.Classification = domain.ClassificationNormal
		}
		return result
	}
	var searchErr *domain.SearchError
	if errors.As(err, &searchErr) {
		switch searchErr.Code {
		case domain.ErrCaptchaRequired:
			result.Classification = domain.ClassificationCaptcha
		case domain.ErrRateLimited:
			result.Classification = domain.ClassificationRateLimited
		case domain.ErrUpstreamChanged:
			result.Classification = domain.ClassificationParseChanged
		case domain.ErrUpstreamTimeout:
			result.Classification = domain.ClassificationTimeout
		default:
			result.Classification = domain.ClassificationNetworkError
		}
	}
	return result
}
