package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"web-search-backend/websearch/internal/config"
	"web-search-backend/websearch/internal/domain"
	"web-search-backend/websearch/internal/headerprofile"
	"web-search-backend/websearch/internal/profilepool"
	"web-search-backend/websearch/internal/provider"
	"web-search-backend/websearch/internal/provider/baidu"
	"web-search-backend/websearch/internal/provider/bing"
	"web-search-backend/websearch/internal/provider/brave"
	"web-search-backend/websearch/internal/provider/duckduckgo"
	"web-search-backend/websearch/internal/resilience"
	"web-search-backend/websearch/internal/routing"
	"web-search-backend/websearch/internal/searchtrace"
	"web-search-backend/websearch/internal/transport/chromebrowser"
	"web-search-backend/websearch/internal/transport/httpsearch"
)

type searchArtifactStore interface {
	baidu.ArtifactStore
	bing.ArtifactStore
	brave.ArtifactStore
}

type searchRuntime struct {
	registry        *provider.Registry
	pools           []*profilepool.Pool
	managers        []*profilepool.Manager
	baseTransport   *http.Transport
	closeShared     func()
	tracer          *searchtrace.Manager
	closeOnce       sync.Once
	reconcileCancel context.CancelFunc
	reconcileDone   chan struct{}
}

func newSearchRuntime(configValue config.Config, artifacts searchArtifactStore, headers headerprofile.Pool) (*searchRuntime, error) {
	if !configValue.ProfilePoolEnabled {
		configValue.BaiduProfileCount, configValue.BingProfileCount, configValue.BraveProfileCount, configValue.DuckDuckGoProfileCount = 1, 1, 1, 1
		configValue.BaiduProfileCapacity, configValue.BingProfileCapacity, configValue.BraveProfileCapacity, configValue.DuckDuckGoProfileCapacity = 1, 1, 1, 1
	}
	baseTransport := http.DefaultTransport.(*http.Transport).Clone()
	baseTransport.MaxIdleConns = 100
	baseTransport.MaxIdleConnsPerHost = 20
	baseTransport.IdleConnTimeout = 90 * time.Second

	runtimeValue := &searchRuntime{baseTransport: baseTransport}
	var err error
	fail := func(err error) (*searchRuntime, error) {
		runtimeValue.Close()
		return nil, err
	}

	runtimeValue.tracer = searchtrace.New(searchtrace.Config{Logger: slog.Default(), Diagnostics: configValue.Debug, StoreQuery: configValue.LogStoreQuery, PreviewChars: configValue.LogQueryPreviewChars})

	enabled := make(map[string]bool, len(configValue.EnabledProviders))
	for _, name := range configValue.EnabledProviders {
		enabled[name] = true
	}
	if enabled["baidu"] {
		pool, sharedClose, buildErr := buildBaiduPool(configValue, artifacts, headers, baseTransport)
		if buildErr != nil {
			return fail(buildErr)
		}
		runtimeValue.closeShared = sharedClose
		runtimeValue.pools = append(runtimeValue.pools, pool)
	}
	if enabled["bing"] {
		pool, buildErr := buildBingPool(configValue, artifacts, headers)
		if buildErr != nil {
			return fail(buildErr)
		}
		runtimeValue.pools = append(runtimeValue.pools, pool)
	}
	if enabled["brave"] {
		pool, buildErr := buildBravePool(configValue, artifacts, headers)
		if buildErr != nil {
			return fail(buildErr)
		}
		runtimeValue.pools = append(runtimeValue.pools, pool)
	}
	if enabled["duckduckgo"] {
		pool, buildErr := buildDuckDuckGoPool(configValue, headers, baseTransport)
		if buildErr != nil {
			return fail(buildErr)
		}
		runtimeValue.pools = append(runtimeValue.pools, pool)
	}
	for _, pool := range runtimeValue.pools {
		runtimeValue.managers = append(runtimeValue.managers, profilepool.NewManager(pool, nil))
	}
	reconcileCtx, reconcileCancel := context.WithCancel(context.Background())
	runtimeValue.reconcileCancel, runtimeValue.reconcileDone = reconcileCancel, make(chan struct{})
	go runtimeValue.runReconcile(reconcileCtx)

	registry := provider.NewRegistry()
	explicitProviders := make([]provider.Provider, 0, len(runtimeValue.pools))
	for _, pool := range runtimeValue.pools {
		explicit := routing.NewPooledProvider(pool, configValue.ExplicitRouteWait, time.Now)
		explicit.SetTracer(runtimeValue.tracer)
		explicitProviders = append(explicitProviders, explicit)
		if err := registry.Register(explicit); err != nil {
			return fail(err)
		}
	}
	var autoProvider provider.Provider
	if configValue.CapacityRouterEnabled {
		routerPools := make([]routing.Pool, len(runtimeValue.pools))
		for index, pool := range runtimeValue.pools {
			routerPools[index] = pool
		}
		router, routeErr := routing.New(routerPools, routing.Config{AutoWait: configValue.AutoRouteWait, MaxProviderAttempts: configValue.MaxProviderAttempts, MinimumAttemptBudget: configValue.MinimumAttemptBudget, Now: time.Now, AutoQueueMax: configValue.AutoQueueMax})
		if routeErr != nil {
			return fail(routeErr)
		}
		router.SetTracer(runtimeValue.tracer)
		autoProvider = router
	} else {
		autoProvider, err = provider.NewChain(domain.ProviderNameAuto, explicitProviders...)
		if err != nil {
			return fail(err)
		}
	}
	if err := registry.Register(autoProvider); err != nil {
		return fail(err)
	}
	runtimeValue.registry = registry
	return runtimeValue, nil
}

