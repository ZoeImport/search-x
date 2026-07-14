package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"web-search-backend/internal/domain"
	readpipe "web-search-backend/internal/read"
)

type fakeURLPolicy struct {
	target domain.SafeTarget
	err    error
}

func (p fakeURLPolicy) ValidateAndResolve(context.Context, string) (domain.SafeTarget, error) {
	return p.target, p.err
}

type fakeResourceReader struct {
	name     domain.ImplementationName
	resource domain.Resource
	err      error
	calls    int
}

func (r *fakeResourceReader) Name() domain.ImplementationName { return r.name }
func (r *fakeResourceReader) Read(context.Context, domain.SafeTarget) (domain.Resource, error) {
	r.calls++
	return r.resource, r.err
}

type fakeSourceDetector struct{}

func (fakeSourceDetector) Name() domain.ImplementationName {
	return domain.ImplementationNameMIMETypeDetector
}

func (fakeSourceDetector) Detect(resource domain.Resource) (domain.SourceType, error) {
	if resource.ContentType == "text/plain" {
		return domain.SourceTypePlainText, nil
	}
	return domain.SourceTypeHTML, nil
}

type fakeReadExtractor struct{}

func (fakeReadExtractor) Name() domain.ImplementationName {
	return domain.ImplementationNameHTMLExtractor
}
func (fakeReadExtractor) SourceTypes() []domain.SourceType {
	return []domain.SourceType{domain.SourceTypeHTML}
}
func (fakeReadExtractor) Extract(_ context.Context, resource domain.Resource) (domain.ReadDocument, error) {
	return domain.ReadDocument{
		URL: resource.URL, FinalURL: resource.FinalURL, Title: "Article",
		SourceType: domain.SourceTypeHTML, ContentText: string(resource.Body), ContentHTML: "<p>" + string(resource.Body) + "</p>",
	}, nil
}

type failingReadExtractor struct {
	err error
}

func (failingReadExtractor) Name() domain.ImplementationName {
	return domain.ImplementationNameHTMLExtractor
}
func (failingReadExtractor) SourceTypes() []domain.SourceType {
	return []domain.SourceType{domain.SourceTypeHTML}
}
func (extractor failingReadExtractor) Extract(context.Context, domain.Resource) (domain.ReadDocument, error) {
	return domain.ReadDocument{}, extractor.err
}

type browserFallbackReadExtractor struct{}

func (browserFallbackReadExtractor) Name() domain.ImplementationName {
	return domain.ImplementationNameHTMLExtractor
}
func (browserFallbackReadExtractor) SourceTypes() []domain.SourceType {
	return []domain.SourceType{domain.SourceTypeHTML}
}
func (browserFallbackReadExtractor) Extract(_ context.Context, resource domain.Resource) (domain.ReadDocument, error) {
	if resource.Transport == domain.ReadTransportHTTP {
		return domain.ReadDocument{}, codedReadFailure{code: domain.ErrCaptchaRequired}
	}
	return domain.ReadDocument{
		URL: resource.URL, FinalURL: resource.FinalURL, Title: "Rendered Article",
		SourceType: domain.SourceTypeHTML, ContentText: string(resource.Body), ContentHTML: "<p>" + string(resource.Body) + "</p>",
	}, nil
}

type fakeExtractorRegistry struct{ extractor readpipe.ContentExtractor }

func (r fakeExtractorRegistry) Resolve(domain.SourceType) (readpipe.ContentExtractor, error) {
	return r.extractor, nil
}

type fakeReadEvaluator struct {
	actions map[domain.ReadTransport]domain.QualityResult
}

func (fakeReadEvaluator) Name() domain.ImplementationName {
	return domain.ImplementationNameArticleQualityEvaluator
}

func (e fakeReadEvaluator) Evaluate(_ domain.ReadDocument, resource domain.Resource) domain.QualityResult {
	if result, ok := e.actions[resource.Transport]; ok {
		return result
	}
	return domain.QualityResult{Action: domain.QualityActionAccept, Classification: domain.ReadClassificationSuccess}
}

type fakeReadConverter struct{}

