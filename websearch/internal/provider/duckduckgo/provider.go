package duckduckgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"web-search-backend/websearch/internal/detector"
	"web-search-backend/websearch/internal/domain"
	"web-search-backend/websearch/internal/headerprofile"
)

const defaultMaxBodyBytes int64 = 4 << 20

// Config defines DuckDuckGo HTTP search limits.
type Config struct {
	// BaseURL is the DuckDuckGo HTML search endpoint.
	BaseURL string
	// UserAgent is sent to the upstream endpoint.
	UserAgent string
	// Timeout limits one upstream request.
	Timeout time.Duration
	// MaxBodyBytes limits the response body read into memory.
	MaxBodyBytes int64
	// HeaderProfiles selects a sticky outbound request profile.
	HeaderProfiles headerprofile.Pool
	// HeaderProfileKey pins an Agent Profile to one coherent header identity.
	HeaderProfileKey string
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
	httpRequest, requestURL, err := p.newRequest(ctx, request)
	if err != nil {
		return domain.SearchResponse{}, searchError(domain.ErrInvalidRequest, false, err, domain.Attempt{})
	}
	requestContext, cancel := context.WithTimeout(ctx, p.config.Timeout)
	defer cancel()
	httpRequest = httpRequest.WithContext(requestContext)
	httpRequest.Header.Set("User-Agent", p.config.UserAgent)
	httpRequest.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	httpRequest.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.7")
	selectedProfile := ""
	if p.config.HeaderProfiles != nil {
		profile, selectErr := p.config.HeaderProfiles.Select(p.headerProfileKey(request), 0)
		if selectErr != nil {
			attempt := domain.Attempt{Provider: p.Name(), Transport: domain.TransportNameDuckDuckGoHTTP, RequestURL: requestURL}
			attempt.OriginalError = selectErr.Error()
			return domain.SearchResponse{}, searchError(domain.ErrProviderUnavailable, true, fmt.Errorf("select DuckDuckGo header profile: %w", selectErr), attempt)
		}
		selectedProfile = string(profile.Name)
		httpRequest.Header.Set("User-Agent", profile.UserAgent)
		httpRequest.Header.Set("Accept-Language", profile.AcceptLanguage)
		for key, value := range profile.Headers {
			httpRequest.Header.Set(key, value)
		}
	}

	started := time.Now()
	httpResponse, err := p.client.Do(httpRequest)
	elapsed := time.Since(started)
	attempt := domain.Attempt{
		Provider: p.Name(), Transport: domain.TransportNameDuckDuckGoHTTP,
		RequestURL: requestURL, ElapsedMS: elapsed.Milliseconds(), HeaderProfile: selectedProfile,
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
	if isHumanChallenge(body) {
		err = fmt.Errorf("DuckDuckGo response classified as captcha")
		attempt.Classification = detector.Captcha
		attempt.OriginalError = err.Error()
		return domain.SearchResponse{}, searchError(domain.ErrCaptchaRequired, true, err, attempt)
	}

	results, nextPageToken, err := ParsePage(body, request.Limit)
	if err != nil {
		attempt.Classification = detector.ParseChanged
		attempt.ParserError = err.Error()
		attempt.OriginalError = err.Error()
		return domain.SearchResponse{}, searchError(domain.ErrUpstreamChanged, true, err, attempt)
	}
	attempt.Classification = detector.Normal
	response := domain.SearchResponse{
		Query: request.Query, Provider: p.Name(), Results: results,
		Meta: domain.Meta{
			RequestedProvider: request.Provider,
			Transport:         domain.TransportNameDuckDuckGoHTTP,
			RequestID:         request.RequestID,
		},
		Warnings:        make([]domain.Warning, 0),
		StoredAt:        time.Now(),
		PaginationKnown: true,
		NextPageToken:   nextPageToken,
	}
	if request.Debug {
		response.Debug = &domain.Debug{Attempts: []domain.Attempt{attempt}}
	}
	return response, nil
}

func (p *Provider) headerProfileKey(request domain.SearchRequest) string {
	if p.config.HeaderProfileKey != "" {
		return p.config.HeaderProfileKey
	}
	if request.RequestID != "" {
		return request.RequestID
	}
	return request.Query
}

func (p *Provider) buildURL(request domain.SearchRequest) (string, error) {
	parsed, err := url.Parse(p.config.BaseURL)
	if err != nil {
		return "", fmt.Errorf("parse DuckDuckGo base URL: %w", err)
	}
	values := parsed.Query()
	values.Set("q", request.UpstreamQuery())
	values.Set("s", "0")
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}

func (p *Provider) newRequest(ctx context.Context, request domain.SearchRequest) (*http.Request, string, error) {
	if request.Page <= 1 {
		requestURL, err := p.buildURL(request)
		if err != nil {
			return nil, "", err
		}
		httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		return httpRequest, requestURL, err
	}
	if strings.TrimSpace(request.ProviderPageToken) == "" {
		return nil, "", fmt.Errorf("DuckDuckGo pagination token is required")
	}
	var values url.Values
	if err := json.Unmarshal([]byte(request.ProviderPageToken), &values); err != nil {
		return nil, "", fmt.Errorf("decode DuckDuckGo pagination token: %w", err)
	}
	if values.Get("q") != request.UpstreamQuery() || values.Get("s") == "" {
		return nil, "", fmt.Errorf("DuckDuckGo pagination token does not match the request")
	}
	parsed, err := url.Parse(p.config.BaseURL)
	if err != nil {
		return nil, "", fmt.Errorf("parse DuckDuckGo base URL: %w", err)
	}
	parsed.RawQuery = ""
	requestURL := parsed.String()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewBufferString(values.Encode()))
	if err != nil {
		return nil, "", fmt.Errorf("create DuckDuckGo pagination request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return httpRequest, requestURL, nil
}

func isHumanChallenge(body []byte) bool {
	lowerBody := strings.ToLower(string(body))
	return strings.Contains(lowerBody, `id="challenge-form"`) ||
		strings.Contains(lowerBody, `data-testid="anomaly-modal"`) ||
		strings.Contains(lowerBody, "unfortunately, bots use duckduckgo too")
}

func searchError(code domain.ErrorCode, retryable bool, original error, attempt domain.Attempt) error {
	message := "DuckDuckGo Provider 不可用"
	switch code {
	case domain.ErrInvalidRequest:
		message = "DuckDuckGo 请求参数错误"
	case domain.ErrCaptchaRequired:
		message = "DuckDuckGo 返回安全验证页面"
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
