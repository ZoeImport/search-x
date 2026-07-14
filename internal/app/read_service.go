package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/singleflight"

	"web-search-backend/internal/domain"
	readpipe "web-search-backend/internal/read"
	readconverter "web-search-backend/internal/read/converter"
)

// ReadServiceConfig contains replaceable read-pipeline dependencies.
type ReadServiceConfig struct {
	// Policy validates and resolves the requested URL.
	Policy readpipe.URLPolicy
	// HTTPReader performs the primary direct resource read.
	HTTPReader readpipe.ResourceReader
	// BrowserReader optionally renders JavaScript shell pages.
	BrowserReader readpipe.ResourceReader
	// Detector classifies fetched resource types.
	Detector readpipe.SourceTypeDetector
	// Extractors resolves a source-specific extractor.
	Extractors readpipe.ExtractorRegistry
	// Evaluator selects accept, render, or reject.
	Evaluator readpipe.QualityEvaluator
	// Converters resolves the requested output converter.
	Converters readpipe.ConverterRegistry
	// Cache stores format-independent canonical documents.
	Cache readpipe.ReadCache
	// OperationTimeout bounds a shared live-read operation independently of any one caller.
	OperationTimeout time.Duration
	// Now supplies time for deterministic tests.
	Now func() time.Time
}

// ReadService orchestrates safe resource reading and content conversion.
type ReadService struct {
	config ReadServiceConfig
	group  singleflight.Group
}

type liveReadResult struct {
	document      domain.ReadDocument
	transport     domain.ReadTransport
	extractor     domain.ImplementationName
	fallbackCount int
	warnings      []domain.ReadWarning
	attempts      []domain.ReadAttempt
}

