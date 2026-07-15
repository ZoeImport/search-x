package routing

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/profilepool"
	"web-search-backend/internal/searchtrace"
)

type Config struct {
	AutoWait             time.Duration
	MaxProviderAttempts  int
	MaxAgentAttempts     int
	MinimumAttemptBudget time.Duration
	Now                  func() time.Time
	AutoQueueMax         int
}

// Router strictly prefers configured providers while skipping pools without an immediately obtainable lease.
type Router struct {
	pools  []Pool
	config Config
	tracer *searchtrace.Manager
	queued atomic.Int64
}

func (router *Router) SetTracer(tracer *searchtrace.Manager) { router.tracer = tracer }

func New(pools []Pool, config Config) (*Router, error) {
	if len(pools) == 0 {
		return nil, fmt.Errorf("provider router pools are empty")
	}
	seen := make(map[domain.ProviderName]struct{}, len(pools))
	for index, pool := range pools {
		if pool == nil || pool.Provider() == "" || pool.Provider() == domain.ProviderNameAuto {
			return nil, fmt.Errorf("provider router pool %d is invalid", index)
		}
		if _, exists := seen[pool.Provider()]; exists {
			return nil, fmt.Errorf("duplicate provider router pool %q", pool.Provider())
		}
		seen[pool.Provider()] = struct{}{}
	}
	if config.AutoWait <= 0 {
		config.AutoWait = 2 * time.Second
	}
	if config.MaxProviderAttempts <= 0 {
		config.MaxProviderAttempts = 3
	}
	if config.MaxAgentAttempts <= 0 {
		config.MaxAgentAttempts = 2
	}
	if config.MinimumAttemptBudget <= 0 {
		config.MinimumAttemptBudget = 3 * time.Second
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.AutoQueueMax <= 0 {
		config.AutoQueueMax = 100
	}
	return &Router{pools: append([]Pool(nil), pools...), config: config}, nil
}

func (*Router) Name() domain.ProviderName { return domain.ProviderNameAuto }

func (router *Router) Search(ctx context.Context, request domain.SearchRequest) (domain.SearchResponse, error) {
	shadowProvider := router.shadowCandidate(nil)
	if shadowProvider != "" {
		if err := router.trace(ctx, request, searchtrace.Event{Type: "shadow_route_decision", Provider: string(shadowProvider), Fields: map[string]any{"available_slots": router.availableSlots(shadowProvider)}}); err != nil {
			return domain.SearchResponse{}, traceError(err)
		}
	}
	attempted := make(map[domain.ProviderName]struct{}, router.config.MaxProviderAttempts)
	agentAttempts := make(map[domain.ProviderName]int)
	excludedProfiles := make(map[domain.ProviderName]string)
	routeRound := 0
	var failures []error
	var failedProviders []domain.ProviderName
	for len(attempted) < router.config.MaxProviderAttempts && hasAttemptBudget(ctx, router.config.MinimumAttemptBudget, router.config.Now()) {
		pool, lease := router.tryAcquire(request.RequestID, attempted, excludedProfiles)
		if lease == nil {
			depth := router.queued.Add(1)
			if depth > int64(router.config.AutoQueueMax) {
				router.queued.Add(-1)
				return domain.SearchResponse{}, &domain.SearchError{Code: domain.ErrSearchQueueFull, Message: "自动搜索调度队列已满", Retryable: true}
			}
			waitCtx, cancel := context.WithTimeout(ctx, router.config.AutoWait)
			pool, lease = router.acquireAnyAuto(waitCtx, request.RequestID, attempted)
			cancel()
			router.queued.Add(-1)
			if lease == nil {
				pool, lease = router.tryAcquire(request.RequestID, attempted, excludedProfiles)
			}
			if lease == nil {
				break
			}
		}
		routeRound++
		agentAttempts[pool.Provider()]++
		leaseID := fmt.Sprintf("%s-%s-%d", request.RequestID, lease.ProfileID(), routeRound)
		before := lease.Snapshot()
		if err := router.trace(ctx, request, searchtrace.Event{Type: "route_decision", Provider: string(pool.Provider()), ProfileID: lease.ProfileID(), Fields: map[string]any{"route_round": routeRound, "route_reason": domain.RouteReasonPriority}}); err != nil {
			lease.Release(profilepool.Result{Classification: domain.ClassificationNetworkError, FinishedAt: router.config.Now()})
			return domain.SearchResponse{}, traceError(err)
		}
		if err := router.trace(ctx, request, searchtrace.Event{Type: "lease_acquired", Provider: string(pool.Provider()), ProfileID: lease.ProfileID(), LeaseID: leaseID, Fields: profileTraceFields(before)}); err != nil {
			lease.Release(profilepool.Result{Classification: domain.ClassificationNetworkError, FinishedAt: router.config.Now()})
			return domain.SearchResponse{}, traceError(err)
		}
		providerRequest := request
		providerRequest.Provider = pool.Provider()
		response, err := lease.Search(ctx, providerRequest)
		if releaseErr := lease.Release(resultFor(response, err)); releaseErr != nil {
			return domain.SearchResponse{}, traceError(releaseErr)
		}
		result := resultFor(response, err)
		after := lease.Snapshot()
		if traceErr := router.trace(ctx, request, searchtrace.Event{Type: "provider_attempt", Provider: string(pool.Provider()), ProfileID: lease.ProfileID(), LeaseID: leaseID, Classification: string(result.Classification), Fields: map[string]any{"result_count": len(response.Results)}}); traceErr != nil {
			return domain.SearchResponse{}, traceError(traceErr)
		}
		if traceErr := router.trace(ctx, request, searchtrace.Event{Type: "lease_released", Provider: string(pool.Provider()), ProfileID: lease.ProfileID(), LeaseID: leaseID, Classification: string(result.Classification), Fields: map[string]any{"hold_ms": router.config.Now().Sub(lease.AcquiredAt()).Milliseconds(), "trust_before": before.RecentEWMA, "trust_after": after.RecentEWMA, "generation": after.Generation, "state": after.State}}); traceErr != nil {
			return domain.SearchResponse{}, traceError(traceErr)
		}
		if err == nil && len(response.Results) > 0 {
			_ = router.trace(ctx, request, searchtrace.Event{Type: "shadow_route_compare", Provider: string(pool.Provider()), Fields: map[string]any{"predicted_provider": shadowProvider, "matched": shadowProvider == pool.Provider()}})
			annotateDebugAttempts(&response, pool.Provider(), lease.ProfileID(), leaseID, routeRound)
			return router.prepareSuccess(response, lease, pool.Provider(), failedProviders), nil
		}
		if err == nil {
			err = &domain.SearchError{Code: domain.ErrProviderUnavailable, Message: "Provider 未返回有效结果", Retryable: true}
		}
		if !reroutable(err) {
			return domain.SearchResponse{}, err
		}
		excludedProfiles[pool.Provider()] = lease.ProfileID()
		if agentAttempts[pool.Provider()] < router.config.MaxAgentAttempts && hasAlternativeProfile(pool, lease.ProfileID()) {
			continue
		}
		attempted[pool.Provider()] = struct{}{}
		failures = append(failures, fmt.Errorf("provider %s: %w", pool.Provider(), err))
		failedProviders = append(failedProviders, pool.Provider())
	}
	return domain.SearchResponse{}, &domain.SearchError{
		Code: domain.ErrProviderCapacityExhausted, Message: "当前没有可用的搜索 Provider 容量", Retryable: true, Original: errors.Join(failures...),
	}
}

func (router *Router) acquireAnyAuto(ctx context.Context, requestID string, attempted map[domain.ProviderName]struct{}) (Pool, *profilepool.Lease) {
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		pool  Pool
		lease *profilepool.Lease
	}
	results := make(chan result, len(router.pools))
	var accepted atomic.Bool
	workers := 0
	for _, pool := range router.pools {
		if _, skip := attempted[pool.Provider()]; skip {
			continue
		}
		workers++
		go func(candidate Pool) {
			lease, err := candidate.AcquireAuto(waitCtx, requestID)
			if err != nil {
				return
			}
			if waitCtx.Err() != nil || !accepted.CompareAndSwap(false, true) {
				_ = lease.Release(profilepool.Result{Classification: domain.ClassificationNetworkError, FinishedAt: router.config.Now()})
				return
			}
			select {
			case results <- result{pool: candidate, lease: lease}:
			case <-waitCtx.Done():
				_ = lease.Release(profilepool.Result{Classification: domain.ClassificationNetworkError, FinishedAt: router.config.Now()})
			}
		}(pool)
	}
	if workers == 0 {
		return nil, nil
	}
	select {
	case <-ctx.Done():
		accepted.Store(true)
		select {
		case late := <-results:
			_ = late.lease.Release(profilepool.Result{Classification: domain.ClassificationNetworkError, FinishedAt: router.config.Now()})
		default:
		}
		return nil, nil
	case winner := <-results:
		return winner.pool, winner.lease
	}
}

