package baidu

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"web-search-backend/internal/detector"
	"web-search-backend/internal/domain"
	"web-search-backend/internal/headerprofile"
	"web-search-backend/internal/transport"
)

// SessionConfig defines the fixed Baidu Cookie session behavior.
type SessionConfig struct {
	BootstrapURL      string
	SearchURL         string
	RequestTimeout    time.Duration
	MinInterval       time.Duration
	MaxJitter         time.Duration
	CaptchaCooldown   time.Duration
	RateLimitCooldown time.Duration
	FallbackReserve   time.Duration
	MaxBodyBytes      int64
}

// SessionClientFactory creates a new HTTP client with a fresh CookieJar.
type SessionClientFactory interface {
	New() (*http.Client, error)
}

// SessionPacer enforces a cancellable delay after the previous upstream attempt.
type SessionPacer interface {
	Wait(context.Context, time.Time, time.Duration) (time.Duration, error)
}

// ResponseClassifier classifies a Baidu response using provider-specific evidence.
type ResponseClassifier interface {
	Classify(int, string, string, []byte) domain.Classification
}

// SessionError preserves the upstream classification across the transport boundary.
type SessionError struct {
	Classification domain.Classification
	RetryAfter     time.Duration
	Original       error
}

func (err *SessionError) Error() string {
	if err == nil {
		return "Baidu session error"
	}
	if err.Original != nil {
		return err.Original.Error()
	}
	return fmt.Sprintf("Baidu session classified as %s", err.Classification)
}

func (err *SessionError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Original
}

type baiduSession struct {
	state                 domain.BaiduSessionState
	client                *http.Client
	lastFinished          time.Time
	blockedUntil          time.Time
	blockedClassification domain.Classification
	generation            uint64
}

// BaiduSessionTransport implements one fixed-identity, serialized Baidu Cookie session.
type BaiduSessionTransport struct {
	config        SessionConfig
	profile       headerprofile.Profile
	clientFactory SessionClientFactory
	pacer         SessionPacer
	classifier    ResponseClassifier
	now           func() time.Time
	gate          chan struct{}
	session       baiduSession
}

// NewSessionTransport validates dependencies and creates a cold fixed Baidu session.
func NewSessionTransport(
	config SessionConfig,
	profile headerprofile.Profile,
	clientFactory SessionClientFactory,
	pacer SessionPacer,
	classifier ResponseClassifier,
	now func() time.Time,
) (*BaiduSessionTransport, error) {
	if err := validateSessionConfig(config); err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(profile.Name)) == "" || strings.TrimSpace(profile.UserAgent) == "" || strings.TrimSpace(profile.AcceptLanguage) == "" {
		return nil, fmt.Errorf("Baidu fixed session header profile is incomplete")
	}
	if clientFactory == nil {
		return nil, fmt.Errorf("Baidu session client factory is nil")
	}
	if pacer == nil {
		return nil, fmt.Errorf("Baidu session pacer is nil")
	}
	if classifier == nil {
		return nil, fmt.Errorf("Baidu response classifier is nil")
	}
	if now == nil {
		now = time.Now
	}
	return &BaiduSessionTransport{
		config: config, profile: profile, clientFactory: clientFactory, pacer: pacer,
		classifier: classifier, now: now, gate: make(chan struct{}, 1),
		session: baiduSession{state: domain.BaiduSessionStateCold},
	}, nil
}

func validateSessionConfig(config SessionConfig) error {
	for name, raw := range map[string]string{"bootstrap": config.BootstrapURL, "search": config.SearchURL} {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("invalid Baidu session %s URL %q", name, raw)
		}
	}
	if config.RequestTimeout <= 0 || config.MinInterval <= 0 || config.CaptchaCooldown <= 0 || config.RateLimitCooldown <= 0 {
		return fmt.Errorf("Baidu session timeouts and cooldowns must be positive")
	}
	if config.MaxJitter < 0 || config.FallbackReserve < 0 {
		return fmt.Errorf("Baidu session jitter and fallback reserve must not be negative")
	}
	if config.MaxBodyBytes <= 0 {
		return fmt.Errorf("Baidu session max body bytes must be positive")
	}
	return nil
}

// Name returns the concrete fixed-session transport name.
func (*BaiduSessionTransport) Name() domain.TransportName {
	return domain.TransportNameBaiduSessionHTTP
}