// NewReadService validates dependencies and creates a read service.
func NewReadService(config ReadServiceConfig) (*ReadService, error) {
	if config.Policy == nil || config.HTTPReader == nil || config.Detector == nil || config.Extractors == nil ||
		config.Evaluator == nil || config.Converters == nil || config.Cache == nil {
		return nil, fmt.Errorf("read service dependencies must not be nil")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.OperationTimeout <= 0 {
		config.OperationTimeout = 20 * time.Second
	}
	return &ReadService{config: config}, nil
}

// Read retrieves, extracts, caches, and formats one public resource.
func (s *ReadService) Read(ctx context.Context, request domain.ReadRequest) (domain.ReadResponse, error) {
	started := s.config.Now()
	normalized, err := request.Normalize()
	if err != nil {
		return domain.ReadResponse{}, readError(domain.ErrInvalidRequest, "正文读取参数错误", false, err, nil)
	}

	policyStarted := s.config.Now()
	target, err := s.config.Policy.ValidateAndResolve(ctx, normalized.URL)
	policyAttempt := domain.ReadAttempt{
		Stage: domain.ReadStagePolicy, Implementation: domain.ImplementationNameSafeURLPolicy,
		Classification: domain.ReadClassificationSuccess, RequestURL: normalized.URL,
		ElapsedMS: s.config.Now().Sub(policyStarted).Milliseconds(),
	}
	if err != nil {
		policyAttempt.Classification = domain.ReadClassificationUnsafe
		policyAttempt.OriginalError = err.Error()
		return domain.ReadResponse{}, readError(domain.ErrUnsafeURL, "目标 URL 不允许访问", false, err, []domain.ReadAttempt{policyAttempt})
	}
	if target.URL == nil {
		err = errors.New("URL policy returned a nil target URL")
		policyAttempt.Classification = domain.ReadClassificationUnsafe
		policyAttempt.OriginalError = err.Error()
		return domain.ReadResponse{}, readError(domain.ErrUnsafeURL, "目标 URL 不允许访问", false, err, []domain.ReadAttempt{policyAttempt})
	}
	key := target.URL.String()
	if !normalized.Refresh {
		if document, ok := s.config.Cache.GetFresh(ctx, key); ok {
			return s.formatResponse(ctx, normalized, document, domain.ReadTransportFreshCache, cachedExtractor(document), 0, true, false, nil, []domain.ReadAttempt{policyAttempt}, started)
		}
	}

	resultChannel := s.group.DoChan(key, func() (any, error) {
		operationCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.config.OperationTimeout)
		defer cancel()
		if !normalized.Refresh {
			if document, ok := s.config.Cache.GetFresh(operationCtx, key); ok {
				return liveReadResult{document: document, transport: domain.ReadTransportFreshCache, extractor: cachedExtractor(document)}, nil
			}
		}
		result, liveErr := s.readLive(operationCtx, target, policyAttempt)
		if liveErr != nil {
			return liveReadResult{}, liveErr
		}
		if cacheErr := s.config.Cache.Set(operationCtx, key, result.document); cacheErr != nil {
			result.warnings = append(result.warnings, domain.ReadWarning{
				Code: domain.ReadWarningCacheWriteError, Message: "正文缓存写入失败，本次仍返回实时结果",
			})
			result.attempts = append(result.attempts, domain.ReadAttempt{
				Stage: domain.ReadStageCache, Implementation: domain.ImplementationNameMemoryReadCache,
				Classification: domain.ReadClassificationRejected, OriginalError: cacheErr.Error(),
			})
		}
		return result, nil
	})

	select {
	case <-ctx.Done():
		attempt := domain.ReadAttempt{
			Stage: domain.ReadStageRead, Implementation: s.config.HTTPReader.Name(),
			Classification: domain.ReadClassificationTimeout, RequestURL: normalized.URL, OriginalError: ctx.Err().Error(),
		}
		return domain.ReadResponse{}, readError(domain.ErrFetchTimeout, "正文读取超时", true, ctx.Err(), []domain.ReadAttempt{attempt})
	case flight := <-resultChannel:
		if flight.Err != nil {
			if stale, ok := s.config.Cache.GetStale(ctx, key); ok {
				attempts := attemptsFromReadError(flight.Err)
				warnings := []domain.ReadWarning{{Code: domain.ReadWarningLiveReadUnavailable, Message: "实时正文读取不可用，当前返回旧缓存"}}
				return s.formatResponse(ctx, normalized, stale, domain.ReadTransportStaleCache, cachedExtractor(stale), 0, true, true, warnings, attempts, started)
			}
			return domain.ReadResponse{}, flight.Err
		}
		result, ok := flight.Val.(liveReadResult)
		if !ok {
			return domain.ReadResponse{}, readError(domain.ErrFetchFailed, "正文读取失败", true, fmt.Errorf("singleflight returned %T", flight.Val), nil)
		}
		cached := result.transport == domain.ReadTransportFreshCache
		return s.formatResponse(ctx, normalized, result.document, result.transport, result.extractor, result.fallbackCount, cached, false, result.warnings, result.attempts, started)
	}
}

func (s *ReadService) readLive(ctx context.Context, target domain.SafeTarget, policyAttempt domain.ReadAttempt) (liveReadResult, error) {
	document, resource, extractorName, attempts, err := s.readAndExtract(ctx, s.config.HTTPReader, target, []domain.ReadAttempt{policyAttempt})
	if err != nil {
		if s.config.BrowserReader == nil || !browserFallbackEligible(err) {
			return liveReadResult{}, err
		}
		return s.readRendered(ctx, target, attemptsFromReadError(err))
	}
	qualityStarted := s.config.Now()
	qualityResult := s.config.Evaluator.Evaluate(document, resource)
	attempts = append(attempts, domain.ReadAttempt{
		Stage: domain.ReadStageQuality, Implementation: s.config.Evaluator.Name(),
		Classification: qualityResult.Classification, RequestURL: resource.URL, FinalURL: resource.FinalURL,
		ElapsedMS: s.config.Now().Sub(qualityStarted).Milliseconds(),
	})
	switch qualityResult.Action {
	case domain.QualityActionAccept:
		return liveReadResult{document: document, transport: resource.Transport, extractor: extractorName, attempts: attempts}, nil
	case domain.QualityActionRender:
		if s.config.BrowserReader == nil {
			err = fmt.Errorf("browser fallback is disabled: %s", qualityResult.Reason)
			return liveReadResult{}, readError(domain.ErrExtractionFailed, "网页正文需要浏览器渲染，但浏览器 fallback 未启用", false, err, attempts)
		}
		return s.readRendered(ctx, target, attempts)
	default:
		err = fmt.Errorf("content rejected: %s", qualityResult.Reason)
		return liveReadResult{}, qualityRejectionError(qualityResult, "未提取到有效网页正文", err, attempts)
	}
}

