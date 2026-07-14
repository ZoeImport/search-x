package duckduckgo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"web-search-backend/internal/detector"
	"web-search-backend/internal/domain"
)

const defaultMaxBodyBytes int64 = 4 << 20

// Config defines DuckDuckGo HTTP search limits.
type Config struct {
	BaseURL      string
	UserAgent    string
	Timeout      time.Duration
	MaxBodyBytes int64
}

// Provider searches DuckDuckGo's public HTML result page.
type Provider struct {
	config Config
	client *http.Client
}

// New validates configuration and creates a DuckDuckGo provider.
func New(config Config, client *http.Client) (*Provider, error) {
	parsed, err := url.Parse(strings.TrimSpace(config.BaseURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("invalid DuckDuckGo base URL %q", config.BaseURL)
	}
	config.BaseURL = parsed.String()
	if config.Timeout <= 0 {
		return nil, fmt.Errorf("DuckDuckGo timeout must be positive")
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = defaultMaxBodyBytes
	}
	if strings.TrimSpace(config.UserAgent) == "" {
		config.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/138.0 Safari/537.36"
	}
	if client == nil {
		client = &http.Client{}
	}
	return &Provider{config: config, client: client}, nil
}

// Name returns the DuckDuckGo provider name.
func (p *Provider) Name() domain.ProviderName {
	return domain.ProviderNameDuckDuckGo
}

// Search fetches and parses one DuckDuckGo HTML result page.
func (p *Provider) Search(ctx context.Context, request domain.SearchRequest) (domain.SearchResponse, error) {
	requestURL, err := p.buildURL(request)
	if err != nil {
		return domain.SearchResponse{}, searchError(domain.ErrInvalidRequest, false, err, domain.Attempt{})
	}
	requestContext, cancel := context.WithTimeout(ctx, p.config.Timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestContext, http.MethodGet, requestURL, nil)
	if err != nil {
		attempt := domain.Attempt{Provider: p.Name(), Transport: domain.TransportNameDuckDuckGoHTTP, RequestURL: requestURL}
		return domain.SearchResponse{}, searchError(domain.ErrProviderUnavailable, true, fmt.Errorf("create DuckDuckGo request: %w", err), attempt)
	}
	httpRequest.Header.Set("User-Agent", p.config.UserAgent)
	httpRequest.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	httpRequest.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.7")

	started := time.Now()
	httpResponse, err := p.client.Do(httpRequest)
	elapsed := time.Since(started)
	attempt := domain.Attempt{
		Provider: p.Name(), Transport: domain.TransportNameDuckDuckGoHTTP,
		RequestURL: requestURL, ElapsedMS: elapsed.Milliseconds(),
	}
	if err != nil {
		code := domain.ErrProviderUnavailable
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(requestContext.Err(), context.DeadlineExceeded) {
			code = domain.ErrUpstreamTimeout
		}
		attempt.OriginalError = err.Error()
		return domain.SearchResponse{}, searchError(code, true, fmt.Errorf("DuckDuckGo request: %w", err), attempt)
	}
	defer httpResponse.Body.Close()
	attempt.HTTPStatus = httpResponse.StatusCode
	attempt.FinalURL = httpResponse.Request.URL.String()

	body, err := io.ReadAll(io.LimitReader(httpResponse.Body, p.config.MaxBodyBytes+1))
	if err != nil {
		attempt.OriginalError = err.Error()
		return domain.SearchResponse{}, searchError(domain.ErrProviderUnavailable, true, fmt.Errorf("read DuckDuckGo response: %w", err), attempt)
	}
	if int64(len(body)) > p.config.MaxBodyBytes {
		err = fmt.Errorf("DuckDuckGo response body exceeds %d bytes", p.config.MaxBodyBytes)
		attempt.OriginalError = err.Error()
		return domain.SearchResponse{}, searchError(domain.ErrProviderUnavailable, true, err, attempt)
	}

	if httpResponse.StatusCode == http.StatusTooManyRequests {
		err = fmt.Errorf("DuckDuckGo returned HTTP %d", httpResponse.StatusCode)
		attempt.OriginalError = err.Error()
		return domain.SearchResponse{}, searchError(domain.ErrRateLimited, true, err, attempt)
	}
	if httpResponse.StatusCode >= http.StatusBadRequest {
		err = fmt.Errorf("DuckDuckGo returned HTTP %d", httpResponse.StatusCode)
		attempt.OriginalError = err.Error()
		return domain.SearchResponse{}, searchError(domain.ErrProviderUnavailable, true, err, attempt)
	}

	results, err := Parse(body, request.Limit)
	if err != nil {
		attempt.Classification = string(detector.ParseChanged)
		attempt.ParserError = err.Error()
		attempt.OriginalError = err.Error()
		return domain.SearchResponse{}, searchError(domain.ErrUpstreamChanged, true, err, attempt)
	}
	attempt.Classification = string(detector.Normal)
	response := domain.SearchResponse{
		Query: request.Query, Provider: p.Name(), Results: results,
		Meta: domain.Meta{
			RequestedProvider: request.Provider,
			Transport:         domain.TransportNameDuckDuckGoHTTP,
			RequestID:         request.RequestID,
		},
		Warnings: make([]domain.Warning, 0),
		StoredAt: time.Now(),
	}
	if request.Debug {
		response.Debug = &domain.Debug{Attempts: []domain.Attempt{attempt}}
	}
	return response, nil
}

func (p *Provider) buildURL(request domain.SearchRequest) (string, error) {
	parsed, err := url.Parse(p.config.BaseURL)
	if err != nil {
		return "", fmt.Errorf("parse DuckDuckGo base URL: %w", err)
	}
	values := parsed.Query()
	values.Set("q", request.Query)
	values.Set("s", strconv.Itoa((request.Page-1)*request.Limit))
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}

func searchError(code domain.ErrorCode, retryable bool, original error, attempt domain.Attempt) error {
	message := "DuckDuckGo Provider 不可用"
	switch code {
	case domain.ErrInvalidRequest:
		message = "DuckDuckGo 请求参数错误"
	case domain.ErrRateLimited:
		message = "DuckDuckGo 限制了当前请求频率"
	case domain.ErrUpstreamTimeout:
		message = "DuckDuckGo 查询超时"
	case domain.ErrUpstreamChanged:
		message = "DuckDuckGo 页面结构发生变化"
	}
	attempts := make([]domain.Attempt, 0, 1)
	if attempt.Transport != "" || attempt.OriginalError != "" {
		attempts = append(attempts, attempt)
	}
	return &domain.SearchError{
		Code: code, Message: message, Retryable: retryable, Original: original, Attempts: attempts,
	}
}
