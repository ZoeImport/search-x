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

// Options configures HTTP routing, diagnostics, rate limiting, and reading.
type Options struct {
	Reader         Reader
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
	reader   Reader
	options  Options
}

// NewRouter creates the Gin engine and registers enabled API and UI routes.
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
	h := &handler{searcher: searcher, reader: options.Reader, options: options}
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
	router.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusTemporaryRedirect, "/ui/")
	})
	router.StaticFS("/ui", http.FS(webui.Assets))
	router.GET("/v1/search", h.search)
	if h.reader != nil {
		router.POST("/v1/read", h.read)
	}
	return router, nil
}
