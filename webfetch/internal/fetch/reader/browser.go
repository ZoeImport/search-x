package reader

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"web-search-backend/webfetch/internal/domain"
	readpipeline "web-search-backend/webfetch/internal/fetch"
	"web-search-backend/webfetch/internal/transport"
)

// BrowserFetcher renders one URL and returns the final DOM snapshot.
type BrowserFetcher interface {
	FetchURL(ctx context.Context, rawURL string) (transport.Response, error)
}

// BrowserReader adapts a rendered browser response to the read pipeline.
type BrowserReader struct {
	policy       readpipeline.URLPolicy
	fetcher      BrowserFetcher
	maxBodyBytes int64
}

// NewBrowserReader creates a bounded browser-backed resource reader.
func NewBrowserReader(policy readpipeline.URLPolicy, fetcher BrowserFetcher, maxBodyBytes int64) (*BrowserReader, error) {
	if policy == nil {
		return nil, errors.New("URL policy is required")
	}
	if fetcher == nil {
		return nil, errors.New("browser fetcher is required")
	}
	if maxBodyBytes <= 0 {
		return nil, errors.New("max body bytes must be positive")
	}
	return &BrowserReader{policy: policy, fetcher: fetcher, maxBodyBytes: maxBodyBytes}, nil
}

// Name returns the typed browser-reader implementation name.
func (*BrowserReader) Name() domain.ImplementationName {
	return domain.ImplementationNameChromedpReader
}

// Read renders an already validated URL, then revalidates its final URL.
func (reader *BrowserReader) Read(ctx context.Context, target domain.SafeTarget) (domain.Resource, error) {
	if target.URL == nil {
		return domain.Resource{}, newReaderError(domain.ErrUnsafeURL, 0, errors.New("safe target URL is missing"))
	}
	requestURL := target.URL.String()
	response, err := reader.fetcher.FetchURL(ctx, requestURL)
	if err != nil {
		return domain.Resource{}, classifyFetchError(err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return domain.Resource{}, newReaderError(domain.ErrFetchFailed, response.StatusCode, fmt.Errorf("browser returned HTTP %d", response.StatusCode))
	}
	if int64(len(response.Body)) > reader.maxBodyBytes {
		return domain.Resource{}, newReaderError(domain.ErrContentTooLarge, response.StatusCode, fmt.Errorf("rendered DOM exceeds %d bytes", reader.maxBodyBytes))
	}
	finalURL := strings.TrimSpace(response.FinalURL)
	if finalURL == "" {
		finalURL = requestURL
	}
	validatedFinal, err := reader.policy.ValidateAndResolve(ctx, finalURL)
	if err != nil {
		return domain.Resource{}, newReaderError(readErrorCode(err), response.StatusCode, err)
	}
	if validatedFinal.URL == nil {
		return domain.Resource{}, newReaderError(domain.ErrUnsafeURL, response.StatusCode, errors.New("final URL policy returned a nil URL"))
	}

	contentType, charset, _, parseErr := parseContentType(response.Headers.Get("Content-Type"))
	if parseErr != nil || !supportedContentType(contentType) {
		contentType = "text/html"
		charset = ""
	}
	return domain.Resource{
		URL: requestURL, FinalURL: validatedFinal.URL.String(), StatusCode: response.StatusCode,
		ContentType: contentType, Charset: charset, Headers: response.Headers.Clone(), Body: response.Body,
		Transport: domain.ReadTransportChromedp,
	}, nil
}