func (s *ReadService) readRendered(ctx context.Context, target domain.SafeTarget, attempts []domain.ReadAttempt) (liveReadResult, error) {
	document, resource, extractorName, attempts, err := s.readAndExtract(ctx, s.config.BrowserReader, target, attempts)
	if err != nil {
		return liveReadResult{}, err
	}
	qualityStarted := s.config.Now()
	qualityResult := s.config.Evaluator.Evaluate(document, resource)
	attempts = append(attempts, domain.ReadAttempt{
		Stage: domain.ReadStageQuality, Implementation: s.config.Evaluator.Name(),
		Classification: qualityResult.Classification, RequestURL: resource.URL, FinalURL: resource.FinalURL,
		ElapsedMS: s.config.Now().Sub(qualityStarted).Milliseconds(),
	})
	if qualityResult.Action != domain.QualityActionAccept {
		err = fmt.Errorf("rendered content rejected: %s", qualityResult.Reason)
		return liveReadResult{}, qualityRejectionError(qualityResult, "浏览器渲染后仍未提取到有效正文", err, attempts)
	}
	return liveReadResult{document: document, transport: resource.Transport, extractor: extractorName, fallbackCount: 1, attempts: attempts}, nil
}

func browserFallbackEligible(err error) bool {
	switch readErrorCode(err) {
	case domain.ErrCaptchaRequired, domain.ErrExtractionFailed:
		return true
	case domain.ErrFetchFailed:
		var statusError interface{ HTTPStatusCode() int }
		if !errors.As(err, &statusError) {
			return false
		}
		switch statusError.HTTPStatusCode() {
		case http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable:
			return true
		}
	default:
	}
	return false
}

