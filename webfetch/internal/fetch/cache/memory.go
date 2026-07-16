// Package cache provides read-document cache implementations.
package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"web-search-backend/webfetch/internal/domain"
)

type entry struct {
	document domain.ReadDocument
	storedAt time.Time
}

// Memory is a bounded concurrency-safe in-memory read-document cache.
type Memory struct {
	mu       sync.RWMutex
	entries  map[string]entry
	maxItems int
	freshTTL time.Duration
	staleTTL time.Duration
	now      func() time.Time
}

// NewMemory validates limits and creates an empty read-document cache.
func NewMemory(maxItems int, freshTTL, staleTTL time.Duration, now func() time.Time) (*Memory, error) {
	if maxItems <= 0 {
		return nil, fmt.Errorf("read cache max items must be positive")
	}
	if freshTTL <= 0 || staleTTL <= freshTTL {
		return nil, fmt.Errorf("read cache TTLs must satisfy 0 < fresh < stale")
	}
	if now == nil {
		now = time.Now
	}
	return &Memory{
		entries: make(map[string]entry), maxItems: maxItems,
		freshTTL: freshTTL, staleTTL: staleTTL, now: now,
	}, nil
}

// GetFresh returns a document only while its fresh TTL is valid.
func (m *Memory) GetFresh(ctx context.Context, key string) (domain.ReadDocument, bool) {
	return m.get(ctx, key, m.freshTTL)
}

// GetStale returns a document while its stale TTL is valid.
func (m *Memory) GetStale(ctx context.Context, key string) (domain.ReadDocument, bool) {
	return m.get(ctx, key, m.staleTTL)
}

func (m *Memory) get(ctx context.Context, key string, ttl time.Duration) (domain.ReadDocument, bool) {
	if ctx.Err() != nil {
		return domain.ReadDocument{}, false
	}
	m.mu.RLock()
	item, ok := m.entries[key]
	m.mu.RUnlock()
	if !ok || m.now().Sub(item.storedAt) > ttl {
		return domain.ReadDocument{}, false
	}
	document := item.document
	document.StoredAt = item.storedAt
	return document, true
}

// Set stores one format-independent canonical read document.
func (m *Memory) Set(ctx context.Context, key string, document domain.ReadDocument) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	storedAt := m.now()
	document.StoredAt = storedAt
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.entries[key]; !exists && len(m.entries) >= m.maxItems {
		m.evictOldestLocked()
	}
	m.entries[key] = entry{document: document, storedAt: storedAt}
	return nil
}

func (m *Memory) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	for key, item := range m.entries {
		if oldestKey == "" || item.storedAt.Before(oldest) {
			oldestKey = key
			oldest = item.storedAt
		}
	}
	if oldestKey != "" {
		delete(m.entries, oldestKey)
	}
}
