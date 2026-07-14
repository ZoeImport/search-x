package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"web-search-backend/internal/domain"
	"web-search-backend/webui"
)

// Searcher executes one normalized web search request.
type Searcher interface {
	// Search executes one search request.
	Search(context.Context, domain.SearchRequest) (domain.SearchResponse, error)
}

// Reader reads and normalizes one public web resource.
type Reader interface {
	// Read executes one content-read request.
	Read(context.Context, domain.ReadRequest) (domain.ReadResponse, error)
}

// ContentSearcher executes one advanced search with readable-body orchestration.
type ContentSearcher interface {
	// Search executes one advanced search request.
	Search(context.Context, domain.SearchContentRequest) (domain.SearchContentResponse, error)
}

// Options configures HTTP routing, diagnostics, rate limiting, and reading.
type Options struct {
	// Ready reports whether at least one search profile and required persistence are available.
	Ready func() bool
	// Reader enables the single-URL read endpoint.
	Reader Reader
	// ContentSearcher enables combined search and readable-body orchestration.
	ContentSearcher ContentSearcher
	// Debug records whether the server runs in development mode.
	Debug bool
	// DebugToken authorizes request-scoped raw diagnostics.
	DebugToken string
	// TotalTimeout bounds lightweight search and single-URL read handlers.
	TotalTimeout time.Duration
	// ContentTimeout bounds combined search and readable-body handlers.
	ContentTimeout time.Duration
	// ClientRate is the per-client request refill rate.
	ClientRate float64
	// ClientBurst is the per-client request burst capacity.
	ClientBurst int
	// TrustedProxies contains Gin-compatible trusted proxy networks.
	TrustedProxies []string
	// Logger receives request and failure diagnostics.
	Logger *slog.Logger
}

type handler struct {
	searcher        Searcher
	reader          Reader
	contentSearcher ContentSearcher
	options         Options
}

// NewRouter creates the Gin engine and registers enabled API and UI routes.
func NewRouter(searcher Searcher, options Options) (*gin.Engine, error) {
	if searcher == nil {
		return nil, fmt.Errorf("searcher is nil")
	}
	if options.TotalTimeout <= 0 {
		options.TotalTimeout = 20 * time.Second
	}
	if options.ContentTimeout <= 0 {
		options.ContentTimeout = 30 * time.Second
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
	h := &handler{searcher: searcher, reader: options.Reader, contentSearcher: options.ContentSearcher, options: options}
	router.Use(
		requestIDMiddleware(),
		requestLogMiddleware(options.Logger),
		clientRateLimitMiddleware(options.ClientRate, options.ClientBurst, false),
		gin.CustomRecovery(func(c *gin.Context, recovered any) {
			options.Logger.Error("panic recovered", "request_id", requestID(c), "panic", recovered)
			writeError(c, &domain.SearchError{
				Code: domain.ErrProviderUnavailable, Message: "服务内部错误", Retryable: true,
				Original: fmt.Errorf("panic: %v", recovered),
			}, false)
		}),
	)
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	router.GET("/readyz", func(c *gin.Context) {
		if options.Ready != nil && !options.Ready() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	router.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusTemporaryRedirect, "/ui/")
	})
	router.StaticFS("/ui", http.FS(webui.Assets))
	router.GET("/v1/search", h.search)
	router.POST("/v1/search", h.searchPost)
	if h.reader != nil {
		router.POST("/v1/read", h.read)
	}
	return router, nil
}