func (s *ReadService) readAndExtract(ctx context.Context, reader readpipe.ResourceReader, target domain.SafeTarget, attempts []domain.ReadAttempt) (domain.ReadDocument, domain.Resource, domain.ImplementationName, []domain.ReadAttempt, error) {
	readStarted := s.config.Now()
	resource, err := reader.Read(ctx, target)
	readAttempt := domain.ReadAttempt{
		Stage: domain.ReadStageRead, Implementation: reader.Name(), Classification: domain.ReadClassificationSuccess,
		RequestURL: target.URL.String(), FinalURL: resource.FinalURL, HTTPStatus: resource.StatusCode,
		ContentType: resource.ContentType, ElapsedMS: s.config.Now().Sub(readStarted).Milliseconds(),
	}
	if err != nil {
		code := readErrorCode(err)
		message, retryable, classification := readErrorDescription(code)
		readAttempt.Classification = classification
		readAttempt.OriginalError = err.Error()
		attempts = append(attempts, readAttempt)
		var typed *domain.ReadError
		if errors.As(err, &typed) {
			typed.Attempts = append(attempts, typed.Attempts...)
			return domain.ReadDocument{}, domain.Resource{}, "", attempts, typed
		}
		return domain.ReadDocument{}, domain.Resource{}, "", attempts, readError(code, message, retryable, err, attempts)
	}
	attempts = append(attempts, readAttempt)

	detectStarted := s.config.Now()
	sourceType, err := s.config.Detector.Detect(resource)
	detectAttempt := domain.ReadAttempt{
		Stage: domain.ReadStageDetect, Implementation: s.config.Detector.Name(),
		Classification: domain.ReadClassificationSuccess, RequestURL: resource.URL, FinalURL: resource.FinalURL,
		ContentType: resource.ContentType, ElapsedMS: s.config.Now().Sub(detectStarted).Milliseconds(),
	}
	if err != nil {
		detectAttempt.Classification = domain.ReadClassificationUnsupported
		detectAttempt.OriginalError = err.Error()
		attempts = append(attempts, detectAttempt)
		return domain.ReadDocument{}, resource, "", attempts, readError(domain.ErrUnsupportedContentType, "暂不支持该资源格式", false, err, attempts)
	}
	attempts = append(attempts, detectAttempt)

	extractor, err := s.config.Extractors.Resolve(sourceType)
	if err != nil {
		return domain.ReadDocument{}, resource, "", attempts, readError(domain.ErrUnsupportedContentType, "没有可用的正文提取器", false, err, attempts)
	}
	extractStarted := s.config.Now()
	document, err := extractor.Extract(ctx, resource)
	extractorName := extractor.Name()
	if document.Extractor != "" {
		extractorName = document.Extractor
	}
	extractAttempt := domain.ReadAttempt{
		Stage: domain.ReadStageExtract, Implementation: extractorName, Classification: domain.ReadClassificationSuccess,
		RequestURL: resource.URL, FinalURL: resource.FinalURL, ElapsedMS: s.config.Now().Sub(extractStarted).Milliseconds(),
	}
	if err != nil {
		code := readErrorCode(err)
		extractAttempt.Classification = domain.ReadClassificationRejected
		if code == domain.ErrCaptchaRequired {
			extractAttempt.Classification = domain.ReadClassificationCaptcha
		}
		extractAttempt.OriginalError = err.Error()
		attempts = append(attempts, extractAttempt)
		if code == domain.ErrCaptchaRequired {
			return domain.ReadDocument{}, resource, extractorName, attempts, readError(code, "目标网页要求安全验证", true, err, attempts)
		}
		return domain.ReadDocument{}, resource, extractorName, attempts, readError(domain.ErrExtractionFailed, "网页正文提取失败", false, err, attempts)
	}
	attempts = append(attempts, extractAttempt)
	return document, resource, extractorName, attempts, nil
}

func (s *ReadService) formatResponse(ctx context.Context, request domain.ReadRequest, document domain.ReadDocument, transport domain.ReadTransport, extractor domain.ImplementationName, fallbackCount int, cached, degraded bool, warnings []domain.ReadWarning, attempts []domain.ReadAttempt, started time.Time) (domain.ReadResponse, error) {
	attempts = append([]domain.ReadAttempt(nil), attempts...)
	converter, err := s.config.Converters.Resolve(request.Format)
	if err != nil {
		return domain.ReadResponse{}, readError(domain.ErrInvalidRequest, "正文输出格式不可用", false, err, attempts)
	}
	convertStarted := s.config.Now()
	formatted, err := converter.Convert(ctx, document)
	convertAttempt := domain.ReadAttempt{
		Stage: domain.ReadStageConvert, Implementation: converter.Name(), Classification: domain.ReadClassificationSuccess,
		RequestURL: document.URL, FinalURL: document.FinalURL, ElapsedMS: s.config.Now().Sub(convertStarted).Milliseconds(),
	}
	if err != nil {
		convertAttempt.Classification = domain.ReadClassificationRejected
		convertAttempt.OriginalError = err.Error()
		attempts = append(attempts, convertAttempt)
		return domain.ReadResponse{}, readError(domain.ErrExtractionFailed, "正文格式转换失败", false, err, attempts)
	}
	attempts = append(attempts, convertAttempt)
	content, truncated := readconverter.TruncateContent(formatted.Content, request.MaxChars)
	if formatted.Format == "" {
		formatted.Format = request.Format
	}
	response := domain.ReadResponse{
		URL: request.URL, FinalURL: document.FinalURL, Title: document.Title, Author: document.Author,
		PublishedAt: document.PublishedAt, Language: document.Language, SourceType: document.SourceType,
		Content: content, ContentFormat: formatted.Format, ContentLength: utf8.RuneCountInString(content), Truncated: truncated,
		Meta: domain.ReadMeta{
			Transport: transport, Extractor: extractor, Cached: cached, Degraded: degraded,
			FallbackCount: fallbackCount, TookMS: s.config.Now().Sub(started).Milliseconds(), RequestID: request.RequestID,
		},
		Warnings: append(append([]domain.ReadWarning(nil), document.Warnings...), warnings...),
	}
	if degraded && !document.StoredAt.IsZero() {
		response.Meta.CacheAgeSeconds = int64(s.config.Now().Sub(document.StoredAt).Seconds())
	}
	if response.Warnings == nil {
		response.Warnings = make([]domain.ReadWarning, 0)
	}
	if request.Debug {
		response.Debug = &domain.ReadDebug{Attempts: append([]domain.ReadAttempt(nil), attempts...)}
	}
	return response, nil
}