func profileTraceFields(snapshot profilepool.Snapshot) map[string]any {
	return map[string]any{"generation": snapshot.Generation, "state": snapshot.State, "trust_before": snapshot.RecentEWMA, "in_flight": snapshot.InFlight, "capacity": snapshot.Capacity}
}

func (router *Router) shadowCandidate(excluded map[domain.ProviderName]struct{}) domain.ProviderName {
	for _, pool := range router.pools {
		if _, skip := excluded[pool.Provider()]; skip {
			continue
		}
		if pool.Snapshot().AvailableSlots > 0 {
			return pool.Provider()
		}
	}
	return ""
}

func (router *Router) availableSlots(provider domain.ProviderName) int {
	for _, pool := range router.pools {
		if pool.Provider() == provider {
			return pool.Snapshot().AvailableSlots
		}
	}
	return 0
}

func annotateDebugAttempts(response *domain.SearchResponse, provider domain.ProviderName, profileID, leaseID string, routeRound int) {
	if response == nil || response.Debug == nil {
		return
	}
	for index := range response.Debug.Attempts {
		response.Debug.Attempts[index].Provider = provider
		response.Debug.Attempts[index].ProfileID = profileID
		response.Debug.Attempts[index].LeaseID = leaseID
		response.Debug.Attempts[index].RouteRound = routeRound
	}
}

