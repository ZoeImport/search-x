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
	"web-search-backend/internal/transport"
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
	transports []transport.SearchTransport
	artifacts  ArtifactStore
	breaker    *Breaker
	limiter    Waiter
	delay      Waiter
}

func NewProvider(transports []transport.SearchTransport, artifacts ArtifactStore, breaker *Breaker, limiter Waiter, delays ...Waiter) *Provider {
	if breaker == nil {
		breaker = NewBreaker(time.Now)
	}
	var delay Waiter
	if len(delays) > 0 {
		delay = delays[0]
	}
	return &Provider{
		transports: append([]transport.SearchTransport(nil), transports...),
		artifacts:  artifacts,
		breaker:    breaker,
		limiter:    limiter,
		delay:      delay,
	}
}

func (p *Provider) Name() domain.ProviderName {
	return domain.ProviderNameBaidu
}

func (p *Provider) Search(ctx context.Context, request domain.SearchRequest) (domain.SearchResponse, error) {
	requestID := request.RequestID
	if requestID == "" {
		requestID = "req_internal"
	}
	attempts := make([]domain.Attempt, 0, len(p.transports))
	artifactPaths := make([]string, 0, len(p.transports)*2)
	warnings := make([]domain.Warning, 0)

	for _, current := range p.transports {
		if !p.breaker.Allow(current.Name()) {
			attempts = append(attempts, domain.Attempt{
				Transport:      current.Name(),
				Classification: detector.Blocked,
				OriginalError:  fmt.Sprintf("transport %s circuit is open", current.Name()),
			})
			continue
		}
		if p.limiter != nil {
			if err := p.limiter.Wait(ctx); err != nil {
				attempts = append(attempts, domain.Attempt{
					Transport:      current.Name(),
					Classification: detector.Timeout,
					OriginalError:  fmt.Sprintf("wait for provider rate limiter: %v", err),
				})
				break
			}
		}
		if p.delay != nil {
			if err := p.delay.Wait(ctx); err != nil {
				attempts = append(attempts, domain.Attempt{
					Transport:      current.Name(),
					Classification: detector.Timeout,
					OriginalError:  fmt.Sprintf("wait for provider jitter: %v", err),
				})
				break
			}
		}

		response, fetchErr := current.Fetch(ctx, request)
		attempt := domain.Attempt{
			Transport:     current.Name(),
			HeaderProfile: response.HeaderProfile,
			RequestURL:    response.RequestURL,
			HTTPStatus:    response.StatusCode,
			FinalURL:      response.FinalURL,
			ElapsedMS:     response.Elapsed.Milliseconds(),
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
			classification := detector.NetworkError
			if errors.Is(fetchErr, context.DeadlineExceeded) || errors.Is(fetchErr, context.Canceled) {
				classification = detector.Timeout
			}
			attempt.Classification = classification
			attempt.OriginalError = fetchErr.Error()
			attempts = append(attempts, attempt)
			continue
		}

		classification := detector.Classify(response.StatusCode, response.FinalURL, response.Body)
		attempt.Classification = classification
		if classification != detector.Normal {
			attempt.OriginalError = fmt.Sprintf(
				"baidu response classified as %s: status=%d final_url=%s",
				classification,
				response.StatusCode,
				response.FinalURL,
			)
			attempts = append(attempts, attempt)
			p.breaker.Trip(current.Name(), classification)
			continue
		}

		results, parserWarnings, parserErr := parseForTransport(current.Name(), response.Body, request.Limit)
		if parserErr != nil {
			attempt.Classification = detector.ParseChanged
			attempt.ParserError = parserErr.Error()
			attempt.OriginalError = parserErr.Error()
			attempts = append(attempts, attempt)
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
