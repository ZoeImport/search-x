package chromebrowser

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/transport"
)

const defaultBaseURL = "https://www.baidu.com/s"

type Config struct {
	ProfileDir     string
	BaseURL        string
	ExecPath       string
	Timeout        time.Duration
	Headless       bool
	DisableSandbox bool
	MaxBodyBytes   int
}

type Client struct {
	config        Config
	allocatorCtx  context.Context
	allocatorStop context.CancelFunc
	browserCtx    context.Context
	browserStop   context.CancelFunc
	semaphore     chan struct{}
	closeOnce     sync.Once
}

func New(config Config) (*Client, error) {
	config.ProfileDir = strings.TrimSpace(config.ProfileDir)
	if config.ProfileDir == "" {
		return nil, fmt.Errorf("chrome profile directory is empty")
	}
	if config.Timeout <= 0 {
		return nil, fmt.Errorf("chrome timeout must be positive")
	}
	if config.BaseURL == "" {
		config.BaseURL = defaultBaseURL
	}
	if _, err := buildSearchURL(config.BaseURL, domain.SearchRequest{Query: "validation", Limit: 10, Page: 1}); err != nil {
		return nil, err
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = 4 << 20
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
		allocatorCtx:  allocatorCtx,
		allocatorStop: allocatorStop,
		browserCtx:    browserCtx,
		browserStop:   browserStop,
		semaphore:     make(chan struct{}, 1),
	}, nil
}

func (c *Client) Name() domain.TransportName {
	return domain.TransportNameChromedp
}

func (c *Client) Fetch(ctx context.Context, request domain.SearchRequest) (transport.Response, error) {
	requestURL, err := buildSearchURL(c.config.BaseURL, request)
	if err != nil {
		return transport.Response{}, err
	}
	select {
	case c.semaphore <- struct{}{}:
		defer func() { <-c.semaphore }()
	case <-ctx.Done():
		return transport.Response{RequestURL: requestURL}, fmt.Errorf("wait for chromedp slot: %w", ctx.Err())
	}

	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	tabCtx, tabCancel := chromedp.NewContext(c.browserCtx)
	defer tabCancel()
	started := time.Now()
	response := transport.Response{RequestURL: requestURL}

	if err := chromedp.Run(tabCtx, network.Enable(), chromedp.Navigate(requestURL)); err != nil {
		response.Elapsed = time.Since(started)
		c.captureBestEffort(tabCtx, &response)
		return response, fmt.Errorf("chromedp navigate: %w", err)
	}
	if err := chromedp.Run(tabCtx, chromedp.WaitReady("body", chromedp.ByQuery)); err != nil {
		response.Elapsed = time.Since(started)
		c.captureBestEffort(tabCtx, &response)
		return response, fmt.Errorf("chromedp wait body: %w", err)
	}
	if err := c.captureDOM(tabCtx, &response); err != nil {
		response.Elapsed = time.Since(started)
		c.captureScreenshot(tabCtx, &response)
		return response, err
	}
	c.captureScreenshot(tabCtx, &response)
	response.StatusCode = 200
	response.Elapsed = time.Since(started)
	if len(response.Body) > c.config.MaxBodyBytes {
		response.Body = response.Body[:c.config.MaxBodyBytes]
		return response, fmt.Errorf("chromedp response body exceeds %d bytes", c.config.MaxBodyBytes)
	}
	return response, nil
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

func buildSearchURL(baseURL string, request domain.SearchRequest) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid chrome base URL %q", baseURL)
	}
	values := u.Query()
	values.Set("wd", request.Query)
	values.Set("rn", strconv.Itoa(request.Limit))
	values.Set("pn", strconv.Itoa((request.Page-1)*request.Limit))
	values.Set("ie", "utf-8")
	u.RawQuery = values.Encode()
	return u.String(), nil
}
