package bootstrap

import (
	"fmt"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"

	"web-search-backend/internal/api/httpapi"
	"web-search-backend/internal/app"
	"web-search-backend/internal/cache"
	"web-search-backend/internal/config"
	"web-search-backend/internal/debugartifact"
	"web-search-backend/internal/domain"
	"web-search-backend/internal/headerprofile"
	readpipe "web-search-backend/internal/read"
	readcache "web-search-backend/internal/read/cache"
	"web-search-backend/internal/read/converter"
	"web-search-backend/internal/read/detector"
	"web-search-backend/internal/read/extractor"
	"web-search-backend/internal/read/quality"
	readreader "web-search-backend/internal/read/reader"
	"web-search-backend/internal/read/safeurl"
	"web-search-backend/internal/searchquality"
	"web-search-backend/internal/transport/chromebrowser"
)

// App owns the HTTP router and its shared browser resources.
type App struct {
	Router *gin.Engine
	close  func()
}

// New assembles the search and optional read pipelines.
func New(config config.Config) (*App, error) {
	headerProfiles, err := headerprofile.NewChromiumDesktopPool(config.UserAgent)
	if err != nil {
		return nil, fmt.Errorf("create header profile pool: %w", err)
	}
	artifactStore, err := debugartifact.New(config.DebugDir, config.DebugPreviewBytes)
	if err != nil {
		return nil, err
	}
	searchRuntime, err := newSearchRuntime(config, artifactStore, headerProfiles)
	if err != nil {
		return nil, fmt.Errorf("create search runtime: %w", err)
	}
	var readChromeClient *chromebrowser.Client
	closeResources := func() {
		if readChromeClient != nil {
			readChromeClient.Close()
		}
		searchRuntime.Close()
	}
	memoryCache, err := cache.NewMemory(config.CacheMaxItems, config.FreshTTL, config.StaleTTL, time.Now)
	if err != nil {
		closeResources()
		return nil, err
	}
	searchService := app.NewSearchService(searchRuntime.registry, memoryCache, time.Now)
	searchService.SetTracer(searchRuntime.tracer)
	searchService.ConfigureLiveGuard(config.GlobalInflightMax, config.AutoQueueMax)
	qualityEvaluator := searchquality.NewEvaluator(searchquality.DefaultConfig())
	qualitySelector, err := app.NewSingleProviderSelector(searchService, qualityEvaluator)
	if err != nil {
		closeResources()
		return nil, fmt.Errorf("create single Provider selector: %w", err)
	}

	var readService *app.ReadService
	var contentSearchService *app.SearchContentService
	if config.ReadEnabled {
		readPolicy := safeurl.NewPolicy(nil, safeurl.Config{AllowedHosts: config.ReadHostAllowlist})
		httpReader, err := readreader.NewHTTPReader(readPolicy, readreader.Config{
			Timeout: config.ReadHTTPTimeout, MaxBodyBytes: config.ReadMaxBodyBytes,
			MaxRedirects: config.ReadMaxRedirects, UserAgent: config.UserAgent,
		})
		if err != nil {
			closeResources()
			return nil, fmt.Errorf("create read HTTP reader: %w", err)
		}
		var browserReader readpipe.ResourceReader
		if config.ReadBrowserEnabled {
			readChromeClient, err = chromebrowser.New(chromebrowser.Config{
				ProfileDir: config.ReadChromeProfileDir, ExecPath: config.ChromePath,
				Timeout: config.ReadBrowserTimeout, PostLoadWait: config.ReadBrowserWait,
				Headless: config.ChromeHeadless, DisableSandbox: config.ChromeNoSandbox,
				MaxBodyBytes: int(config.ReadMaxBodyBytes), MaxConcurrentTabs: config.ReadBrowserSlots,
				HeaderProfiles: headerProfiles,
			}, func(request domain.SearchRequest) (string, error) {
				return request.Query, nil
			})
			if err != nil {
				closeResources()
				return nil, fmt.Errorf("create read chromedp transport: %w", err)
			}
			browserReader, err = readreader.NewBrowserReader(readPolicy, readChromeClient, config.ReadMaxBodyBytes)
			if err != nil {
				closeResources()
				return nil, fmt.Errorf("create browser reader: %w", err)
			}
		}
		htmlExtractor, err := extractor.NewHTMLExtractor()
		if err != nil {
			closeResources()
			return nil, fmt.Errorf("create HTML extractor: %w", err)
		}
		extractors, err := extractor.NewRegistry(htmlExtractor, extractor.PlainTextExtractor{})
		if err != nil {
			closeResources()
			return nil, fmt.Errorf("create read extractor registry: %w", err)
		}
		converters, err := converter.NewRegistry(converter.MarkdownConverter{}, converter.TextConverter{})
		if err != nil {
			closeResources()
			return nil, fmt.Errorf("create read converter registry: %w", err)
		}
		readMemoryCache, err := readcache.NewMemory(config.ReadCacheMaxItems, config.ReadFreshTTL, config.ReadStaleTTL, time.Now)
		if err != nil {
			closeResources()
			return nil, fmt.Errorf("create read cache: %w", err)
		}
		readService, err = app.NewReadService(app.ReadServiceConfig{
			Policy: readPolicy, HTTPReader: httpReader, BrowserReader: browserReader,
			Detector: detector.NewMIMETypeDetector(), Extractors: extractors,
			Evaluator: quality.NewArticleQualityEvaluator(200), Converters: converters,
			Cache: readMemoryCache, OperationTimeout: config.TotalTimeout, Now: time.Now,
		})
		if err != nil {
			closeResources()
			return nil, fmt.Errorf("create read service: %w", err)
		}
		readScheduler, schedulerErr := app.NewReadScheduler(readService, 8)
		if schedulerErr != nil {
			closeResources()
			return nil, fmt.Errorf("create read scheduler: %w", schedulerErr)
		}
		contentSearchService, err = app.NewSearchContentService(qualitySelector, readScheduler, qualityEvaluator, time.Now)
		if err != nil {
			closeResources()
			return nil, fmt.Errorf("create search content service: %w", err)
		}
	}
	if !config.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	router, err := httpapi.NewRouter(searchService, httpapi.Options{
		Ready:  searchRuntime.Ready,
		Reader: readService, ContentSearcher: contentSearchService,
		Debug: config.Debug, DebugToken: config.DebugToken, TotalTimeout: config.TotalTimeout, ContentTimeout: config.ContentTimeout, ClientRate: config.ClientRate,
		ClientBurst: config.ClientBurst, TrustedProxies: config.TrustedProxies,
	})
	if err != nil {
		closeResources()
		return nil, err
	}
	return &App{Router: router, close: closeResources}, nil
}

// Close releases browser processes and idle HTTP connections.
func (a *App) Close() {
	if a != nil && a.close != nil {
		a.close()
	}
}

func origin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host + "/"
}
