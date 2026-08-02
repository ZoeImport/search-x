package bootstrap

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"web-search-backend/webfetch/internal/api/httpapi"
	"web-search-backend/webfetch/internal/app"
	"web-search-backend/webfetch/internal/config"
	readpipe "web-search-backend/webfetch/internal/fetch"
	readcache "web-search-backend/webfetch/internal/fetch/cache"
	"web-search-backend/webfetch/internal/fetch/converter"
	"web-search-backend/webfetch/internal/fetch/detector"
	"web-search-backend/webfetch/internal/fetch/extractor"
	"web-search-backend/webfetch/internal/fetch/quality"
	readreader "web-search-backend/webfetch/internal/fetch/reader"
	"web-search-backend/webfetch/internal/fetch/safeurl"
	"web-search-backend/webfetch/internal/fetch/site"
	"web-search-backend/webfetch/internal/headerprofile"
	"web-search-backend/webfetch/internal/transport/chromebrowser"
)

type App struct {
	Router *gin.Engine
	close  func()
}

func New(config config.Config, logger *slog.Logger) (*App, error) {
	if !config.Diagnostics {
		gin.SetMode(gin.ReleaseMode)
	}
	policy := safeurl.NewPolicy(nil, safeurl.Config{AllowedHosts: config.HostAllowlist})
	httpReader, err := readreader.NewHTTPReader(policy, readreader.Config{Timeout: config.HTTPTimeout, MaxBodyBytes: config.MaxBodyBytes, MaxRedirects: config.MaxRedirects, UserAgent: config.UserAgent})
	if err != nil {
		return nil, fmt.Errorf("create HTTP reader: %w", err)
	}
	closeResources := func() {}
	var browserReader readpipe.ResourceReader
	if config.BrowserEnabled {
		headers, profileErr := headerprofile.NewChromiumDesktopPool(config.UserAgent)
		if profileErr != nil {
			return nil, fmt.Errorf("create header profiles: %w", profileErr)
		}
		client, clientErr := chromebrowser.New(chromebrowser.Config{ProfileDir: config.ChromeProfileDir, ExecPath: config.ChromePath, Timeout: config.BrowserTimeout, PostLoadWait: config.BrowserWait, Headless: config.ChromeHeadless, DisableSandbox: config.ChromeNoSandbox, MaxBodyBytes: int(config.MaxBodyBytes), MaxConcurrentTabs: config.BrowserSlots, HeaderProfiles: headers})
		if clientErr != nil {
			return nil, fmt.Errorf("create browser runtime: %w", clientErr)
		}
		closeResources = client.Close
		browserReader, err = readreader.NewBrowserReader(policy, client, config.MaxBodyBytes)
		if err != nil {
			closeResources()
			return nil, err
		}
	}
	htmlExtractor, err := extractor.NewHTMLExtractor()
	if err != nil {
		closeResources()
		return nil, err
	}
	extractors, err := extractor.NewRegistry(htmlExtractor, extractor.PlainTextExtractor{})
	if err != nil {
		closeResources()
		return nil, err
	}
	converters, err := converter.NewRegistry(converter.MarkdownConverter{}, converter.TextConverter{})
	if err != nil {
		closeResources()
		return nil, err
	}
	cache, err := readcache.NewMemory(config.CacheMaxItems, config.FreshTTL, config.StaleTTL, time.Now)
	if err != nil {
		closeResources()
		return nil, err
	}
	service, err := app.NewReadService(app.ReadServiceConfig{Policy: policy, Strategies: site.NewRegistry(nil, site.GenericStrategy{}), HTTPReader: httpReader, BrowserReader: browserReader, Detector: detector.NewMIMETypeDetector(), Extractors: extractors, Evaluator: quality.NewArticleQualityEvaluator(200), Converters: converters, Cache: cache, OperationTimeout: config.RequestTimeout, Now: time.Now})
	if err != nil {
		closeResources()
		return nil, err
	}
	router, err := httpapi.New(httpapi.Options{Reader: service, Ready: func() bool { return true }, Logger: logger, Timeout: config.RequestTimeout, MaxTimeout: config.MaxRequestTimeout, CacheBypass: config.CacheBypass, LogURLQuery: config.LogStoreURLQuery, AllowedOrigins: config.CORSAllowedOrigins, APIKey: config.APIKey})
	if err != nil {
		closeResources()
		return nil, err
	}
	return &App{Router: router, close: closeResources}, nil
}

func (app *App) Close() {
	if app != nil && app.close != nil {
		app.close()
	}
}
