package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"web-search-backend/internal/domain"
)

type Searcher interface {
	Search(context.Context, domain.SearchRequest) (domain.SearchResponse, error)
}

type Options struct {
	Debug          bool
	DebugToken     string
	TotalTimeout   time.Duration
	ClientRate     float64
	ClientBurst    int
	TrustedProxies []string
	Logger         *slog.Logger
}

type handler struct {
	searcher Searcher
	options  Options
}

func NewRouter(searcher Searcher, options Options) (*gin.Engine, error) {
	if searcher == nil {
		return nil, fmt.Errorf("searcher is nil")
	}
	if options.TotalTimeout <= 0 {
		options.TotalTimeout = 20 * time.Second
	}
	if options.ClientRate <= 0 {
		options.ClientRate = 5
	}
	if options.ClientBurst <= 0 {
		options.ClientBurst = 10
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}

	router := gin.New()
	if err := router.SetTrustedProxies(options.TrustedProxies); err != nil {
		return nil, fmt.Errorf("set trusted proxies: %w", err)
	}
	h := &handler{searcher: searcher, options: options}
	router.Use(
		requestIDMiddleware(),
		requestLogMiddleware(options.Logger),
		clientRateLimitMiddleware(options.ClientRate, options.ClientBurst, options.Debug),
		gin.CustomRecovery(func(c *gin.Context, recovered any) {
			options.Logger.Error("panic recovered", "request_id", requestID(c), "panic", recovered)
			writeError(c, &domain.SearchError{
				Code: domain.ErrProviderUnavailable, Message: "服务内部错误", Retryable: true,
				Original: fmt.Errorf("panic: %v", recovered),
			}, options.Debug)
		}),
	)
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	router.GET("/v1/search", h.search)
	return router, nil
}
