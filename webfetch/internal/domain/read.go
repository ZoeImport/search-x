package domain

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// SourceType identifies the source representation of a fetched resource.
type SourceType string

const (
	// SourceTypeHTML identifies an HTML document.
	SourceTypeHTML SourceType = "html"
	// SourceTypePlainText identifies a plain-text document.
	SourceTypePlainText SourceType = "text"
)

// OutputFormat identifies a supported API content representation.
type OutputFormat string

const (
	// OutputFormatMarkdown requests Markdown content.
	OutputFormatMarkdown OutputFormat = "markdown"
	// OutputFormatText requests plain-text content.
	OutputFormatText OutputFormat = "text"
)

// ReadStage identifies one boundary in the read pipeline.
type ReadStage string

const (
	// ReadStagePolicy identifies URL policy validation.
	ReadStagePolicy ReadStage = "policy"
	// ReadStageCache identifies a cache operation.
	ReadStageCache ReadStage = "cache"
	// ReadStageRead identifies resource acquisition.
	ReadStageRead ReadStage = "read"
	// ReadStageDetect identifies source-type detection.
	ReadStageDetect ReadStage = "detect"
	// ReadStageExtract identifies content extraction.
	ReadStageExtract ReadStage = "extract"
	// ReadStageQuality identifies content quality evaluation.
	ReadStageQuality ReadStage = "quality"
	// ReadStageConvert identifies output conversion.
	ReadStageConvert ReadStage = "convert"
)

// ReadClassification identifies the typed outcome of a read-pipeline operation.
type ReadClassification string

const (
	// ReadClassificationSuccess indicates successful processing.
	ReadClassificationSuccess ReadClassification = "success"
	// ReadClassificationEmpty indicates no usable content.
	ReadClassificationEmpty ReadClassification = "empty"
	// ReadClassificationUnsafe indicates a target rejected by URL policy.
	ReadClassificationUnsafe ReadClassification = "unsafe"
	// ReadClassificationUnsupported indicates an unsupported resource type.
	ReadClassificationUnsupported ReadClassification = "unsupported"
	// ReadClassificationTimeout indicates a read deadline was exceeded.
	ReadClassificationTimeout ReadClassification = "timeout"
	// ReadClassificationFetchFailed indicates resource acquisition failed.
	ReadClassificationFetchFailed ReadClassification = "fetch_failed"
	// ReadClassificationCaptcha indicates a verification challenge.
	ReadClassificationCaptcha ReadClassification = "captcha"
	// ReadClassificationLoginRequired indicates content requires authentication.
	ReadClassificationLoginRequired ReadClassification = "login_required"
	// ReadClassificationJSShell indicates browser rendering is required.
	ReadClassificationJSShell ReadClassification = "js_shell"
	// ReadClassificationTooShort indicates extracted HTML content is insufficient.
	ReadClassificationTooShort ReadClassification = "too_short"
	// ReadClassificationRejected indicates content failed quality checks.
	ReadClassificationRejected ReadClassification = "rejected"
)

// QualityAction identifies the next step selected by content quality evaluation.
type QualityAction string

const (
	// QualityActionAccept accepts the extracted document.
	QualityActionAccept QualityAction = "accept"
	// QualityActionRender requests a browser-rendered retry.
	QualityActionRender QualityAction = "render"
	// QualityActionReject rejects the extracted document.
	QualityActionReject QualityAction = "reject"
)

// ImplementationName identifies a concrete read-pipeline implementation.
type ImplementationName string