func (runtimeValue *searchRuntime) Close() {
	if runtimeValue == nil {
		return
	}
	runtimeValue.closeOnce.Do(func() {
		if runtimeValue.reconcileCancel != nil {
			runtimeValue.reconcileCancel()
			<-runtimeValue.reconcileDone
		}
		for _, pool := range runtimeValue.pools {
			_ = pool.Close()
		}
		if runtimeValue.closeShared != nil {
			runtimeValue.closeShared()
		}
		if runtimeValue.baseTransport != nil {
			runtimeValue.baseTransport.CloseIdleConnections()
		}
		if runtimeValue.tracer != nil {
			_ = runtimeValue.tracer.Close()
		}
	})
}

func (runtimeValue *searchRuntime) runReconcile(ctx context.Context) {
	defer close(runtimeValue.reconcileDone)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, manager := range runtimeValue.managers {
				if err := manager.Reconcile(); err != nil {
					slog.Warn("profile reconcile failed", "error", err)
				}
			}
		}
	}
}

func (runtimeValue *searchRuntime) Ready() bool {
	if runtimeValue == nil {
		return false
	}
	if runtimeValue.tracer != nil && !runtimeValue.tracer.Healthy() {
		return false
	}
	for _, pool := range runtimeValue.pools {
		if pool.Snapshot().ServingCount > 0 {
			return true
		}
	}
	return false
}