func (fakeReadConverter) Name() domain.ImplementationName {
	return domain.ImplementationNameTextConverter
}
func (fakeReadConverter) Formats() []domain.OutputFormat {
	return []domain.OutputFormat{domain.OutputFormatText}
}
func (fakeReadConverter) Convert(_ context.Context, document domain.ReadDocument) (domain.FormattedContent, error) {
	return domain.FormattedContent{Content: document.ContentText, Format: domain.OutputFormatText}, nil
}

type fakeConverterRegistry struct{ converter readpipe.ContentConverter }

func (r fakeConverterRegistry) Resolve(domain.OutputFormat) (readpipe.ContentConverter, error) {
	return r.converter, nil
}

type fakeReadCache struct {
	fresh  domain.ReadDocument
	stale  domain.ReadDocument
	set    domain.ReadDocument
	setErr error
}

type codedReadFailure struct {
	code       domain.ErrorCode
	httpStatus int
}

func (e codedReadFailure) Error() string                   { return "coded read failure" }
func (e codedReadFailure) ReadErrorCode() domain.ErrorCode { return e.code }
func (e codedReadFailure) HTTPStatusCode() int             { return e.httpStatus }

func (c *fakeReadCache) GetFresh(context.Context, string) (domain.ReadDocument, bool) {
	return c.fresh, c.fresh.URL != ""
}
func (c *fakeReadCache) GetStale(context.Context, string) (domain.ReadDocument, bool) {
	return c.stale, c.stale.URL != ""
}
func (c *fakeReadCache) Set(_ context.Context, _ string, document domain.ReadDocument) error {
	c.set = document
	return c.setErr
}