const (
	// ImplementationNameSafeURLPolicy identifies the public URL policy.
	ImplementationNameSafeURLPolicy ImplementationName = "safe_url_policy"
	// ImplementationNameHTTPReader identifies the bounded HTTP reader.
	ImplementationNameHTTPReader ImplementationName = "http_reader"
	// ImplementationNameChromedpReader identifies the browser reader.
	ImplementationNameChromedpReader ImplementationName = "chromedp_reader"
	// ImplementationNameMIMETypeDetector identifies the MIME detector.
	ImplementationNameMIMETypeDetector ImplementationName = "mime_type_detector"
	// ImplementationNameHTMLExtractor identifies the HTML extraction strategy chain.
	ImplementationNameHTMLExtractor ImplementationName = "html_extractor_chain"
	// ImplementationNameReadabilityExtractor identifies the primary Readability strategy.
	ImplementationNameReadabilityExtractor ImplementationName = "readability"
	// ImplementationNameDOMArticleExtractor identifies the fallback article-DOM strategy.
	ImplementationNameDOMArticleExtractor ImplementationName = "article_dom"
	// ImplementationNamePlainTextExtractor identifies the plain-text extractor.
	ImplementationNamePlainTextExtractor ImplementationName = "plain_text_extractor"
	// ImplementationNameArticleQualityEvaluator identifies the article quality evaluator.
	ImplementationNameArticleQualityEvaluator ImplementationName = "article_quality_evaluator"
	// ImplementationNameMarkdownConverter identifies the Markdown converter.
	ImplementationNameMarkdownConverter ImplementationName = "markdown_converter"
	// ImplementationNameTextConverter identifies the plain-text converter.
	ImplementationNameTextConverter ImplementationName = "text_converter"
	// ImplementationNameMemoryReadCache identifies the in-memory read cache.
	ImplementationNameMemoryReadCache ImplementationName = "memory_read_cache"
)

// ReadTransport identifies the transport or cache source of a read response.
type ReadTransport string

const (
	// ReadTransportHTTP identifies direct HTTP acquisition.
	ReadTransportHTTP ReadTransport = "http"
	// ReadTransportChromedp identifies browser-rendered acquisition.
	ReadTransportChromedp ReadTransport = "chromedp"
	// ReadTransportFreshCache identifies a fresh cache response.
	ReadTransportFreshCache ReadTransport = "fresh_cache"
	// ReadTransportStaleCache identifies a stale cache response.
	ReadTransportStaleCache ReadTransport = "stale_cache"
)

// ReadWarningCode identifies a stable non-fatal read warning.
type ReadWarningCode string

const (
	// ReadWarningInvalidUTF8 indicates invalid UTF-8 was replaced safely.
	ReadWarningInvalidUTF8 ReadWarningCode = "invalid_utf8_replaced"
	// ReadWarningCacheWriteError indicates a successful document was not cached.
	ReadWarningCacheWriteError ReadWarningCode = "cache_write_error"
	// ReadWarningLiveReadUnavailable indicates stale content replaced a failed live read.
	ReadWarningLiveReadUnavailable ReadWarningCode = "live_read_unavailable"
	// ReadWarningUnsupportedCharset indicates text used a safe fallback for an unknown charset.
	ReadWarningUnsupportedCharset ReadWarningCode = "unsupported_charset"
)

const (
	// DefaultReadMaxChars is the default response content limit in Unicode characters.
	DefaultReadMaxChars = 30000
	// MinReadMaxChars is the smallest accepted response content limit.
	MinReadMaxChars = 1000
	// MaxReadMaxChars is the largest accepted response content limit.
	MaxReadMaxChars = 100000
	// MaxReadURLLength is the largest accepted input URL length.
	MaxReadURLLength = 2048
)

const (
	// ErrUnsafeURL indicates a target blocked by the public URL policy.
	ErrUnsafeURL ErrorCode = "unsafe_url"
	// ErrUnsupportedContentType indicates a resource format unsupported in phase one.
	ErrUnsupportedContentType ErrorCode = "unsupported_content_type"
	// ErrContentTooLarge indicates a resource exceeded its hard body limit.
	ErrContentTooLarge ErrorCode = "content_too_large"
	// ErrFetchTimeout indicates the resource read deadline was exceeded.
	ErrFetchTimeout ErrorCode = "fetch_timeout"
	// ErrFetchFailed indicates a transport-level resource read failure.
	ErrFetchFailed ErrorCode = "fetch_failed"
	// ErrExtractionFailed indicates no usable content could be extracted.
	ErrExtractionFailed ErrorCode = "extraction_failed"
)

// ReadRequest contains a normalized single-URL read request.
type ReadRequest struct {
	// URL is the caller-provided public HTTP or HTTPS URL.
	URL string `json:"url"`
	// Format selects Markdown or plain-text output.
	Format OutputFormat `json:"format"`
	// MaxChars bounds returned content by Unicode character count.
	MaxChars int `json:"max_chars"`
	// Refresh skips a fresh cache lookup.
	Refresh bool `json:"refresh"`
	// Debug enables authorized pipeline diagnostics and forces refresh.
	Debug bool `json:"debug"`
	// RequestID correlates read logs and responses.
	RequestID string `json:"-"`
}

