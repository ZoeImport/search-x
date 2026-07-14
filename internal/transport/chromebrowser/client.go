package chromebrowser

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/transport"
)

// URLBuilder builds an engine-specific search URL.
type URLBuilder func(domain.SearchRequest) (string, error)

type Config struct {
	ProfileDir string
	ExecPath   string
	Timeout    time.Duration
	// MaxConcurrentTabs bounds simultaneous tabs owned by this browser client.
	MaxConcurrentTabs int
	// PostLoadWait allows page scripts to replace an initial JavaScript shell before DOM capture.
	PostLoadWait   time.Duration
	Headless       bool
	DisableSandbox bool
	MaxBodyBytes   int
}

type Client struct {
	config        Config
	urlBuilder    URLBuilder
	allocatorCtx  context.Context
	allocatorStop context.CancelFunc
	browserCtx    context.Context
	browserStop   context.CancelFunc
	browserInit   func(context.Context) error
	browserInitMu sync.Mutex
	browserReady  bool
	semaphore     chan struct{}
	closeOnce     sync.Once
}

func New(config Config, urlBuilder URLBuilder) (*Client, error) {
	config.ProfileDir = strings.TrimSpace(config.ProfileDir)
	if config.ProfileDir == "" {
		return nil, fmt.Errorf("chrome profile directory is empty")
	}
	if config.Timeout <= 0 {
		return nil, fmt.Errorf("chrome timeout must be positive")
	}
	if urlBuilder == nil {
		return nil, fmt.Errorf("chrome URL builder is nil")
	}
	if _, err := urlBuilder(domain.SearchRequest{Query: "validation", Limit: 10, Page: 1}); err != nil {
		return nil, err
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = 4 << 20
	}
	if config.MaxConcurrentTabs <= 0 {
		config.MaxConcurrentTabs = 1
	}
	if err := os.MkdirAll(config.ProfileDir, 0o700); err != nil {
		return nil, fmt.Errorf("create chrome profile directory: %w", err)
	}

	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.UserDataDir(config.ProfileDir),
		chromedp.Flag("headless", config.Headless),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("no-sandbox", config.DisableSandbox),
	)
	if config.ExecPath != "" {
		opts = append(opts, chromedp.ExecPath(config.ExecPath))
	}
	allocatorCtx, allocatorStop := chromedp.NewExecAllocator(context.Background(), opts...)
	browserCtx, browserStop := chromedp.NewContext(allocatorCtx)
	return &Client{
		config:        config,
		urlBuilder:    urlBuilder,
		allocatorCtx:  allocatorCtx,
		allocatorStop: allocatorStop,
		browserCtx:    browserCtx,
		browserStop:   browserStop,
		browserInit: func(ctx context.Context) error {
			return chromedp.Run(ctx)
		},
		semaphore: make(chan struct{}, config.MaxConcurrentTabs),
	}, nil
}

func (c *Client) Name() domain.TransportName {
	return domain.TransportNameChromedp
}

func (c *Client) Fetch(ctx context.Context, request domain.SearchRequest) (transport.Response, error) {
	requestURL, err := c.buildURL(request)
	if err != nil {
		return transport.Response{}, err
	}
	return c.fetchURL(ctx, requestURL)
}

// FetchURL renders an already constructed URL and returns the final DOM.
func (c *Client) FetchURL(ctx context.Context, requestURL string) (transport.Response, error) {
	if strings.TrimSpace(requestURL) == "" {
		return transport.Response{}, fmt.Errorf("chromedp request URL is empty")
	}
	return c.fetchURL(ctx, requestURL)
}

func (c *Client) fetchURL(ctx context.Context, requestURL string) (transport.Response, error) {
	response := transport.Response{RequestURL: requestURL}
	select {
	case c.semaphore <- struct{}{}:
		defer func() { <-c.semaphore }()
	case <-ctx.Done():
		return response, fmt.Errorf("wait for chromedp slot: %w", ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return response, fmt.Errorf("start chromedp request: %w", err)
	}
	if err := c.ensureBrowser(); err != nil {
		return response, fmt.Errorf("initialize chromedp browser: %w", err)
	}

	tabCtx, tabCancel := chromedp.NewContext(c.browserCtx)
	defer tabCancel()
	operationCtx, operationCancel := context.WithTimeout(tabCtx, c.config.Timeout)
	stopRequestCancellation := context.AfterFunc(ctx, operationCancel)
	defer stopRequestCancellation()
	defer operationCancel()
	started := time.Now()

	if err := chromedp.Run(operationCtx, network.Enable(), chromedp.Navigate(requestURL)); err != nil {
		response.Elapsed = time.Since(started)
		c.captureBestEffort(operationCtx, &response)
		return response, fmt.Errorf("chromedp navigate: %w", err)
	}
	if err := chromedp.Run(operationCtx, chromedp.WaitReady("body", chromedp.ByQuery)); err != nil {
		response.Elapsed = time.Since(started)
		c.captureBestEffort(operationCtx, &response)
		return response, fmt.Errorf("chromedp wait body: %w", err)
	}
	if c.config.PostLoadWait > 0 {
		if err := chromedp.Run(operationCtx, chromedp.Sleep(c.config.PostLoadWait)); err != nil {
			response.Elapsed = time.Since(started)
			c.captureBestEffort(operationCtx, &response)
			return response, fmt.Errorf("chromedp wait for rendered content: %w", err)
		}
	}
	if err := c.captureDOM(operationCtx, &response); err != nil {
		response.Elapsed = time.Since(started)
		c.captureScreenshot(operationCtx, &response)
		return response, err
	}
	c.captureScreenshot(operationCtx, &response)
	response.StatusCode = 200
	response.Elapsed = time.Since(started)
	if len(response.Body) > c.config.MaxBodyBytes {
		response.Body = response.Body[:c.config.MaxBodyBytes]
		return response, fmt.Errorf("chromedp response body exceeds %d bytes", c.config.MaxBodyBytes)
	}
	return response, nil
}

func (c *Client) ensureBrowser() error {
	c.browserInitMu.Lock()
	defer c.browserInitMu.Unlock()
	if c.browserReady {
		return nil
	}
	if c.browserInit == nil {
		return fmt.Errorf("chromedp browser initializer is nil")
	}
	if err := c.browserInit(c.browserCtx); err != nil {
		return err
	}
	c.browserReady = true
	return nil
}

func (c *Client) captureBestEffort(ctx context.Context, response *transport.Response) {
	_ = c.captureDOM(ctx, response)
	c.captureScreenshot(ctx, response)
}

func (c *Client) captureDOM(ctx context.Context, response *transport.Response) error {
	var html string
	var finalURL string
	if err := chromedp.Run(ctx,
		chromedp.Location(&finalURL),
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("chromedp capture DOM: %w", err)
	}
	response.FinalURL = finalURL
	response.Body = []byte(html)
	return nil
}

func (c *Client) captureScreenshot(ctx context.Context, response *transport.Response) {
	var screenshot []byte
	if err := chromedp.Run(ctx, chromedp.FullScreenshot(&screenshot, 85)); err == nil {
		response.Screenshot = screenshot
	}
}

func (c *Client) Close() {
	c.closeOnce.Do(func() {
		c.browserStop()
		c.allocatorStop()
	})
}

func (c *Client) buildURL(request domain.SearchRequest) (string, error) {
	return c.urlBuilder(request)
}
