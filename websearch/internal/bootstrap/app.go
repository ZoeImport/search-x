package bootstrap

import (
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"

	"web-search-backend/websearch/internal/api/httpapi"
	"web-search-backend/websearch/internal/app"
	"web-search-backend/websearch/internal/cache"
	"web-search-backend/websearch/internal/config"
	"web-search-backend/websearch/internal/cursor"
	"web-search-backend/websearch/internal/debugartifact"
	"web-search-backend/websearch/internal/headerprofile"
)

type App struct {
	Router *gin.Engine
	close  func()
}

func New(config config.Config, logger *slog.Logger) (*App, error) {
	if !config.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	headers, err := headerprofile.NewChromiumDesktopPool(config.UserAgent)
	if err != nil {
		return nil, fmt.Errorf("create header profiles: %w", err)
	}
	artifacts, err := debugartifact.New(config.DebugDir, config.DebugPreviewBytes)
	if err != nil {
		return nil, err
	}
	runtimeValue, err := newSearchRuntime(config, artifacts, headers)
	if err != nil {
		return nil, fmt.Errorf("create search runtime: %w", err)
	}
	memoryCache, err := cache.NewMemory(config.CacheMaxItems, config.FreshTTL, config.StaleTTL, time.Now)
	if err != nil {
		runtimeValue.Close()
		return nil, err
	}
	service := app.NewSearchService(runtimeValue.registry, memoryCache, time.Now)
	service.SetTracer(runtimeValue.tracer)
	service.ConfigureLiveGuard(config.GlobalInflightMax, config.AutoQueueMax)
	cursorCodec, err := cursor.New(config.CursorSecret, config.CursorTTL, time.Now)
	if err != nil {
		runtimeValue.Close()
		return nil, err
	}
	router, err := httpapi.New(httpapi.Options{Searcher: service, Cursor: cursorCodec, Ready: runtimeValue.Ready, Logger: logger, Timeout: config.TotalTimeout, MaxTimeout: config.MaxRequestTimeout, CacheBypass: config.CacheBypass, AllowRequestProviders: config.AllowRequestProviders, EnabledProviders: config.EnabledProviders, ProviderVisibility: config.ProviderVisibility, AllowedOrigins: config.CORSAllowedOrigins})
	if err != nil {
		runtimeValue.Close()
		return nil, err
	}
	return &App{Router: router, close: runtimeValue.Close}, nil
}

func (app *App) Close() {
	if app != nil && app.close != nil {
		app.close()
	}
}

func origin(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host + "/"
}