// Normalize applies defaults and validates request-level constraints.
func (request ReadRequest) Normalize() (ReadRequest, error) {
	request.URL = strings.TrimSpace(request.URL)
	if request.URL == "" {
		return ReadRequest{}, fmt.Errorf("url is required")
	}
	if len(request.URL) > MaxReadURLLength {
		return ReadRequest{}, fmt.Errorf("url exceeds %d bytes", MaxReadURLLength)
	}
	if request.Format == "" {
		request.Format = OutputFormatMarkdown
	}
	if request.Format != OutputFormatMarkdown && request.Format != OutputFormatText {
		return ReadRequest{}, fmt.Errorf("unsupported format %q", request.Format)
	}
	if request.MaxChars == 0 {
		request.MaxChars = DefaultReadMaxChars
	}
	if request.MaxChars < MinReadMaxChars || request.MaxChars > MaxReadMaxChars {
		return ReadRequest{}, fmt.Errorf("max_chars must be between %d and %d", MinReadMaxChars, MaxReadMaxChars)
	}
	if request.Debug {
		request.Refresh = true
	}
	return request, nil
}

// SafeTarget is a URL and its policy-approved, pinned network addresses.
type SafeTarget struct {
	// URL is the canonical validated URL with its original hostname preserved.
	URL *url.URL
	// Addresses contains every validated, pinned DNS answer.
	Addresses []net.IP
}

// Resource is fetched input before content extraction.
type Resource struct {
	// URL is the initial requested URL.
	URL string
	// FinalURL is the validated URL after redirects.
	FinalURL string
	// StatusCode is the final upstream HTTP status.
	StatusCode int
	// ContentType is the normalized MIME media type.
	ContentType string
	// Charset is the normalized declared character set.
	Charset string
	// Headers contains response metadata for internal diagnostics only.
	Headers map[string][]string
	// Body contains bounded, decompressed source bytes.
	Body []byte
	// Transport identifies how the resource was acquired.
	Transport ReadTransport
}

// ReadDocument is format-independent canonical extracted content.
type ReadDocument struct {
	// URL is the initial requested URL.
	URL string
	// FinalURL is the validated URL after redirects.
	FinalURL string
	// Title is the extracted article title.
	Title string
	// Author is the extracted author name.
	Author string
	// PublishedAt is the source-provided publication time representation.
	PublishedAt string
	// Language is the source-provided language tag.
	Language string
	// SourceType identifies the extracted source representation.
	SourceType SourceType
	// ContentType is the normalized upstream MIME media type.
	ContentType string
	// StatusCode is the final upstream HTTP status.
	StatusCode int
	// Extractor records the strategy that produced this canonical document.
	Extractor ImplementationName
	// ContentHTML contains sanitized canonical article HTML.
	ContentHTML string
	// ContentText contains normalized canonical plain text.
	ContentText string
	// Warnings contains stable extraction warnings safe to cache and return.
	Warnings []ReadWarning
	// StoredAt records when the canonical document entered the cache.
	StoredAt time.Time
}

// QualityResult is a typed extraction quality decision.
type QualityResult struct {
	// Action selects accept, render, or reject.
	Action QualityAction
	// Classification describes the decision in stable machine-readable form.
	Classification ReadClassification
	// Reason provides a developer-facing explanation.
	Reason string
}

// FormattedContent is canonical content converted to one output representation.
type FormattedContent struct {
	// Content is the converted response body.
	Content string
	// Format identifies the representation of Content.
	Format OutputFormat
}

// ReadWarning is a stable non-fatal read warning.
type ReadWarning struct {
	// Code is the stable warning identifier.
	Code ReadWarningCode `json:"code"`
	// Message provides a human-readable warning detail.
	Message string `json:"message"`
}

