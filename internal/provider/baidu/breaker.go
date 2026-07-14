package baidu

import (
	"sync"
	"time"

	"web-search-backend/internal/detector"
)

type Breaker struct {
	mu        sync.Mutex
	now       func() time.Time
	openUntil map[string]time.Time
}

func NewBreaker(now func() time.Time) *Breaker {
	if now == nil {
		now = time.Now
	}
	return &Breaker{now: now, openUntil: make(map[string]time.Time)}
}

func (b *Breaker) Allow(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	until, exists := b.openUntil[name]
	if !exists {
		return true
	}
	if !b.now().Before(until) {
		delete(b.openUntil, name)
		return true
	}
	return false
}

func (b *Breaker) Trip(name string, classification detector.Classification) {
	var cooldown time.Duration
	switch classification {
	case detector.Captcha:
		cooldown = 30 * time.Minute
	case detector.RateLimited:
		cooldown = 5 * time.Minute
	default:
		return
	}
	b.mu.Lock()
	b.openUntil[name] = b.now().Add(cooldown)
	b.mu.Unlock()
}
