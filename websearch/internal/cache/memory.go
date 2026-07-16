package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"web-search-backend/websearch/internal/domain"
)

type entry struct {
	value    domain.SearchResponse
	storedAt time.Time
}

type Memory struct {
	mu       sync.RWMutex
	entries  map[string]entry
	maxItems int
	freshTTL time.Duration
	staleTTL time.Duration
	now      func() time.Time
}

func NewMemory(maxItems int, freshTTL, staleTTL time.Duration, now func() time.Time) (*Memory, error) {
	if maxItems <= 0 {
		return nil, fmt.Errorf("cache max items must be positive")
	}
	if freshTTL <= 0 || staleTTL <= freshTTL {
		return nil, fmt.Errorf("cache TTLs must satisfy 0 < fresh < stale")
	}
	if now == nil {
		now = time.Now
	}
	return &Memory{
		entries:  make(map[string]entry),
		maxItems: maxItems,
		freshTTL: freshTTL,
		staleTTL: staleTTL,
		now:      now,
	}, nil
}

func (m *Memory) GetFresh(ctx context.Context, key string) (domain.SearchResponse, bool) {
	return m.get(ctx, key, m.freshTTL)
}

func (m *Memory) GetStale(ctx context.Context, key string) (domain.SearchResponse, bool) {
	return m.get(ctx, key, m.staleTTL)
}

func (m *Memory) get(ctx context.Context, key string, ttl time.Duration) (domain.SearchResponse, bool) {
	if ctx.Err() != nil {
		return domain.SearchResponse{}, false
	}
	m.mu.RLock()
	item, ok := m.entries[key]
	m.mu.RUnlock()
	if !ok || m.now().Sub(item.storedAt) > ttl {
		return domain.SearchResponse{}, false
	}
	value := cloneResponse(item.value)
	value.StoredAt = item.storedAt
	return value, true
}

func (m *Memory) Set(ctx context.Context, key string, value domain.SearchResponse) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := m.now()
	value = cloneResponse(value)
	value.StoredAt = now

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.entries[key]; !exists && len(m.entries) >= m.maxItems {
		m.evictOldestLocked()
	}
	m.entries[key] = entry{value: value, storedAt: now}
	return nil
}

func (m *Memory) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	for key, item := range m.entries {
		if oldestKey == "" || item.storedAt.Before(oldest) {
			oldestKey, oldest = key, item.storedAt
		}
	}
	if oldestKey != "" {
		delete(m.entries, oldestKey)
	}
}

func cloneResponse(value domain.SearchResponse) domain.SearchResponse {
	cloned := value
	cloned.Results = append([]domain.SearchResult(nil), value.Results...)
	cloned.Warnings = append([]domain.Warning(nil), value.Warnings...)
	if value.Debug != nil {
		debug := &domain.Debug{
			Attempts:     append([]domain.Attempt(nil), value.Debug.Attempts...),
			RawArtifacts: append([]string(nil), value.Debug.RawArtifacts...),
		}
		for i := range debug.Attempts {
			if value.Debug.Attempts[i].ResponseHeaders != nil {
				debug.Attempts[i].ResponseHeaders = make(map[string][]string, len(value.Debug.Attempts[i].ResponseHeaders))
				for key, values := range value.Debug.Attempts[i].ResponseHeaders {
					debug.Attempts[i].ResponseHeaders[key] = append([]string(nil), values...)
				}
			}
		}
		cloned.Debug = debug
	}
	return cloned
}
