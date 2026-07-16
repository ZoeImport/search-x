// Package reader contains bounded resource acquisition implementations.
package reader

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"web-search-backend/webfetch/internal/domain"
	readpipeline "web-search-backend/webfetch/internal/fetch"
)

const defaultUserAgent = "web-search-backend/webfetch/1.0"

// Config configures a bounded HTTP reader.
type Config struct {
	// Timeout limits the complete direct HTTP read including redirects and body reading.
	Timeout time.Duration
	// MaxBodyBytes bounds the decompressed response body.
	MaxBodyBytes int64
	// MaxRedirects bounds followed redirect hops.
	MaxRedirects int
	// UserAgent is the non-sensitive outbound user agent.
	UserAgent string
}

// Error retains a stable read error code and the original low-level error.
type Error struct {
	// Code is the stable API-level error classification.
	Code domain.ErrorCode
	// StatusCode is the last upstream HTTP status, when available.
	StatusCode int
	// Original retains the low-level policy, network, or parsing failure.
	Original error
}

// Error returns the original error text for development diagnostics.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Original != nil {
		return e.Original.Error()
	}
	return string(e.Code)
}

// Unwrap exposes the original error for errors.Is and errors.As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Original
}

// ReadErrorCode returns the stable read-pipeline classification.
func (e *Error) ReadErrorCode() domain.ErrorCode {
	if e == nil {
		return domain.ErrFetchFailed
	}
	return e.Code
}

// HTTPStatusCode returns the last upstream status for fallback decisions.
func (e *Error) HTTPStatusCode() int {
	if e == nil {
		return 0
	}
	return e.StatusCode
}

// HTTPReader reads HTML and plain-text resources through pinned addresses.
type HTTPReader struct {
	policy        readpipeline.URLPolicy
	timeout       time.Duration
	maxBodyBytes  int64
	maxRedirects  int
	userAgent     string
	baseTransport *http.Transport
}

// NewHTTPReader constructs an HTTP reader with an independent transport.
func NewHTTPReader(policy readpipeline.URLPolicy, cfg Config) (*HTTPReader, error) {
	if policy == nil {
		return nil, errors.New("URL policy is required")
	}
	if cfg.Timeout <= 0 {
		return nil, errors.New("timeout must be positive")
	}
	if cfg.MaxBodyBytes <= 0 {
		return nil, errors.New("max body bytes must be positive")
	}
	if cfg.MaxRedirects < 0 {
		return nil, errors.New("max redirects must not be negative")
	}
	if strings.TrimSpace(cfg.UserAgent) == "" {
		cfg.UserAgent = defaultUserAgent
	}
	return &HTTPReader{
		policy:        policy,
		timeout:       cfg.Timeout,
		maxBodyBytes:  cfg.MaxBodyBytes,
		maxRedirects:  cfg.MaxRedirects,
		userAgent:     cfg.UserAgent,
		baseTransport: newBaseTransport(),
	}, nil
}

// Name returns the typed implementation name.
func (r *HTTPReader) Name() domain.ImplementationName {
	return domain.ImplementationNameHTTPReader
}

// Read fetches one safe target and revalidates each redirect through the URL policy.
func (r *HTTPReader) Read(ctx context.Context, target domain.SafeTarget) (domain.Resource, error) {
	if target.URL == nil || len(target.Addresses) == 0 {
		return domain.Resource{}, newReaderError(domain.ErrUnsafeURL, 0, errors.New("safe target is incomplete"))
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	requestURL := target.URL.String()
	current := target
	for redirectCount := 0; ; redirectCount++ {
		response, err := r.fetch(ctx, current)
		if err != nil {
			return domain.Resource{}, classifyFetchError(err)
		}
		if isRedirect(response.StatusCode) {
			location := response.Header.Get("Location")
			_ = response.Body.Close()
			if location == "" {
				return domain.Resource{}, newReaderError(domain.ErrFetchFailed, response.StatusCode, errors.New("redirect response has no Location header"))
			}
			if redirectCount >= r.maxRedirects {
				return domain.Resource{}, newReaderError(domain.ErrFetchFailed, response.StatusCode, fmt.Errorf("redirect limit %d exceeded", r.maxRedirects))
			}
			nextURL, err := current.URL.Parse(location)
			if err != nil {
				return domain.Resource{}, newReaderError(domain.ErrFetchFailed, response.StatusCode, err)
			}
			current, err = r.policy.ValidateAndResolve(ctx, nextURL.String())
			if err != nil {
				return domain.Resource{}, newReaderError(readErrorCode(err), response.StatusCode, err)
			}
			continue
		}

		resource, err := r.readResponse(response, requestURL, current.URL.String())
		if err != nil {
			return domain.Resource{}, err
		}
		return resource, nil
	}
}

func (r *HTTPReader) fetch(ctx context.Context, target domain.SafeTarget) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9")
	request.Header.Set("User-Agent", r.userAgent)

	transport := r.baseTransport.Clone()
	transport.DialContext = pinnedDialContext(target)
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	}
	transport.TLSClientConfig.ServerName = target.URL.Hostname()
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	response.Body = &closingBody{ReadCloser: response.Body, closeTransport: transport.CloseIdleConnections}
	return response, nil
}

