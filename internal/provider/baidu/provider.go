package baidu

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"web-search-backend/internal/detector"
	"web-search-backend/internal/domain"
)

type ArtifactStore interface {
	Preview([]byte) (string, string)
	RedactHeaders(http.Header) http.Header
	SaveHTML(requestID, transport string, body []byte) (string, error)
	SaveScreenshot(requestID, transport string, body []byte) (string, error)
}

type Waiter interface {
	Wait(context.Context) error
}

type Provider struct {
	chain     *StrategyChain
	artifacts ArtifactStore
	breaker   *Breaker
}

func NewProvider(chain *StrategyChain, artifacts ArtifactStore, breaker *Breaker) (*Provider, error) {
	if chain == nil || len(chain.steps) == 0 {
		return nil, fmt.Errorf("Baidu strategy chain is empty")
	}
	if breaker == nil {
		breaker = NewBreaker(time.Now)
	}
	return &Provider{chain: chain, artifacts: artifacts, breaker: breaker}, nil
}

func (p *Provider) Name() domain.ProviderName {
	return domain.ProviderNameBaidu
}

func (p *Provider) Search(ctx context.Context, request domain.SearchRequest) (domain.SearchResponse, error) {
	requestID := request.RequestID
	if requestID == "" {
		requestID = "req_internal"
	}
	steps := p.chain.stepsCopy()
	attempts := make([]domain.Attempt, 0, len(steps))
	artifactPaths := make([]string, 0, len(steps)*2)
	warnings := make([]domain.Warning, 0)

	for _, step := range steps {
		current := step.Transport
		if step.UseBreaker && !p.breaker.Allow(current.Name()) {
			attempts = append(attempts, domain.Attempt{
				Strategy:       step.Name,
				Transport:      current.Name(),
				Classification: detector.Blocked,
				OriginalError:  fmt.Sprintf("transport %s circuit is open", current.Name()),
			})
			continue
		}
		if err := waitForStrategy(ctx, step); err != nil {
			attempts = append(attempts, domain.Attempt{
				Strategy:       step.Name,
				Transport:      current.Name(),
				Classification: detector.Timeout,
				OriginalError:  fmt.Sprintf("wait for Baidu strategy %s: %v", step.Name, err),
			})
			break
		}

		response, fetchErr := current.Fetch(ctx, request)
		if response.PageTitle == "" && len(response.Body) > 0 {
			response.PageTitle = detector.ExtractTitle(response.Body)
		}
		attempt := domain.Attempt{
			Strategy:          step.Name,
			Transport:         current.Name(),
			HeaderProfile:     response.HeaderProfile,
			RequestURL:        response.RequestURL,
			HTTPStatus:        response.StatusCode,
			FinalURL:          response.FinalURL,
			PageTitle:         response.PageTitle,
			ElapsedMS:         response.Elapsed.Milliseconds(),
			SessionState:      response.SessionState,
			SessionGeneration: response.SessionGeneration,
			SessionWaitMS:     response.SessionWait.Milliseconds(),
		}
		if !response.BlockedUntil.IsZero() {
			blockedUntil := response.BlockedUntil
			attempt.BlockedUntil = &blockedUntil
		}
		if request.Debug && p.artifacts != nil {
			attempt.BodyPreview, attempt.BodySHA256 = p.artifacts.Preview(response.Body)
			attempt.ResponseHeaders = p.artifacts.RedactHeaders(response.Headers)
			if len(response.Body) > 0 {
				path, err := p.artifacts.SaveHTML(requestID, string(current.Name()), response.Body)
				if err != nil {
					warnings = append(warnings, domain.Warning{Code: domain.WarningCodeArtifactSaveError, Message: err.Error()})
				} else {
					artifactPaths = append(artifactPaths, path)
				}
			}
			if len(response.Screenshot) > 0 {
				path, err := p.artifacts.SaveScreenshot(requestID, string(current.Name()), response.Screenshot)
				if err != nil {
					warnings = append(warnings, domain.Warning{Code: domain.WarningCodeArtifactSaveError, Message: err.Error()})
				} else {
					artifactPaths = append(artifactPaths, path)
				}
			}
		}

		if fetchErr != nil {
			classification := response.Classification
			var sessionErr *SessionError
			if errors.As(fetchErr, &sessionErr) && sessionErr.Classification != "" {
				classification = sessionErr.Classification
			}
			if classification == "" {
				classification = detector.NetworkError
				if errors.Is(fetchErr, context.DeadlineExceeded) || errors.Is(fetchErr, context.Canceled) {
					classification = detector.Timeout
				}
			}
			attempt.Classification = classification
			attempt.OriginalError = fetchErr.Error()
			attempts = append(attempts, attempt)
			if step.UseBreaker {
				p.tripStrategy(step.Name, classification)
			}
			if !p.chain.policy.Allows(classification) {
				return domain.SearchResponse{}, buildSearchError(attempts, artifactPaths)
			}
			continue
		}

		classification := response.Classification
		if classification == "" {
			classification = detector.Classify(response.StatusCode, response.FinalURL, response.PageTitle, response.Body)
		}
		attempt.Classification = classification
		if classification != detector.Normal && classification != detector.Empty {
			attempt.OriginalError = fmt.Sprintf(
				"baidu response classified as %s: status=%d final_url=%s title=%q",
				classification,
				response.StatusCode,
				response.FinalURL,
				response.PageTitle,
			)
			attempts = append(attempts, attempt)
			if step.UseBreaker {
				p.tripStrategy(step.Name, classification)
			}
			if !p.chain.policy.Allows(classification) {
				return domain.SearchResponse{}, buildSearchError(attempts, artifactPaths)
			}
			continue
		}

		results, parserWarnings, parserErr := parseForTransport(current.Name(), response.Body, request.Limit)
		if parserErr != nil {
			attempt.Classification = detector.ParseChanged
			attempt.ParserError = parserErr.Error()
			attempt.OriginalError = parserErr.Error()
			attempts = append(attempts, attempt)
			if !p.chain.policy.Allows(detector.ParseChanged) {
				return domain.SearchResponse{}, buildSearchError(attempts, artifactPaths)
			}
			continue
		}
		if len(results) == 0 {
			attempt.Classification = detector.Empty
		}
		attempts = append(attempts, attempt)
		warnings = append(warnings, parserWarnings...)
		responseValue := domain.SearchResponse{
			Query:    request.Query,
			Provider: p.Name(),
			Results:  results,
			Meta: domain.Meta{
				Strategy:      step.Name,
				Transport:     current.Name(),
				FallbackCount: len(attempts) - 1,
				RequestID:     requestID,
			},
			Warnings: warnings,
			StoredAt: time.Now(),
		}
		if request.Debug {
			responseValue.Debug = &domain.Debug{Attempts: attempts, RawArtifacts: artifactPaths}
		}
		return responseValue, nil
	}

	return domain.SearchResponse{}, buildSearchError(attempts, artifactPaths)
}