func newReadServiceForTest(t *testing.T, httpReader, browserReader *fakeResourceReader, evaluator readpipe.QualityEvaluator, store readpipe.ReadCache) *ReadService {
	t.Helper()
	targetURL, err := url.Parse("https://example.com/article")
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewReadService(ReadServiceConfig{
		Policy:        fakeURLPolicy{target: domain.SafeTarget{URL: targetURL}},
		HTTPReader:    httpReader,
		BrowserReader: browserReader,
		Detector:      fakeSourceDetector{},
		Extractors:    fakeExtractorRegistry{extractor: fakeReadExtractor{}},
		Evaluator:     evaluator,
		Converters:    fakeConverterRegistry{converter: fakeReadConverter{}},
		Cache:         store,
		Now:           time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestReadServiceReturnsHTTPContentAndCachesCanonicalDocument(t *testing.T) {
	httpReader := &fakeResourceReader{name: domain.ImplementationNameHTTPReader, resource: domain.Resource{
		URL: "https://example.com/article", FinalURL: "https://example.com/article", ContentType: "text/html",
		Body: []byte("clean body"), Transport: domain.ReadTransportHTTP,
	}}
	store := &fakeReadCache{}
	service := newReadServiceForTest(t, httpReader, nil, fakeReadEvaluator{}, store)
	response, err := service.Read(context.Background(), domain.ReadRequest{
		URL: "https://example.com/article", Format: domain.OutputFormatText, MaxChars: 1000, RequestID: "req_read", Debug: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "clean body" || response.Meta.Transport != domain.ReadTransportHTTP || store.set.ContentText != "clean body" {
		t.Fatalf("response=%#v cached=%#v", response, store.set)
	}
	if response.Debug == nil || len(response.Debug.Attempts) == 0 {
		t.Fatalf("debug=%#v", response.Debug)
	}
}

func TestReadServiceUsesBrowserOnlyForRenderQualityAction(t *testing.T) {
	httpReader := &fakeResourceReader{name: domain.ImplementationNameHTTPReader, resource: domain.Resource{
		URL: "https://example.com/article", FinalURL: "https://example.com/article", ContentType: "text/html",
		Body: []byte("shell"), Transport: domain.ReadTransportHTTP,
	}}
	browserReader := &fakeResourceReader{name: domain.ImplementationNameChromedpReader, resource: domain.Resource{
		URL: "https://example.com/article", FinalURL: "https://example.com/article", ContentType: "text/html",
		Body: []byte("rendered body"), Transport: domain.ReadTransportChromedp,
	}}
	evaluator := fakeReadEvaluator{actions: map[domain.ReadTransport]domain.QualityResult{
		domain.ReadTransportHTTP:     {Action: domain.QualityActionRender, Classification: domain.ReadClassificationJSShell},
		domain.ReadTransportChromedp: {Action: domain.QualityActionAccept, Classification: domain.ReadClassificationSuccess},
	}}
	service := newReadServiceForTest(t, httpReader, browserReader, evaluator, &fakeReadCache{})
	response, err := service.Read(context.Background(), domain.ReadRequest{
		URL: "https://example.com/article", Format: domain.OutputFormatText, MaxChars: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "rendered body" || response.Meta.Transport != domain.ReadTransportChromedp || response.Meta.FallbackCount != 1 || browserReader.calls != 1 {
		t.Fatalf("response=%#v browser_calls=%d", response, browserReader.calls)
	}
}

func TestReadServiceReturnsStaleDocumentWithLiveOriginalError(t *testing.T) {
	httpReader := &fakeResourceReader{name: domain.ImplementationNameHTTPReader, err: errors.New("dial tcp: connection refused")}
	store := &fakeReadCache{stale: domain.ReadDocument{
		URL: "https://example.com/article", FinalURL: "https://example.com/article", SourceType: domain.SourceTypeHTML,
		ContentText: "cached body", StoredAt: time.Now().Add(-time.Hour),
	}}
	service := newReadServiceForTest(t, httpReader, nil, fakeReadEvaluator{}, store)
	response, err := service.Read(context.Background(), domain.ReadRequest{
		URL: "https://example.com/article", Format: domain.OutputFormatText, MaxChars: 1000, Refresh: true, Debug: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "cached body" || !response.Meta.Cached || !response.Meta.Degraded || response.Meta.Transport != domain.ReadTransportStaleCache {
		t.Fatalf("response=%#v", response)
	}
	foundOriginal := false
	if response.Debug != nil {
		for _, attempt := range response.Debug.Attempts {
			if attempt.OriginalError == "dial tcp: connection refused" {
				foundOriginal = true
			}
		}
	}
	if response.Debug == nil || !foundOriginal {
		t.Fatalf("debug=%#v", response.Debug)
	}
}

func TestReadServicePreservesReaderErrorCode(t *testing.T) {
	httpReader := &fakeResourceReader{
		name: domain.ImplementationNameHTTPReader,
		err:  codedReadFailure{code: domain.ErrContentTooLarge},
	}
	service := newReadServiceForTest(t, httpReader, nil, fakeReadEvaluator{}, &fakeReadCache{})
	_, err := service.Read(context.Background(), domain.ReadRequest{
		URL: "https://example.com/article", Format: domain.OutputFormatText, MaxChars: 1000,
	})
	var readErr *domain.ReadError
	if !errors.As(err, &readErr) || readErr.Code != domain.ErrContentTooLarge || readErr.Retryable {
		t.Fatalf("error=%#v", err)
	}
}

func TestReadServiceMapsCaptchaQualityToCaptchaError(t *testing.T) {
	httpReader := &fakeResourceReader{name: domain.ImplementationNameHTTPReader, resource: domain.Resource{
		URL: "https://example.com/article", FinalURL: "https://example.com/article", ContentType: "text/html",
		Body: []byte("captcha"), Transport: domain.ReadTransportHTTP,
	}}
	evaluator := fakeReadEvaluator{actions: map[domain.ReadTransport]domain.QualityResult{
		domain.ReadTransportHTTP: {Action: domain.QualityActionReject, Classification: domain.ReadClassificationCaptcha, Reason: "captcha detected"},
	}}
	service := newReadServiceForTest(t, httpReader, nil, evaluator, &fakeReadCache{})
	_, err := service.Read(context.Background(), domain.ReadRequest{
		URL: "https://example.com/article", Format: domain.OutputFormatText, MaxChars: 1000,
	})
	var readErr *domain.ReadError
	if !errors.As(err, &readErr) || readErr.Code != domain.ErrCaptchaRequired || !readErr.Retryable {
		t.Fatalf("error=%#v", err)
	}
}

func TestReadServicePreservesExtractorCaptchaClassification(t *testing.T) {
	targetURL, err := url.Parse("https://example.com/article")
	if err != nil {
		t.Fatal(err)
	}
	httpReader := &fakeResourceReader{name: domain.ImplementationNameHTTPReader, resource: domain.Resource{
		URL: targetURL.String(), FinalURL: targetURL.String(), ContentType: "text/html", Body: []byte("challenge"), Transport: domain.ReadTransportHTTP,
	}}
	service, err := NewReadService(ReadServiceConfig{
		Policy: fakeURLPolicy{target: domain.SafeTarget{URL: targetURL}}, HTTPReader: httpReader,
		Detector: fakeSourceDetector{}, Extractors: fakeExtractorRegistry{extractor: failingReadExtractor{err: codedReadFailure{code: domain.ErrCaptchaRequired}}},
		Evaluator: fakeReadEvaluator{}, Converters: fakeConverterRegistry{converter: fakeReadConverter{}}, Cache: &fakeReadCache{}, Now: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Read(context.Background(), domain.ReadRequest{URL: targetURL.String(), Format: domain.OutputFormatText, MaxChars: 1000, Refresh: true})
	var readErr *domain.ReadError
	if !errors.As(err, &readErr) || readErr.Code != domain.ErrCaptchaRequired {
		t.Fatalf("error = %#v, want captcha_required", err)
	}
}

func TestReadServiceFallsBackToBrowserWhenHTTPExtractionNeedsRendering(t *testing.T) {
	targetURL, err := url.Parse("https://example.com/article")
	if err != nil {
		t.Fatal(err)
	}
	httpReader := &fakeResourceReader{name: domain.ImplementationNameHTTPReader, resource: domain.Resource{
		URL: targetURL.String(), FinalURL: targetURL.String(), ContentType: "text/html", Body: []byte("challenge"), Transport: domain.ReadTransportHTTP,
	}}
	browserReader := &fakeResourceReader{name: domain.ImplementationNameChromedpReader, resource: domain.Resource{
		URL: targetURL.String(), FinalURL: targetURL.String(), ContentType: "text/html", Body: []byte("rendered body"), Transport: domain.ReadTransportChromedp,
	}}
	service, err := NewReadService(ReadServiceConfig{
		Policy: fakeURLPolicy{target: domain.SafeTarget{URL: targetURL}}, HTTPReader: httpReader, BrowserReader: browserReader,
		Detector: fakeSourceDetector{}, Extractors: fakeExtractorRegistry{extractor: browserFallbackReadExtractor{}},
		Evaluator: fakeReadEvaluator{}, Converters: fakeConverterRegistry{converter: fakeReadConverter{}}, Cache: &fakeReadCache{}, Now: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.Read(context.Background(), domain.ReadRequest{
		URL: targetURL.String(), Format: domain.OutputFormatText, MaxChars: 1000, Refresh: true, Debug: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "rendered body" || response.Meta.Transport != domain.ReadTransportChromedp || response.Meta.FallbackCount != 1 || browserReader.calls != 1 {
		t.Fatalf("response=%#v browser_calls=%d", response, browserReader.calls)
	}
}

func TestReadServiceFallsBackToBrowserWhenHTTPReturnsForbidden(t *testing.T) {
	targetURL, err := url.Parse("https://example.com/article")
	if err != nil {
		t.Fatal(err)
	}
	httpReader := &fakeResourceReader{
		name: domain.ImplementationNameHTTPReader,
		err:  codedReadFailure{code: domain.ErrFetchFailed, httpStatus: http.StatusForbidden},
	}
	browserReader := &fakeResourceReader{name: domain.ImplementationNameChromedpReader, resource: domain.Resource{
		URL: targetURL.String(), FinalURL: targetURL.String(), ContentType: "text/html", Body: []byte("rendered forbidden body"), Transport: domain.ReadTransportChromedp,
	}}
	service := newReadServiceForTest(t, httpReader, browserReader, fakeReadEvaluator{}, &fakeReadCache{})
	response, err := service.Read(context.Background(), domain.ReadRequest{
		URL: targetURL.String(), Format: domain.OutputFormatText, MaxChars: 1000, Refresh: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "rendered forbidden body" || response.Meta.Transport != domain.ReadTransportChromedp || browserReader.calls != 1 {
		t.Fatalf("response=%#v browser_calls=%d", response, browserReader.calls)
	}
}

func TestReadServiceFreshCacheAndRefreshBehavior(t *testing.T) {
	cached := domain.ReadDocument{
		URL: "https://example.com/article", FinalURL: "https://example.com/article",
		SourceType: domain.SourceTypeHTML, ContentText: "cached body",
	}
	httpReader := &fakeResourceReader{name: domain.ImplementationNameHTTPReader, resource: domain.Resource{
		URL: cached.URL, FinalURL: cached.FinalURL, ContentType: "text/html", Body: []byte("live body"), Transport: domain.ReadTransportHTTP,
	}}
	store := &fakeReadCache{fresh: cached}
	service := newReadServiceForTest(t, httpReader, nil, fakeReadEvaluator{}, store)
	request := domain.ReadRequest{URL: cached.URL, Format: domain.OutputFormatText, MaxChars: 1000}
	response, err := service.Read(context.Background(), request)
	if err != nil || response.Content != "cached body" || response.Meta.Transport != domain.ReadTransportFreshCache || httpReader.calls != 0 {
		t.Fatalf("cached response=%#v calls=%d err=%v", response, httpReader.calls, err)
	}
	request.Refresh = true
	response, err = service.Read(context.Background(), request)
	if err != nil || response.Content != "live body" || response.Meta.Transport != domain.ReadTransportHTTP || httpReader.calls != 1 {
		t.Fatalf("refresh response=%#v calls=%d err=%v", response, httpReader.calls, err)
	}
}

func TestReadServiceReportsCacheWriteWarning(t *testing.T) {
	httpReader := &fakeResourceReader{name: domain.ImplementationNameHTTPReader, resource: domain.Resource{
		URL: "https://example.com/article", FinalURL: "https://example.com/article", ContentType: "text/html",
		Body: []byte("live body"), Transport: domain.ReadTransportHTTP,
	}}
	service := newReadServiceForTest(t, httpReader, nil, fakeReadEvaluator{}, &fakeReadCache{setErr: errors.New("cache unavailable")})
	response, err := service.Read(context.Background(), domain.ReadRequest{
		URL: "https://example.com/article", Format: domain.OutputFormatText, MaxChars: 1000, Debug: true,
	})
	if err != nil || len(response.Warnings) != 1 || response.Warnings[0].Code != domain.ReadWarningCacheWriteError || strings.Contains(response.Warnings[0].Message, "cache unavailable") {
		t.Fatalf("response=%#v err=%v", response, err)
	}
	if response.Debug == nil || !hasReadAttemptError(response.Debug.Attempts, domain.ReadStageCache, "cache unavailable") {
		t.Fatalf("debug=%#v", response.Debug)
	}
}

func hasReadAttemptError(attempts []domain.ReadAttempt, stage domain.ReadStage, originalError string) bool {
	for _, attempt := range attempts {
		if attempt.Stage == stage && attempt.OriginalError == originalError {
			return true
		}
	}
	return false
}

type blockingReadResourceReader struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

type countingReadPolicy struct {
	target    domain.SafeTarget
	validated chan struct{}
}

func (p countingReadPolicy) ValidateAndResolve(context.Context, string) (domain.SafeTarget, error) {
	p.validated <- struct{}{}
	return p.target, nil
}

func (r *blockingReadResourceReader) Name() domain.ImplementationName {
	return domain.ImplementationNameHTTPReader
}

func (r *blockingReadResourceReader) Read(context.Context, domain.SafeTarget) (domain.Resource, error) {
	r.calls.Add(1)
	select {
	case r.started <- struct{}{}:
	default:
	}
	<-r.release
	return domain.Resource{
		URL: "https://example.com/article", FinalURL: "https://example.com/article",
		ContentType: "text/html", Body: []byte("live body"), Transport: domain.ReadTransportHTTP,
	}, nil
}

type contextAwareBlockingReader struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (r *contextAwareBlockingReader) Name() domain.ImplementationName {
	return domain.ImplementationNameHTTPReader
}

func (r *contextAwareBlockingReader) Read(ctx context.Context, _ domain.SafeTarget) (domain.Resource, error) {
	r.calls.Add(1)
	select {
	case r.started <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		return domain.Resource{}, ctx.Err()
	case <-r.release:
		return domain.Resource{
			URL: "https://example.com/article", FinalURL: "https://example.com/article",
			ContentType: "text/html", Body: []byte("live body"), Transport: domain.ReadTransportHTTP,
		}, nil
	}
}

func TestReadServiceCoalescesConcurrentLiveReads(t *testing.T) {
	targetURL, err := url.Parse("https://example.com/article")
	if err != nil {
		t.Fatal(err)
	}
	httpReader := &blockingReadResourceReader{started: make(chan struct{}, 1), release: make(chan struct{})}
	validated := make(chan struct{}, 5)
	service, err := NewReadService(ReadServiceConfig{
		Policy: countingReadPolicy{target: domain.SafeTarget{URL: targetURL}, validated: validated}, HTTPReader: httpReader,
		Detector: fakeSourceDetector{}, Extractors: fakeExtractorRegistry{extractor: fakeReadExtractor{}},
		Evaluator: fakeReadEvaluator{}, Converters: fakeConverterRegistry{converter: fakeReadConverter{}},
		Cache: &fakeReadCache{}, Now: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := domain.ReadRequest{URL: targetURL.String(), Format: domain.OutputFormatText, MaxChars: 1000, Refresh: true}
	firstDone := make(chan error, 1)
	go func() {
		_, readErr := service.Read(context.Background(), request)
		firstDone <- readErr
	}()
	<-validated
	<-httpReader.started
	for range 4 {
		waiterCtx, cancelWaiter := context.WithCancel(context.Background())
		waiterDone := make(chan error, 1)
		go func() {
			_, readErr := service.Read(waiterCtx, request)
			waiterDone <- readErr
		}()
		<-validated
		cancelWaiter()
		if waiterErr := <-waiterDone; waiterErr == nil {
			t.Fatal("canceled waiter error = nil")
		}
		if httpReader.calls.Load() != 1 {
			t.Fatalf("reader calls while first flight is active=%d", httpReader.calls.Load())
		}
	}
	close(httpReader.release)
	if firstErr := <-firstDone; firstErr != nil {
		t.Fatalf("first caller error=%v", firstErr)
	}
	if httpReader.calls.Load() != 1 {
		t.Fatalf("reader calls=%d", httpReader.calls.Load())
	}
}

func TestReadServiceSharedReadOutlivesFirstCallerCancellation(t *testing.T) {
	targetURL, err := url.Parse("https://example.com/article")
	if err != nil {
		t.Fatal(err)
	}
	httpReader := &contextAwareBlockingReader{started: make(chan struct{}, 1), release: make(chan struct{})}
	validated := make(chan struct{}, 2)
	service, err := NewReadService(ReadServiceConfig{
		Policy: countingReadPolicy{target: domain.SafeTarget{URL: targetURL}, validated: validated}, HTTPReader: httpReader,
		Detector: fakeSourceDetector{}, Extractors: fakeExtractorRegistry{extractor: fakeReadExtractor{}},
		Evaluator: fakeReadEvaluator{}, Converters: fakeConverterRegistry{converter: fakeReadConverter{}},
		Cache: &fakeReadCache{}, OperationTimeout: time.Second, Now: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := domain.ReadRequest{URL: targetURL.String(), Format: domain.OutputFormatText, MaxChars: 1000, Refresh: true}
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, readErr := service.Read(firstCtx, request)
		firstDone <- readErr
	}()
	<-validated
	<-httpReader.started

	secondDone := make(chan error, 1)
	go func() {
		_, readErr := service.Read(context.Background(), request)
		secondDone <- readErr
	}()
	<-validated
	cancelFirst()
	if firstErr := <-firstDone; firstErr == nil {
		t.Fatal("first caller error = nil, want cancellation")
	}
	close(httpReader.release)
	if secondErr := <-secondDone; secondErr != nil {
		t.Fatalf("second caller error = %v", secondErr)
	}
	if httpReader.calls.Load() != 1 {
		t.Fatalf("reader calls=%d", httpReader.calls.Load())
	}
}