func buildBaiduPool(configValue config.Config, artifacts baidu.ArtifactStore, headers headerprofile.Pool, baseTransport *http.Transport) (*profilepool.Pool, func(), error) {
	closeShared := func() {}
	limiter := rate.NewLimiter(rate.Limit(configValue.ProviderRate), configValue.ProviderBurst)
	jitter, err := resilience.NewJitter(configValue.JitterMin, configValue.JitterMax)
	if err != nil {
		closeShared()
		return nil, nil, err
	}
	fixedHeader, err := headerprofile.NewBaiduFixedSessionProfile(configValue.UserAgent)
	if err != nil {
		closeShared()
		return nil, nil, err
	}

	count := normalizedPositive(configValue.BaiduProfileCount, 6)
	profiles := make([]profilepool.Profile, 0, count)
	closeCreated := func() {
		for _, profile := range profiles {
			_ = profile.Close()
		}
	}
	for index := 0; index < count; index++ {
		profileID := fmt.Sprintf("baidu-%04d", index+1)
		jar, createErr := cookiejar.New(nil)
		if createErr != nil {
			closeCreated()
			return nil, nil, createErr
		}
		identityClient := &http.Client{Transport: baseTransport, Jar: jar}
		desktopClient, createErr := httpsearch.New(httpsearch.Config{Name: domain.TransportNameDesktopHTTP, BaseURL: configValue.DesktopURL, Referer: origin(configValue.DesktopURL), UserAgent: configValue.UserAgent, Timeout: configValue.DesktopTimeout, MaxBodyBytes: configValue.MaxBodyBytes, HeaderProfiles: headers, HeaderProfileKey: profileID}, identityClient)
		if createErr != nil {
			closeCreated()
			return nil, nil, createErr
		}
		mobileClient, createErr := httpsearch.New(httpsearch.Config{Name: domain.TransportNameMobileHTTP, BaseURL: configValue.MobileURL, Referer: origin(configValue.MobileURL), UserAgent: configValue.UserAgent, Timeout: configValue.MobileTimeout, MaxBodyBytes: configValue.MaxBodyBytes, HeaderProfiles: headers, HeaderProfileKey: profileID}, identityClient)
		if createErr != nil {
			closeCreated()
			return nil, nil, createErr
		}
		chromeClient, createErr := chromebrowser.New(chromebrowser.Config{ProfileDir: filepath.Join(configValue.ChromeProfileDir, profileID), ExecPath: configValue.ChromePath, Timeout: configValue.ChromeTimeout, Headless: configValue.ChromeHeadless, DisableSandbox: configValue.ChromeNoSandbox, MaxBodyBytes: int(configValue.MaxBodyBytes), MaxConcurrentTabs: normalizedPositive(configValue.ProviderBrowserSlots, 2), HeaderProfiles: headers, HeaderProfileKey: profileID}, func(request domain.SearchRequest) (string, error) {
			return baidu.BuildSearchURL(configValue.DesktopURL, request)
		})
		if createErr != nil {
			closeCreated()
			return nil, nil, createErr
		}
		pacer, createErr := baidu.NewFixedSessionPacer(configValue.BaiduSessionMinInterval, configValue.BaiduSessionMaxJitter, time.Now)
		if createErr != nil {
			chromeClient.Close()
			closeCreated()
			return nil, nil, createErr
		}
		session, createErr := baidu.NewSessionTransport(baidu.SessionConfig{
			BootstrapURL: origin(configValue.DesktopURL), SearchURL: configValue.DesktopURL,
			RequestTimeout: configValue.DesktopTimeout, MinInterval: configValue.BaiduSessionMinInterval,
			MaxJitter: configValue.BaiduSessionMaxJitter, CaptchaCooldown: configValue.BaiduCaptchaCooldown,
			RateLimitCooldown: configValue.BaiduRateLimitCooldown, FallbackReserve: configValue.BaiduFallbackReserve,
			MaxBodyBytes: configValue.MaxBodyBytes,
		}, fixedHeader, baidu.NewHTTPSessionClientFactory(baseTransport), pacer, baidu.BaiduResponseClassifier{}, time.Now)
		if createErr != nil {
			chromeClient.Close()
			closeCreated()
			return nil, nil, createErr
		}
		chain, createErr := baidu.NewStrategyChain(baidu.ConservativeStrategyFallback{},
			baidu.StrategyStep{Name: domain.BaiduStrategyNameFixedSession, Transport: session},
			baidu.StrategyStep{Name: domain.BaiduStrategyNameHeaderPool, Transport: desktopClient, Waiters: []baidu.Waiter{limiter, jitter}, UseBreaker: true},
			baidu.StrategyStep{Name: domain.BaiduStrategyNameHeaderPool, Transport: mobileClient, Waiters: []baidu.Waiter{limiter, jitter}, UseBreaker: true},
			baidu.StrategyStep{Name: domain.BaiduStrategyNameHeaderPool, Transport: chromeClient, Waiters: []baidu.Waiter{limiter, jitter}, UseBreaker: true},
		)
		if createErr != nil {
			chromeClient.Close()
			closeCreated()
			return nil, nil, createErr
		}
		searcher, createErr := baidu.NewProvider(chain, artifacts, baidu.NewBreaker(time.Now))
		if createErr != nil {
			chromeClient.Close()
			closeCreated()
			return nil, nil, createErr
		}
		profile, createErr := profilepool.NewProviderProfile(profileID, normalizedPositive(configValue.BaiduProfileCapacity, 1), searcher, func() error { chromeClient.Close(); return nil })
		if createErr != nil {
			chromeClient.Close()
			closeCreated()
			return nil, nil, createErr
		}
		profiles = append(profiles, profile)
	}
	pool, err := newManagedPool(profiles, filepath.Join(configValue.ProfileManifestRoot, string(domain.ProviderNameBaidu)), configValue.ProviderRate, configValue.ProviderBurst)
	if err != nil {
		closeCreated()
		return nil, nil, err
	}
	return pool, closeShared, nil
}

