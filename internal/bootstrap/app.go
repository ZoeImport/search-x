package bootstrap

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"web-search-backend/internal/api/httpapi"
	"web-search-backend/internal/app"
	"web-search-backend/internal/cache"
	"web-search-backend/internal/config"
	"web-search-backend/internal/debugartifact"
	"web-search-backend/internal/domain"
	"web-search-backend/internal/provider"
	"web-search-backend/internal/provider/baidu"
	"web-search-backend/internal/resilience"
	"web-search-backend/internal/transport"
	"web-search-backend/internal/transport/chromebrowser"
	"web-search-backend/internal/transport/httpsearch"
)

type App struct {
	Router *gin.Engine
	close  func()
}

func New(config config.Config) (*App, error) {
	artifactStore, err := debugartifact.New(config.DebugDir, config.DebugPreviewBytes)
	if err != nil {
		return nil, err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}
	baseTransport := http.DefaultTransport.(*http.Transport).Clone()
	baseTransport.MaxIdleConns = 20
	baseTransport.MaxIdleConnsPerHost = 4
	baseTransport.IdleConnTimeout = 90 * time.Second
	httpClient := &http.Client{Transport: baseTransport, Jar: jar}

	desktopClient, err := httpsearch.New(httpsearch.Config{
		Name: "desktop_http", BaseURL: config.DesktopURL, Referer: origin(config.DesktopURL),
		UserAgent: config.UserAgent, Timeout: config.DesktopTimeout, MaxBodyBytes: config.MaxBodyBytes,
	}, httpClient)
	if err != nil {
		return nil, fmt.Errorf("create desktop transport: %w", err)
	}
	mobileClient, err := httpsearch.New(httpsearch.Config{
		Name: "mobile_http", BaseURL: config.MobileURL, Referer: origin(config.MobileURL),
		UserAgent: config.UserAgent, Timeout: config.MobileTimeout, MaxBodyBytes: config.MaxBodyBytes,
	}, httpClient)
	if err != nil {
		return nil, fmt.Errorf("create mobile transport: %w", err)
	}
	chromeClient, err := chromebrowser.New(chromebrowser.Config{
		ProfileDir: config.ChromeProfileDir, ExecPath: config.ChromePath,
		Timeout: config.ChromeTimeout, Headless: config.ChromeHeadless, DisableSandbox: config.ChromeNoSandbox,
		MaxBodyBytes: int(config.MaxBodyBytes),
	}, func(request domain.SearchRequest) (string, error) {
		return baidu.BuildSearchURL(config.DesktopURL, request)
	})
	if err != nil {
		return nil, fmt.Errorf("create chromedp transport: %w", err)
	}

	transports := []transport.SearchTransport{desktopClient, mobileClient, chromeClient}
	limiter := rate.NewLimiter(rate.Limit(config.ProviderRate), config.ProviderBurst)
	jitter, err := resilience.NewJitter(config.JitterMin, config.JitterMax)
	if err != nil {
		chromeClient.Close()
		return nil, err
	}
	baiduProvider := baidu.NewProvider(transports, artifactStore, baidu.NewBreaker(time.Now), limiter, jitter)
	registry := provider.NewRegistry()
	if err := registry.Register(baiduProvider); err != nil {
		chromeClient.Close()
		return nil, err
	}
	memoryCache, err := cache.NewMemory(config.CacheMaxItems, config.FreshTTL, config.StaleTTL, time.Now)
	if err != nil {
		chromeClient.Close()
		return nil, err
	}
	searchService := app.NewSearchService(registry, memoryCache, time.Now)
	if !config.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	router, err := httpapi.NewRouter(searchService, httpapi.Options{
		Debug: config.Debug, DebugToken: config.DebugToken, TotalTimeout: config.TotalTimeout, ClientRate: config.ClientRate,
		ClientBurst: config.ClientBurst, TrustedProxies: config.TrustedProxies,
	})
	if err != nil {
		chromeClient.Close()
		return nil, err
	}
	return &App{Router: router, close: func() {
		chromeClient.Close()
		baseTransport.CloseIdleConnections()
	}}, nil
}

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
