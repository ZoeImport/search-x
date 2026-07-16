package baidu

import (
	"testing"
	"time"

	"web-search-backend/websearch/internal/detector"
)

func TestBreakerUsesReasonSpecificCooldown(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	breaker := NewBreaker(func() time.Time { return now })
	breaker.Trip("desktop_http", detector.Captcha)
	if breaker.Allow("desktop_http") {
		t.Fatal("captcha circuit should be open")
	}
	now = now.Add(30 * time.Minute)
	if !breaker.Allow("desktop_http") {
		t.Fatal("captcha circuit should be closed")
	}
	breaker.Trip("mobile_http", detector.RateLimited)
	now = now.Add(4 * time.Minute)
	if breaker.Allow("mobile_http") {
		t.Fatal("429 circuit should remain open")
	}
	now = now.Add(time.Minute)
	if !breaker.Allow("mobile_http") {
		t.Fatal("429 circuit should be closed")
	}
}