// ReadAttempt records one observable read-pipeline implementation call.
type ReadAttempt struct {
	// Stage identifies the observed pipeline boundary.
	Stage ReadStage `json:"stage"`
	// Implementation identifies the concrete component invoked.
	Implementation ImplementationName `json:"implementation"`
	// Classification records the typed outcome.
	Classification ReadClassification `json:"classification"`
	// RequestURL is the target entering the stage.
	RequestURL string `json:"request_url,omitempty"`
	// FinalURL is the validated redirect result when known.
	FinalURL string `json:"final_url,omitempty"`
	// HTTPStatus is the upstream status when applicable.
	HTTPStatus int `json:"http_status,omitempty"`
	// ContentType is the normalized source MIME type when known.
	ContentType string `json:"content_type,omitempty"`
	// ElapsedMS records stage duration in milliseconds.
	ElapsedMS int64 `json:"elapsed_ms"`
	// OriginalError contains low-level details only exposed in authorized debug.
	OriginalError string `json:"original_error,omitempty"`
}

// ReadMeta describes how a read response was produced.
type ReadMeta struct {
	// Transport identifies the live or cache source.
	Transport ReadTransport `json:"transport"`
	// Extractor identifies the canonical content extractor.
	Extractor ImplementationName `json:"extractor"`
	// Cached reports whether content came from fresh or stale cache.
	Cached bool `json:"cached"`
	// Degraded reports whether stale content replaced a failed live read.
	Degraded bool `json:"degraded"`
	// FallbackCount records HTTP-to-browser fallback transitions.
	FallbackCount int `json:"fallback_count"`
	// TookMS records total request duration in milliseconds.
	TookMS int64 `json:"took_ms"`
	// RequestID correlates logs and API responses.
	RequestID string `json:"request_id"`
	// CacheAgeSeconds records stale content age when degraded.
	CacheAgeSeconds int64 `json:"cache_age_seconds,omitempty"`
}

// ReadDebug contains authorized read-pipeline diagnostics.
type ReadDebug struct {
	// Attempts contains ordered pipeline diagnostics.
	Attempts []ReadAttempt `json:"attempts"`
	// RawArtifacts contains authorized local debug artifact paths.
	RawArtifacts []string `json:"raw_artifacts,omitempty"`
}

// ReadResponse is the successful read API payload.
type ReadResponse struct {
	// URL is the caller-provided URL.
	URL string `json:"url"`
	// FinalURL is the final validated URL after redirects.
	FinalURL string `json:"final_url"`
	// Title is the extracted article title.
	Title string `json:"title,omitempty"`
	// Author is the extracted author name.
	Author string `json:"author,omitempty"`
	// PublishedAt is the extracted publication time representation.
	PublishedAt string `json:"published_at,omitempty"`
	// Language is the extracted language tag.
	Language string `json:"language,omitempty"`
	// SourceType identifies HTML or plain text.
	SourceType  SourceType `json:"source_type"`
	ContentType string     `json:"content_type"`
	StatusCode  int        `json:"status_code"`
	// Content is the converted and bounded body.
	Content string `json:"content"`
	// ContentFormat identifies the representation of Content.
	ContentFormat OutputFormat `json:"content_format"`
	// ContentLength is the Unicode character count of Content.
	ContentLength int `json:"content_length"`
	// Truncated reports whether MaxChars shortened Content.
	Truncated bool `json:"truncated"`
	// Meta describes how the response was produced.
	Meta ReadMeta `json:"meta"`
	// Warnings contains stable non-fatal diagnostics.
	Warnings []ReadWarning `json:"warnings"`
	// Debug contains authorized pipeline details.
	Debug *ReadDebug `json:"debug,omitempty"`
}

// ReadError is a stable read-pipeline failure with optional debug details.
type ReadError struct {
	// Code is the stable API error code.
	Code ErrorCode
	// Message is the stable caller-facing error message.
	Message string
	// Retryable reports whether retrying may succeed.
	Retryable bool
	// Original retains the lowest-level error for authorized debugging.
	Original error
	// Attempts contains ordered pipeline diagnostics.
	Attempts []ReadAttempt
	// Artifacts contains authorized local debug artifact paths.
	Artifacts []string
}

// Error returns the lowest-level error when available, otherwise the stable message.
func (err *ReadError) Error() string {
	if err == nil {
		return ""
	}
	if err.Original != nil {
		return err.Original.Error()
	}
	return err.Message
}

// Unwrap returns the underlying read error.
func (err *ReadError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Original
}