// Fetch bootstraps, paces, and executes one serialized Baidu search request.
func (transportValue *BaiduSessionTransport) Fetch(ctx context.Context, request domain.SearchRequest) (transport.Response, error) {
	requestURL, err := BuildSearchURL(transportValue.config.SearchURL, request)
	if err != nil {
		return transport.Response{}, err
	}
	response := transport.Response{RequestURL: requestURL, HeaderProfile: string(transportValue.profile.Name)}
	if request.Page > 1 {
		response.Classification = domain.ClassificationParseChanged
		return response, &SessionError{
			Classification: domain.ClassificationParseChanged,
			Original:       fmt.Errorf("Baidu fixed session only supports the natural first-page URL; requested page=%d", request.Page),
		}
	}
	if err := transportValue.acquire(ctx); err != nil {
		return response, &SessionError{Classification: domain.ClassificationTimeout, Original: fmt.Errorf("wait for Baidu session gate: %w", err)}
	}
	defer transportValue.release()

	if coolingErr := transportValue.handleCooling(&response); coolingErr != nil {
		return response, coolingErr
	}

	totalWait := time.Duration(0)
	if transportValue.session.state == domain.BaiduSessionStateCold {
		client, createErr := transportValue.clientFactory.New()
		if createErr != nil {
			return transportValue.decorate(response, totalWait), &SessionError{
				Classification: domain.ClassificationNetworkError,
				Original:       fmt.Errorf("create Baidu session client: %w", createErr),
			}
		}
		transportValue.session.client = client
		transportValue.session.generation++

		bootstrapReserve := 2*transportValue.config.RequestTimeout + transportValue.config.MinInterval +
			transportValue.config.MaxJitter + transportValue.config.FallbackReserve
		waited, waitErr := transportValue.pacer.Wait(ctx, transportValue.session.lastFinished, bootstrapReserve)
		totalWait += waited
		if waitErr != nil {
			return transportValue.decorate(response, totalWait), &SessionError{
				Classification: domain.ClassificationTimeout,
				Original:       fmt.Errorf("wait before Baidu bootstrap: %w", waitErr),
			}
		}

		bootstrapResponse, bootstrapErr := transportValue.fetchURL(ctx, transportValue.config.BootstrapURL, false)
		bootstrapResponse.SessionWait = totalWait
		bootstrapClassification := transportValue.classifier.Classify(
			bootstrapResponse.StatusCode, bootstrapResponse.FinalURL, bootstrapResponse.PageTitle, bootstrapResponse.Body,
		)
		bootstrapResponse.Classification = bootstrapClassification
		if bootstrapErr != nil {
			bootstrapClassification = classificationForFetchError(bootstrapErr)
			bootstrapResponse.Classification = bootstrapClassification
			return transportValue.decorate(bootstrapResponse, totalWait), transportValue.sessionFailure(bootstrapClassification, bootstrapErr, &bootstrapResponse)
		}
		if isRiskClassification(bootstrapClassification) {
			failure := transportValue.sessionFailure(
				bootstrapClassification,
				fmt.Errorf("Baidu bootstrap classified as %s: status=%d final_url=%s title=%q", bootstrapClassification, bootstrapResponse.StatusCode, bootstrapResponse.FinalURL, bootstrapResponse.PageTitle),
				&bootstrapResponse,
			)
			return transportValue.decorate(bootstrapResponse, totalWait), failure
		}
		if bootstrapResponse.StatusCode < http.StatusOK || bootstrapResponse.StatusCode >= http.StatusBadRequest {
			return transportValue.decorate(bootstrapResponse, totalWait), &SessionError{
				Classification: domain.ClassificationNetworkError,
				Original:       fmt.Errorf("Baidu bootstrap returned HTTP %d", bootstrapResponse.StatusCode),
			}
		}
		transportValue.session.state = domain.BaiduSessionStateWarm
	}

	waited, waitErr := transportValue.pacer.Wait(
		ctx,
		transportValue.session.lastFinished,
		transportValue.config.RequestTimeout+transportValue.config.FallbackReserve,
	)
	totalWait += waited
	if waitErr != nil {
		return transportValue.decorate(response, totalWait), &SessionError{
			Classification: domain.ClassificationTimeout,
			Original:       fmt.Errorf("wait before Baidu search: %w", waitErr),
		}
	}

	response, fetchErr := transportValue.fetchURL(ctx, requestURL, true)
	response.SessionWait = totalWait
	classification := transportValue.classifier.Classify(response.StatusCode, response.FinalURL, response.PageTitle, response.Body)
	response.Classification = classification
	if fetchErr != nil {
		classification = classificationForFetchError(fetchErr)
		response.Classification = classification
		return transportValue.decorate(response, totalWait), transportValue.sessionFailure(classification, fetchErr, &response)
	}
	if classification != domain.ClassificationNormal && classification != domain.ClassificationEmpty {
		failure := transportValue.sessionFailure(
			classification,
			fmt.Errorf("Baidu response classified as %s: status=%d final_url=%s title=%q", classification, response.StatusCode, response.FinalURL, response.PageTitle),
			&response,
		)
		return transportValue.decorate(response, totalWait), failure
	}
	return transportValue.decorate(response, totalWait), nil
}