func buildBingPool(configValue config.Config, artifacts bing.ArtifactStore, headers headerprofile.Pool) (*profilepool.Pool, error) {
	return buildBrowserPool(domain.ProviderNameBing, normalizedPositive(configValue.BingProfileCount, 5), normalizedPositive(configValue.BingProfileCapacity, 2), configValue.BingProfileDir, filepath.Join(configValue.ProfileManifestRoot, string(domain.ProviderNameBing)), configValue.ProviderRate, configValue.ProviderBurst,
		func(profileDir string) (profilepool.Searcher, func() error, error) {
			profileID := filepath.Base(profileDir)
			client, err := chromebrowser.New(chromebrowser.Config{
				ProfileDir: profileDir, ExecPath: configValue.ChromePath, Timeout: configValue.BingTimeout,
				Headless: configValue.ChromeHeadless, DisableSandbox: configValue.ChromeNoSandbox,
				MaxBodyBytes: int(configValue.MaxBodyBytes), MaxConcurrentTabs: normalizedPositive(configValue.BingProfileCapacity, 2), HeaderProfiles: headers, HeaderProfileKey: profileID,
			}, func(request domain.SearchRequest) (string, error) {
				return bing.BuildSearchURL(configValue.BingURL, request)
			})
			if err != nil {
				return nil, nil, err
			}
			searcher, err := bing.New(client, artifacts)
			if err != nil {
				client.Close()
				return nil, nil, err
			}
			return searcher, func() error { client.Close(); return nil }, nil
		})
}

func buildBravePool(configValue config.Config, artifacts brave.ArtifactStore, headers headerprofile.Pool) (*profilepool.Pool, error) {
	return buildBrowserPool(domain.ProviderNameBrave, normalizedPositive(configValue.BraveProfileCount, 3), normalizedPositive(configValue.BraveProfileCapacity, 1), configValue.BraveProfileDir, filepath.Join(configValue.ProfileManifestRoot, string(domain.ProviderNameBrave)), configValue.ProviderRate, configValue.ProviderBurst,
		func(profileDir string) (profilepool.Searcher, func() error, error) {
			profileID := filepath.Base(profileDir)
			client, err := chromebrowser.New(chromebrowser.Config{
				ProfileDir: profileDir, ExecPath: configValue.ChromePath, Timeout: configValue.BraveTimeout,
				Headless: configValue.ChromeHeadless, DisableSandbox: configValue.ChromeNoSandbox,
				MaxBodyBytes: int(configValue.MaxBodyBytes), MaxConcurrentTabs: normalizedPositive(configValue.BraveProfileCapacity, 1), HeaderProfiles: headers, HeaderProfileKey: profileID,
			}, func(request domain.SearchRequest) (string, error) {
				return brave.BuildSearchURL(configValue.BraveURL, request)
			})
			if err != nil {
				return nil, nil, err
			}
			searcher, err := brave.New(client, artifacts)
			if err != nil {
				client.Close()
				return nil, nil, err
			}
			return searcher, func() error { client.Close(); return nil }, nil
		})
}

