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
	"web-search-backend/internal/provider/bing"
	"web-search-backend/internal/provider/duckduckgo"
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
	baiduHTTPClient := &http.Client{Transport: baseTransport, Jar: jar}
	duckDuckGoHTTPClient := &http.Client{Transport: baseTransport}

	desktopClient, err := httpsearch.New(httpsearch.Config{
		Name: domain.TransportNameDesktopHTTP, BaseURL: config.DesktopURL, Referer: origin(config.DesktopURL),
		UserAgent: config.UserAgent, Timeout: config.DesktopTimeout, MaxBodyBytes: config.MaxBodyBytes,
	}, baiduHTTPClient)
	if err != nil {
		return nil, fmt.Errorf("create desktop transport: %w", err)
	}
	mobileClient, err := httpsearch.New(httpsearch.Config{
		Name: domain.TransportNameMobileHTTP, BaseURL: config.MobileURL, Referer: origin(config.MobileURL),
		UserAgent: config.UserAgent, Timeout: config.MobileTimeout, MaxBodyBytes: config.MaxBodyBytes,
	}, baiduHTTPClient)
	if err != nil {
		return nil, fmt.Errorf("create mobile transport: %w", err)
	}
	duckDuckGoProvider, err := duckduckgo.New(duckduckgo.Config{
		BaseURL: config.DuckDuckGoURL, UserAgent: config.UserAgent,
		Timeout: config.DuckDuckGoTimeout, MaxBodyBytes: config.MaxBodyBytes,
	}, duckDuckGoHTTPClient)
	if err != nil {
		return nil, fmt.Errorf("create DuckDuckGo provider: %w", err)
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
	bingChromeClient, err := chromebrowser.New(chromebrowser.Config{
		ProfileDir: config.BingProfileDir, ExecPath: config.ChromePath,
		Timeout: config.BingTimeout, Headless: config.ChromeHeadless, DisableSandbox: config.ChromeNoSandbox,
		MaxBodyBytes: int(config.MaxBodyBytes),
	}, func(request domain.SearchRequest) (string, error) {
		return bing.BuildSearchURL(config.BingURL, request)
	})
	if err != nil {
		chromeClient.Close()
		return nil, fmt.Errorf("create Bing chromedp transport: %w", err)
	}
	closeBrowsers := func() {
		bingChromeClient.Close()
		chromeClient.Close()
	}

	transports := []transport.SearchTransport{desktopClient, mobileClient, chromeClient}
	limiter := rate.NewLimiter(rate.Limit(config.ProviderRate), config.ProviderBurst)
	jitter, err := resilience.NewJitter(config.JitterMin, config.JitterMax)
	if err != nil {
		closeBrowsers()
		return nil, err
	}
	baiduProvider := baidu.NewProvider(transports, artifactStore, baidu.NewBreaker(time.Now), limiter, jitter)
	bingProvider, err := bing.New(bingChromeClient, artifactStore)
	if err != nil {
		closeBrowsers()
		return nil, err
	}
	autoProvider, err := provider.NewChain(domain.ProviderNameAuto, baiduProvider, duckDuckGoProvider, bingProvider)
	if err != nil {
		closeBrowsers()
		return nil, err
	}
	registry := provider.NewRegistry()
	for _, searchProvider := range []provider.Provider{baiduProvider, duckDuckGoProvider, bingProvider, autoProvider} {
		if err := registry.Register(searchProvider); err != nil {
			closeBrowsers()
			return nil, err
		}
	}
	memoryCache, err := cache.NewMemory(config.CacheMaxItems, config.FreshTTL, config.StaleTTL, time.Now)
	if err != nil {
		closeBrowsers()
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
		closeBrowsers()
		return nil, err
	}
	return &App{Router: router, close: func() {
		closeBrowsers()
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
