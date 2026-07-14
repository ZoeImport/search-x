package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"web-search-backend/internal/domain"
)

const requestIDKey = "request_id"

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if !validRequestID.MatchString(id) {
			id = newRequestID()
		}
		c.Set(requestIDKey, id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

func newRequestID() string {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return "req_fallback"
	}
	return "req_" + hex.EncodeToString(bytes)
}

func requestID(c *gin.Context) string {
	value, _ := c.Get(requestIDKey)
	id, _ := value.(string)
	return id
}

func requestLogMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		logger.Info("http request",
			"request_id", requestID(c),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"elapsed_ms", time.Since(started).Milliseconds(),
		)
	}
}

type clientEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func clientRateLimitMiddleware(eventsPerSecond float64, burst int, debug bool) gin.HandlerFunc {
	var mu sync.Mutex
	clients := make(map[string]*clientEntry)
	var requests uint64
	return func(c *gin.Context) {
		now := time.Now()
		ip := c.ClientIP()
		mu.Lock()
		entry := clients[ip]
		if entry == nil {
			entry = &clientEntry{limiter: rate.NewLimiter(rate.Limit(eventsPerSecond), burst)}
			clients[ip] = entry
		}
		entry.lastSeen = now
		allowed := entry.limiter.Allow()
		requests++
		if requests%256 == 0 {
			for key, candidate := range clients {
				if now.Sub(candidate.lastSeen) > 10*time.Minute {
					delete(clients, key)
				}
			}
		}
		mu.Unlock()
		if !allowed {
			writeError(c, &domain.SearchError{
				Code: domain.ErrRateLimited, Message: "请求频率超过本服务限制", Retryable: true,
				Original: errors.New("client IP token bucket rejected request"),
			}, debug)
			return
		}
		c.Next()
	}
}

func contextWithTimeout(c *gin.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.Request.Context(), timeout)
}