func (transportValue *BaiduSessionTransport) acquire(ctx context.Context) error {
	select {
	case transportValue.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (transportValue *BaiduSessionTransport) release() {
	<-transportValue.gate
}

func (transportValue *BaiduSessionTransport) handleCooling(response *transport.Response) error {
	if transportValue.session.state != domain.BaiduSessionStateCooling {
		return nil
	}
	if !transportValue.now().Before(transportValue.session.blockedUntil) {
		transportValue.session.state = domain.BaiduSessionStateCold
		transportValue.session.client = nil
		transportValue.session.blockedUntil = time.Time{}
		transportValue.session.blockedClassification = ""
		return nil
	}
	transportValue.decorateInto(response, 0)
	retryAfter := transportValue.session.blockedUntil.Sub(transportValue.now())
	return &SessionError{
		Classification: transportValue.session.blockedClassification,
		RetryAfter:     retryAfter,
		Original: fmt.Errorf(
			"Baidu fixed session is cooling until %s after %s",
			transportValue.session.blockedUntil.Format(time.RFC3339Nano),
			transportValue.session.blockedClassification,
		),
	}
}

func (transportValue *BaiduSessionTransport) fetchURL(ctx context.Context, requestURL string, search bool) (response transport.Response, returnErr error) {
	response.RequestURL = requestURL
	response.HeaderProfile = string(transportValue.profile.Name)
	requestContext, cancel := context.WithTimeout(ctx, transportValue.config.RequestTimeout)
	defer cancel()

	httpRequest, err := http.NewRequestWithContext(requestContext, http.MethodGet, requestURL, nil)
	if err != nil {
		return response, fmt.Errorf("create Baidu session request: %w", err)
	}
	transportValue.applyHeaders(httpRequest)
	if search {
		httpRequest.Header.Set("Referer", transportValue.config.BootstrapURL)
	}

	started := transportValue.now()
	defer func() {
		transportValue.session.lastFinished = transportValue.now()
	}()
	httpResponse, err := transportValue.session.client.Do(httpRequest)
	response.Elapsed = transportValue.now().Sub(started)
	if err != nil {
		return response, fmt.Errorf("Baidu session request: %w", err)
	}
	defer httpResponse.Body.Close()

	response.StatusCode = httpResponse.StatusCode
	response.FinalURL = httpResponse.Request.URL.String()
	response.Headers = httpResponse.Header.Clone()
	body, err := io.ReadAll(io.LimitReader(httpResponse.Body, transportValue.config.MaxBodyBytes+1))
	if err != nil {
		return response, fmt.Errorf("read Baidu session response: %w", err)
	}
	if int64(len(body)) > transportValue.config.MaxBodyBytes {
		response.Body = body[:transportValue.config.MaxBodyBytes]
		response.PageTitle = detector.ExtractTitle(response.Body)
		return response, fmt.Errorf("Baidu session response body exceeds %d bytes", transportValue.config.MaxBodyBytes)
	}
	response.Body = body
	response.PageTitle = detector.ExtractTitle(body)
	return response, nil
}

func (transportValue *BaiduSessionTransport) applyHeaders(request *http.Request) {
	request.Header.Set("User-Agent", transportValue.profile.UserAgent)
	request.Header.Set("Accept-Language", transportValue.profile.AcceptLanguage)
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	for key, value := range transportValue.profile.Headers {
		request.Header.Set(key, value)
	}
}

func (transportValue *BaiduSessionTransport) sessionFailure(classification domain.Classification, original error, response *transport.Response) error {
	if classification == "" {
		if errors.Is(original, context.DeadlineExceeded) || errors.Is(original, context.Canceled) {
			classification = domain.ClassificationTimeout
		} else {
			classification = domain.ClassificationNetworkError
		}
	}
	if isRiskClassification(classification) {
		cooldown := transportValue.config.RateLimitCooldown
		if classification == domain.ClassificationCaptcha {
			cooldown = transportValue.config.CaptchaCooldown
		}
		transportValue.session.state = domain.BaiduSessionStateCooling
		transportValue.session.client = nil
		transportValue.session.blockedClassification = classification
		transportValue.session.blockedUntil = transportValue.now().Add(cooldown)
		if response != nil {
			transportValue.decorateInto(response, response.SessionWait)
		}
		return &SessionError{Classification: classification, RetryAfter: cooldown, Original: original}
	}
	return &SessionError{Classification: classification, Original: original}
}

func classificationForFetchError(err error) domain.Classification {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return domain.ClassificationTimeout
	}
	return domain.ClassificationNetworkError
}

func isRiskClassification(classification domain.Classification) bool {
	switch classification {
	case domain.ClassificationCaptcha, domain.ClassificationRateLimited, domain.ClassificationBlocked:
		return true
	default:
		return false
	}
}

func (transportValue *BaiduSessionTransport) decorate(response transport.Response, wait time.Duration) transport.Response {
	transportValue.decorateInto(&response, wait)
	return response
}

func (transportValue *BaiduSessionTransport) decorateInto(response *transport.Response, wait time.Duration) {
	response.HeaderProfile = string(transportValue.profile.Name)
	response.SessionState = transportValue.session.state
	response.SessionGeneration = transportValue.session.generation
	response.SessionWait = wait
	response.BlockedUntil = transportValue.session.blockedUntil
}

type httpSessionClientFactory struct {
	transport http.RoundTripper
}

// NewHTTPSessionClientFactory creates fresh Cookie clients over a shared connection pool.
func NewHTTPSessionClientFactory(roundTripper http.RoundTripper) SessionClientFactory {
	if roundTripper == nil {
		roundTripper = http.DefaultTransport
	}
	return &httpSessionClientFactory{transport: roundTripper}
}

func (factory *httpSessionClientFactory) New() (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create Baidu CookieJar: %w", err)
	}
	return &http.Client{Transport: factory.transport, Jar: jar}, nil
}