func buildBrowserPool(providerName domain.ProviderName, count, capacity int, root, manifestRoot string, providerRate float64, providerBurst int, factory func(string) (profilepool.Searcher, func() error, error)) (*profilepool.Pool, error) {
	profiles := make([]profilepool.Profile, 0, count)
	closeCreated := func() {
		for _, profile := range profiles {
			_ = profile.Close()
		}
	}
	for index := 0; index < count; index++ {
		id := fmt.Sprintf("%s-%04d", providerName, index+1)
		profileDir := filepath.Join(root, id)
		var createProfile func() (profilepool.Profile, error)
		createProfile = func() (profilepool.Profile, error) {
			searcher, closeFn, err := factory(profileDir)
			if err != nil {
				return nil, err
			}
			profile, profileErr := profilepool.NewProviderProfileWithFactory(id, capacity, searcher, closeFn, createProfile)
			if profileErr != nil {
				_ = closeFn()
				return nil, profileErr
			}
			return profile, nil
		}
		created, err := createProfile()
		if err != nil {
			closeCreated()
			return nil, fmt.Errorf("create %s profile %s: %w", providerName, id, err)
		}
		profiles = append(profiles, created)
	}
	pool, err := newManagedPool(profiles, manifestRoot, providerRate, providerBurst)
	if err != nil {
		closeCreated()
		return nil, err
	}
	return pool, nil
}

func buildDuckDuckGoPool(configValue config.Config, headers headerprofile.Pool, baseTransport *http.Transport) (*profilepool.Pool, error) {
	count := normalizedPositive(configValue.DuckDuckGoProfileCount, 4)
	profiles := make([]profilepool.Profile, 0, count)
	for index := 0; index < count; index++ {
		id := fmt.Sprintf("duckduckgo-%04d", index+1)
		var createProfile func() (profilepool.Profile, error)
		createProfile = func() (profilepool.Profile, error) {
			jar, err := cookiejar.New(nil)
			if err != nil {
				return nil, err
			}
			client := &http.Client{Transport: baseTransport, Jar: jar}
			searcher, err := duckduckgo.New(duckduckgo.Config{BaseURL: configValue.DuckDuckGoURL, UserAgent: configValue.UserAgent, Timeout: configValue.DuckDuckGoTimeout, MaxBodyBytes: configValue.MaxBodyBytes, HeaderProfiles: headers, HeaderProfileKey: id}, client)
			if err != nil {
				return nil, err
			}
			return profilepool.NewProviderProfileWithFactory(id, normalizedPositive(configValue.DuckDuckGoProfileCapacity, 1), searcher, nil, createProfile)
		}
		profile, err := createProfile()
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return newManagedPool(profiles, filepath.Join(configValue.ProfileManifestRoot, string(domain.ProviderNameDuckDuckGo)), configValue.ProviderRate, configValue.ProviderBurst)
}

func newManagedPool(profiles []profilepool.Profile, manifestRoot string, providerRate float64, providerBurst int) (*profilepool.Pool, error) {
	manifest, err := profilepool.NewManifestStore(manifestRoot)
	if err != nil {
		return nil, err
	}
	limiter := rate.NewLimiter(rate.Limit(providerRate), providerBurst)
	pool, err := profilepool.New(profiles, profilepool.Config{Manifest: manifest, Limiter: limiter})
	if err != nil {
		return nil, err
	}
	if err := pool.Activate(); err != nil {
		_ = pool.Close()
		return nil, err
	}
	return pool, nil
}

func normalizedPositive(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}