func (router *Router) QueueDepth() int { return int(router.queued.Load()) }

func (router *Router) trace(ctx context.Context, request domain.SearchRequest, event searchtrace.Event) error {
	if router.tracer == nil {
		return nil
	}
	event.TraceID, event.RequestID = request.RequestID, request.RequestID
	event.RequestedProvider = string(request.Provider)
	return router.tracer.Append(ctx, event)
}

func traceError(err error) error {
	return &domain.SearchError{Code: domain.ErrTracePersistenceUnavailable, Message: "搜索链路记录暂不可用", Retryable: true, Original: err}
}

func (router *Router) prepareSuccess(response domain.SearchResponse, lease *profilepool.Lease, selected domain.ProviderName, failed []domain.ProviderName) domain.SearchResponse {
	response.Provider = selected
	response.Meta.RequestedProvider = domain.ProviderNameAuto
	response.Meta.SelectedProvider = selected
	response.Meta.ProfileID = lease.ProfileID()
	response.Meta.ProviderFallbackCount = len(failed)
	response.Meta.RouteReason = domain.RouteReasonPriority
	if len(failed) > 0 {
		response.Meta.RouteReason = domain.RouteReasonRetryReroute
		response.Meta.Degraded = true
		for _, previous := range failed {
			response.Warnings = append(response.Warnings, domain.Warning{Code: domain.WarningCodeProviderFallback, Message: fmt.Sprintf("provider %s failed; using %s", previous, selected)})
		}
	}
	return response
}

func (router *Router) tryAcquire(requestID string, attempted map[domain.ProviderName]struct{}, excludedProfiles map[domain.ProviderName]string) (Pool, *profilepool.Lease) {
	for _, pool := range router.pools {
		if _, exists := attempted[pool.Provider()]; exists {
			continue
		}
		if lease, ok := pool.TryAcquireAutoExcept(requestID, excludedProfiles[pool.Provider()]); ok {
			return pool, lease
		}
	}
	return nil, nil
}

func hasAlternativeProfile(pool Pool, excludedProfileID string) bool {
	for _, profile := range pool.Snapshot().Profiles {
		if profile.ID != excludedProfileID && (profile.State == profilepool.StateProbation || profile.State == profilepool.StateTrusted) && profile.InFlight < profile.Capacity {
			return true
		}
	}
	return false
}

func (router *Router) waitAny(ctx context.Context, attempted map[domain.ProviderName]struct{}) Pool {
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ready := make(chan Pool, 1)
	for _, pool := range router.pools {
		if _, exists := attempted[pool.Provider()]; exists {
			continue
		}
		go func(candidate Pool) {
			if candidate.WaitAvailable(waitCtx) == nil {
				select {
				case ready <- candidate:
				default:
				}
			}
		}(pool)
	}
	select {
	case <-ctx.Done():
		return nil
	case candidate := <-ready:
		return candidate
	}
}

func hasAttemptBudget(ctx context.Context, minimum time.Duration, now time.Time) bool {
	deadline, ok := ctx.Deadline()
	return !ok || deadline.Sub(now) >= minimum
}

func reroutable(err error) bool {
	var searchErr *domain.SearchError
	if !errors.As(err, &searchErr) || !searchErr.Retryable {
		return false
	}
	switch searchErr.Code {
	case domain.ErrCaptchaRequired, domain.ErrRateLimited, domain.ErrUpstreamChanged, domain.ErrProviderUnavailable, domain.ErrUpstreamTimeout:
		return true
	default:
		return false
	}
}