// BaiduResponseClassifier delegates to the provider-specific response detector.
type BaiduResponseClassifier struct{}

// Classify returns the stable Baidu response classification.
func (BaiduResponseClassifier) Classify(status int, finalURL, pageTitle string, body []byte) domain.Classification {
	return detector.Classify(status, finalURL, pageTitle, body)
}

// FixedSessionPacer enforces the configured session interval with random jitter.
type FixedSessionPacer struct {
	minInterval time.Duration
	maxJitter   time.Duration
	now         func() time.Time
	random      func(int64) (int64, error)
}

// NewFixedSessionPacer creates a context-aware fixed-session pacer.
func NewFixedSessionPacer(minInterval, maxJitter time.Duration, now func() time.Time) (*FixedSessionPacer, error) {
	return newFixedSessionPacer(minInterval, maxJitter, now, func(upperExclusive int64) (int64, error) {
		value, err := rand.Int(rand.Reader, big.NewInt(upperExclusive))
		if err != nil {
			return 0, err
		}
		return value.Int64(), nil
	})
}

func newFixedSessionPacer(
	minInterval, maxJitter time.Duration,
	now func() time.Time,
	random func(int64) (int64, error),
) (*FixedSessionPacer, error) {
	if minInterval <= 0 || maxJitter < 0 {
		return nil, fmt.Errorf("invalid Baidu session pacing: min=%s jitter=%s", minInterval, maxJitter)
	}
	if now == nil {
		now = time.Now
	}
	if random == nil {
		return nil, fmt.Errorf("Baidu session random source is nil")
	}
	return &FixedSessionPacer{minInterval: minInterval, maxJitter: maxJitter, now: now, random: random}, nil
}

// Wait blocks until the fixed session interval is satisfied or the context is cancelled.
func (pacer *FixedSessionPacer) Wait(ctx context.Context, lastFinished time.Time, reserve time.Duration) (time.Duration, error) {
	delay := time.Duration(0)
	if !lastFinished.IsZero() {
		jitter := time.Duration(0)
		if pacer.maxJitter > 0 {
			offset, err := pacer.random(int64(pacer.maxJitter) + 1)
			if err != nil {
				return 0, fmt.Errorf("generate Baidu session jitter: %w", err)
			}
			jitter = time.Duration(offset)
		}
		delay = pacer.minInterval + jitter - pacer.now().Sub(lastFinished)
		if delay < 0 {
			delay = 0
		}
	}
	if deadline, exists := ctx.Deadline(); exists && deadline.Sub(pacer.now()) < delay+reserve {
		return 0, fmt.Errorf("Baidu session deadline cannot preserve %s after %s wait: %w", reserve, delay, context.DeadlineExceeded)
	}
	if delay == 0 {
		return 0, ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-timer.C:
		return delay, nil
	}
}
