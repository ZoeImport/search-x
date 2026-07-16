package bing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"web-search-backend/websearch/internal/detector"
	"web-search-backend/websearch/internal/domain"
	"web-search-backend/websearch/internal/transport"
)

// ArtifactStore persists authorized Bing debug artifacts.
type ArtifactStore interface {
	// SaveHTML persists an HTML response artifact and returns its path.
	SaveHTML(requestID, transportName string, body []byte) (string, error)
	// SaveScreenshot persists a screenshot artifact and returns its path.
	SaveScreenshot(requestID, transportName string, body []byte) (string, error)
}

// Provider searches Bing through a browser transport.
type Provider struct {
	browser   transport.SearchTransport
	artifacts ArtifactStore
}

// New validates dependencies and creates a Bing provider.
func New(browser transport.SearchTransport, artifacts ...ArtifactStore) (*Provider, error) {
	if browser == nil {
		return nil, fmt.Errorf("Bing browser transport is nil")
	}
	var artifactStore ArtifactStore
	if len(artifacts) > 0 {
		artifactStore = artifacts[0]
	}
	return &Provider{browser: browser, artifacts: artifactStore}, nil
}

// Name returns the Bing provider name.
func (p *Provider) Name() domain.ProviderName {
	return domain.ProviderNameBing
}

// Search fetches, classifies, and parses Bing browser results.
func (p *Provider) Search(ctx context.Context, request domain.SearchRequest) (domain.SearchResponse, error) {
	response, fetchErr := p.browser.Fetch(ctx, request)
	attempt := domain.Attempt{
		Provider: p.Name(), Transport: domain.TransportNameBingChromedp,
		HeaderProfile: response.HeaderProfile,
		RequestURL:    response.RequestURL, HTTPStatus: response.StatusCode, FinalURL: response.FinalURL,
		ElapsedMS: response.Elapsed.Milliseconds(),
	}
	artifactPaths, artifactWarnings := p.saveArtifacts(request, response)
	if fetchErr != nil {
		code := domain.ErrProviderUnavailable
		if errors.Is(fetchErr, context.DeadlineExceeded) || errors.Is(fetchErr, context.Canceled) {
			code = domain.ErrUpstreamTimeout
		}
		attempt.OriginalError = fetchErr.Error()
		return domain.SearchResponse{}, newSearchError(code, fetchErr, attempt, artifactPaths)
	}
	if isCaptcha(response.FinalURL, response.Body) {
		attempt.Classification = detector.Captcha
		attempt.OriginalError = fmt.Sprintf("Bing response classified as captcha: status=%d final_url=%s", response.StatusCode, response.FinalURL)
		return domain.SearchResponse{}, newSearchError(domain.ErrCaptchaRequired, errors.New(attempt.OriginalError), attempt, artifactPaths)
	}
	if response.StatusCode == http.StatusTooManyRequests {
		attempt.Classification = detector.RateLimited
		attempt.OriginalError = fmt.Sprintf("Bing returned HTTP %d", response.StatusCode)
		return domain.SearchResponse{}, newSearchError(domain.ErrRateLimited, errors.New(attempt.OriginalError), attempt, artifactPaths)
	}
	if response.StatusCode >= http.StatusBadRequest {
		attempt.Classification = detector.Blocked
		attempt.OriginalError = fmt.Sprintf("Bing returned HTTP %d", response.StatusCode)
		return domain.SearchResponse{}, newSearchError(domain.ErrProviderUnavailable, errors.New(attempt.OriginalError), attempt, artifactPaths)
	}
	results, err := Parse(response.Body, request.Limit)
	if err != nil {
		attempt.Classification = detector.ParseChanged
		attempt.ParserError = err.Error()
		attempt.OriginalError = err.Error()
		return domain.SearchResponse{}, newSearchError(domain.ErrUpstreamChanged, err, attempt, artifactPaths)
	}
	attempt.Classification = detector.Normal
	searchResponse := domain.SearchResponse{
		Query: request.Query, Provider: p.Name(), Results: results,
		Meta: domain.Meta{
			RequestedProvider: request.Provider,
			Transport:         domain.TransportNameBingChromedp,
			RequestID:         request.RequestID,
		},
		Warnings: artifactWarnings, StoredAt: time.Now(),
	}
	if request.Debug {
		searchResponse.Debug = &domain.Debug{Attempts: []domain.Attempt{attempt}, RawArtifacts: artifactPaths}
	}
	return searchResponse, nil
}

func (p *Provider) saveArtifacts(request domain.SearchRequest, response transport.Response) ([]string, []domain.Warning) {
	if !request.Debug || p.artifacts == nil {
		return nil, nil
	}
	paths := make([]string, 0, 2)
	warnings := make([]domain.Warning, 0, 2)
	if len(response.Body) > 0 {
		if path, err := p.artifacts.SaveHTML(request.RequestID, string(domain.TransportNameBingChromedp), response.Body); err != nil {
			warnings = append(warnings, domain.Warning{Code: domain.WarningCodeArtifactSaveError, Message: err.Error()})
		} else {
			paths = append(paths, path)
		}
	}
	if len(response.Screenshot) > 0 {
		if path, err := p.artifacts.SaveScreenshot(request.RequestID, string(domain.TransportNameBingChromedp), response.Screenshot); err != nil {
			warnings = append(warnings, domain.Warning{Code: domain.WarningCodeArtifactSaveError, Message: err.Error()})
		} else {
			paths = append(paths, path)
		}
	}
	return paths, warnings
}

func isCaptcha(finalURL string, body []byte) bool {
	parsed, _ := url.Parse(finalURL)
	path := strings.ToLower(parsed.Path)
	lowerBody := strings.ToLower(string(body))
	return strings.Contains(path, "captcha") || strings.Contains(path, "challenge") ||
		strings.Contains(lowerBody, "id=\"b_captcha\"") ||
		strings.Contains(lowerBody, "id=\"turnstile-widget\"") ||
		strings.Contains(lowerBody, "challenges.cloudflare.com/turnstile") ||
		strings.Contains(lowerBody, "\"verifyendpoint\":\"https://www.bing.com/challenge/verify") ||
		strings.Contains(lowerBody, "verify you are human")
}

func newSearchError(code domain.ErrorCode, original error, attempt domain.Attempt, artifacts []string) error {
	message := "Bing Provider 不可用"
	switch code {
	case domain.ErrCaptchaRequired:
		message = "Bing 返回安全验证页面"
	case domain.ErrRateLimited:
		message = "Bing 限制了当前请求频率"
	case domain.ErrUpstreamTimeout:
		message = "Bing 查询超时"
	case domain.ErrUpstreamChanged:
		message = "Bing 页面结构发生变化"
	}
	return &domain.SearchError{
		Code: code, Message: message, Retryable: true, Original: original,
		Attempts: []domain.Attempt{attempt}, Artifacts: append([]string(nil), artifacts...),
	}
}