func readError(code domain.ErrorCode, message string, retryable bool, original error, attempts []domain.ReadAttempt) *domain.ReadError {
	return &domain.ReadError{Code: code, Message: message, Retryable: retryable, Original: original, Attempts: append([]domain.ReadAttempt(nil), attempts...)}
}

func qualityRejectionError(result domain.QualityResult, fallbackMessage string, original error, attempts []domain.ReadAttempt) *domain.ReadError {
	if result.Classification == domain.ReadClassificationCaptcha {
		return readError(domain.ErrCaptchaRequired, "目标网页要求安全验证", true, original, attempts)
	}
	return readError(domain.ErrExtractionFailed, fallbackMessage, false, original, attempts)
}

type readErrorCoder interface {
	ReadErrorCode() domain.ErrorCode
}

func readErrorCode(err error) domain.ErrorCode {
	var typed *domain.ReadError
	if errors.As(err, &typed) && typed.Code != "" {
		return typed.Code
	}
	var coder readErrorCoder
	if errors.As(err, &coder) && coder.ReadErrorCode() != "" {
		return coder.ReadErrorCode()
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return domain.ErrFetchTimeout
	}
	return domain.ErrFetchFailed
}

func readErrorDescription(code domain.ErrorCode) (string, bool, domain.ReadClassification) {
	switch code {
	case domain.ErrUnsafeURL:
		return "目标 URL 或重定向地址不允许访问", false, domain.ReadClassificationUnsafe
	case domain.ErrUnsupportedContentType:
		return "暂不支持该资源格式", false, domain.ReadClassificationUnsupported
	case domain.ErrContentTooLarge:
		return "网页资源超过大小限制", false, domain.ReadClassificationRejected
	case domain.ErrFetchTimeout:
		return "网页资源获取超时", true, domain.ReadClassificationTimeout
	default:
		return "网页资源获取失败", true, domain.ReadClassificationFetchFailed
	}
}

func attemptsFromReadError(err error) []domain.ReadAttempt {
	var readErr *domain.ReadError
	if errors.As(err, &readErr) {
		return append([]domain.ReadAttempt(nil), readErr.Attempts...)
	}
	return []domain.ReadAttempt{{Stage: domain.ReadStageRead, Classification: domain.ReadClassificationFetchFailed, OriginalError: err.Error()}}
}

func cachedExtractor(document domain.ReadDocument) domain.ImplementationName {
	if document.Extractor != "" {
		return document.Extractor
	}
	if document.SourceType == domain.SourceTypePlainText {
		return domain.ImplementationNamePlainTextExtractor
	}
	return domain.ImplementationNameHTMLExtractor
}