func (p *Provider) tripStrategy(name domain.BaiduStrategyName, classification domain.Classification) {
	for _, step := range p.chain.steps {
		if step.UseBreaker && step.Name == name {
			p.breaker.Trip(step.Transport.Name(), classification)
		}
	}
}

func parseForTransport(name domain.TransportName, body []byte, limit int) ([]domain.SearchResult, []domain.Warning, error) {
	switch name {
	case domain.TransportNameMobileHTTP:
		return ParseMobile(body, limit)
	case domain.TransportNameChromedp:
		results, warnings, desktopErr := ParseDesktop(body, limit)
		if desktopErr == nil {
			return results, warnings, nil
		}
		results, warnings, mobileErr := ParseMobile(body, limit)
		if mobileErr == nil {
			return results, warnings, nil
		}
		return nil, nil, fmt.Errorf("browser DOM parsers failed: desktop=%v; mobile=%v", desktopErr, mobileErr)
	default:
		return ParseDesktop(body, limit)
	}
}

func buildSearchError(attempts []domain.Attempt, artifacts []string) error {
	code := domain.ErrProviderUnavailable
	message := "BaiduProvider is unavailable"
	retryable := true
	priority := []struct {
		classification domain.Classification
		code           domain.ErrorCode
		message        string
	}{
		{detector.Captcha, domain.ErrCaptchaRequired, "百度返回安全验证页面"},
		{detector.RateLimited, domain.ErrRateLimited, "百度限制了当前请求频率"},
		{detector.Timeout, domain.ErrUpstreamTimeout, "百度查询链路超时"},
		{detector.ParseChanged, domain.ErrUpstreamChanged, "百度页面结构发生变化"},
	}
	var original string
	for _, wanted := range priority {
		for _, attempt := range attempts {
			if attempt.Classification == wanted.classification {
				code, message, original = wanted.code, wanted.message, attempt.OriginalError
				break
			}
		}
		if original != "" {
			break
		}
	}
	if original == "" {
		for i := len(attempts) - 1; i >= 0; i-- {
			if strings.TrimSpace(attempts[i].OriginalError) != "" {
				original = attempts[i].OriginalError
				break
			}
		}
	}
	if original == "" {
		original = message
	}
	return &domain.SearchError{
		Code:      code,
		Message:   message,
		Retryable: retryable,
		Original:  errors.New(original),
		Attempts:  attempts,
		Artifacts: artifacts,
	}
}