func (r *HTTPReader) readResponse(response *http.Response, requestURL, finalURL string) (domain.Resource, error) {
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return domain.Resource{}, newReaderError(domain.ErrFetchFailed, response.StatusCode, fmt.Errorf("upstream returned HTTP %d", response.StatusCode))
	}

	contentType, charset, hasContentType, parseErr := parseContentType(response.Header.Get("Content-Type"))
	body, err := io.ReadAll(io.LimitReader(response.Body, r.maxBodyBytes+1))
	if err != nil {
		return domain.Resource{}, classifyFetchError(err)
	}
	if int64(len(body)) > r.maxBodyBytes {
		return domain.Resource{}, newReaderError(domain.ErrContentTooLarge, response.StatusCode, fmt.Errorf("decompressed body exceeds %d bytes", r.maxBodyBytes))
	}
	if parseErr != nil || !hasContentType || !supportedContentType(contentType) {
		detectedType, _, _, detectErr := parseContentType(http.DetectContentType(bodyPreview(body)))
		if detectErr != nil || !supportedContentType(detectedType) {
			if parseErr != nil {
				return domain.Resource{}, newReaderError(domain.ErrUnsupportedContentType, response.StatusCode, parseErr)
			}
			return domain.Resource{}, newReaderError(domain.ErrUnsupportedContentType, response.StatusCode, fmt.Errorf("unsupported content type %q; detected %q", contentType, detectedType))
		}
		contentType = detectedType
		charset = ""
	}
	return domain.Resource{
		URL:         requestURL,
		FinalURL:    finalURL,
		StatusCode:  response.StatusCode,
		ContentType: contentType,
		Charset:     charset,
		Headers:     response.Header.Clone(),
		Body:        body,
		Transport:   domain.ReadTransportHTTP,
	}, nil
}

func newBaseTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

func pinnedDialContext(target domain.SafeTarget) func(context.Context, string, string) (net.Conn, error) {
	port := target.URL.Port()
	if port == "" {
		if target.URL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	addresses := append([]net.IP(nil), target.Addresses...)
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		var dialErrors []error
		for _, address := range addresses {
			connection, err := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(address.String(), port))
			if err == nil {
				return connection, nil
			}
			dialErrors = append(dialErrors, err)
		}
		return nil, errors.Join(dialErrors...)
	}
}

func parseContentType(rawContentType string) (contentType, charset string, present bool, err error) {
	if strings.TrimSpace(rawContentType) == "" {
		return "", "", false, nil
	}
	mediaType, parameters, err := mime.ParseMediaType(rawContentType)
	if err != nil {
		return "", "", true, err
	}
	return strings.ToLower(mediaType), strings.ToLower(parameters["charset"]), true, nil
}

func supportedContentType(contentType string) bool {
	return contentType == "text/html" || contentType == "application/xhtml+xml" || contentType == "text/plain"
}

func bodyPreview(body []byte) []byte {
	if len(body) <= 512 {
		return body
	}
	return body[:512]
}

func isRedirect(statusCode int) bool {
	return statusCode == http.StatusMovedPermanently || statusCode == http.StatusFound || statusCode == http.StatusSeeOther ||
		statusCode == http.StatusTemporaryRedirect || statusCode == http.StatusPermanentRedirect
}

func classifyFetchError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return newReaderError(domain.ErrFetchTimeout, 0, err)
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return newReaderError(domain.ErrFetchTimeout, 0, err)
	}
	return newReaderError(domain.ErrFetchFailed, 0, err)
}

func newReaderError(code domain.ErrorCode, statusCode int, original error) error {
	return &Error{Code: code, StatusCode: statusCode, Original: original}
}

func readErrorCode(err error) domain.ErrorCode {
	var codedError interface {
		ReadErrorCode() domain.ErrorCode
	}
	if errors.As(err, &codedError) {
		return codedError.ReadErrorCode()
	}
	return domain.ErrUnsafeURL
}

type closingBody struct {
	io.ReadCloser
	closeTransport func()
}

func (b *closingBody) Close() error {
	err := b.ReadCloser.Close()
	b.closeTransport()
	return err
}
