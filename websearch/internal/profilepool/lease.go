package profilepool

import (
	"context"
	"sync"
	"time"

	"web-search-backend/websearch/internal/domain"
)

// Lease is one exclusive reservation against a profile capacity slot.
type Lease struct {
	pool       *Pool
	entry      *entry
	acquiredAt time.Time
	once       sync.Once
}

// ProfileID identifies the reserved profile without exposing local paths.
func (lease *Lease) ProfileID() string { return lease.entry.profile.ID() }

// Provider identifies the reserved provider.
func (lease *Lease) Provider() domain.ProviderName { return lease.entry.profile.Provider() }
func (lease *Lease) AcquiredAt() time.Time         { return lease.acquiredAt }
func (lease *Lease) Snapshot() Snapshot {
	lease.pool.mu.Lock()
	defer lease.pool.mu.Unlock()
	return snapshotEntry(lease.entry)
}

// Search executes through the reserved profile.
func (lease *Lease) Search(ctx context.Context, request domain.SearchRequest) (domain.SearchResponse, error) {
	if lease.pool.config.Limiter != nil {
		if err := lease.pool.config.Limiter.Wait(ctx); err != nil {
			return domain.SearchResponse{}, &domain.SearchError{Code: domain.ErrUpstreamTimeout, Message: "等待 Provider 全局速率配额超时", Retryable: true, Original: err}
		}
	}
	return lease.entry.profile.Search(ctx, request)
}

// Release reports the result and returns the capacity slot exactly once.
func (lease *Lease) Release(result Result) error {
	var releaseErr error
	lease.once.Do(func() {
		releaseErr = lease.pool.release(lease.entry, result)
	})
	return releaseErr
}

func (pool *Pool) release(value *entry, result Result) error {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if value.inFlight > 0 {
		value.inFlight--
	}
	if value.state == StateDraining && value.inFlight == 0 {
		value.state = StateRetired
		err := pool.saveManifest(value)
		pool.signal()
		return err
	}
	profileRelevant := result.Succeeded || result.Classification != domain.ClassificationParseChanged
	if !profileRelevant {
		pool.signal()
		return nil
	}
	value.effectiveSamples++
	observation := 0.0
	if result.Succeeded {
		value.successWeight++
		value.consecutiveFailures = 0
		observation = 1
	} else {
		value.failureWeight += failureWeight(result.Classification)
		value.consecutiveFailures++
	}
	alpha := pool.config.Trust.RecentAlpha
	value.recentEWMA = alpha*observation + (1-alpha)*value.recentEWMA
	if result.Classification == domain.ClassificationCaptcha || result.Classification == domain.ClassificationRateLimited {
		value.state = StateQuarantined
		value.quarantinedUntil = pool.config.Now().Add(pool.config.QuarantineCooldown)
	} else if value.consecutiveFailures >= pool.config.Trust.MaxConsecutiveFailures {
		value.state = StateDegraded
		value.quarantinedUntil = pool.config.Now().Add(pool.config.DegradedCooldown)
	} else if value.state == StateProbation && value.effectiveSamples >= pool.config.Trust.MinSamples &&
		successRate(value.successWeight, value.failureWeight) >= pool.config.Trust.MinSuccessRate &&
		value.recentEWMA >= pool.config.Trust.MinRecentEWMA {
		value.state = StateTrusted
	}
	err := pool.saveManifest(value)
	pool.signal()
	return err
}

func failureWeight(classification domain.Classification) float64 {
	switch classification {
	case domain.ClassificationTimeout, domain.ClassificationNetworkError:
		return 0.25
	case domain.ClassificationParseChanged:
		return 0
	case domain.ClassificationRateLimited:
		return 0.75
	case domain.ClassificationCaptcha, domain.ClassificationBlocked:
		return 1
	default:
		return 1
	}
}
