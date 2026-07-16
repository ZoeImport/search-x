package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"web-search-backend/runtime/httpx"
	"web-search-backend/runtime/requesttimeout"
	"web-search-backend/webfetch/internal/domain"
)

type Reader interface {
	Read(context.Context, domain.ReadRequest) (domain.ReadResponse, error)
}

type Options struct {
	Reader         Reader
	Ready          func() bool
	Logger         *slog.Logger
	Timeout        time.Duration
	MaxTimeout     time.Duration
	CacheBypass    bool
	LogURLQuery    bool
	AllowedOrigins []string
}

type readPayload struct {
	URL     string `json:"url" binding:"required,max=2048"`
	Timeout string `json:"timeout,omitempty"`
	Output  struct {
		Format   domain.OutputFormat `json:"format"`
		MaxChars int                 `json:"max_chars"`
	} `json:"output"`
}

type readEnvelope struct {
	RequestID string               `json:"request_id"`
	Document  readDocument         `json:"document"`
	Meta      readMeta             `json:"meta"`
	Warnings  []domain.ReadWarning `json:"warnings"`
	Usage     usage                `json:"usage"`
}

type usage struct {
	Units int `json:"units"`
}

type readDocument struct {
	URL         string              `json:"url"`
	FinalURL    string              `json:"final_url"`
	Title       string              `json:"title,omitempty"`
	Author      string              `json:"author,omitempty"`
	PublishedAt string              `json:"published_at,omitempty"`
	Language    string              `json:"language,omitempty"`
	SourceType  domain.SourceType   `json:"source_type"`
	ContentType string              `json:"content_type"`
	StatusCode  int                 `json:"status_code"`
	Content     string              `json:"content"`
	Format      domain.OutputFormat `json:"format"`
	RetrievedAt time.Time           `json:"retrieved_at"`
}

type readMeta struct {
	Cached          bool                 `json:"cached"`
	Transport       domain.ReadTransport `json:"transport"`
	Truncated       bool                 `json:"truncated"`
	ContentLength   int                  `json:"content_length"`
	CacheAgeSeconds int64                `json:"cache_age_seconds,omitempty"`
	TookMS          int64                `json:"took_ms"`
}

func New(options Options) (*gin.Engine, error) {
	if options.Reader == nil || options.Logger == nil || options.Ready == nil {
		return nil, errors.New("reader, logger, and readiness check are required")
	}
	if options.Timeout <= 0 {
		options.Timeout = 20 * time.Second
	}
	if options.MaxTimeout <= 0 {
		options.MaxTimeout = 60 * time.Second
	}
	router := gin.New()
	router.Use(gin.Recovery(), requestID(), cors(options.AllowedOrigins))
	router.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	router.GET("/readyz", func(c *gin.Context) {
		if !options.Ready() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	router.POST("/v1/webfetch", func(c *gin.Context) {
		var payload readPayload
		decoder := newStrictDecoder(c.Request.Body)
		if err := decoder.Decode(&payload); err != nil {
			writeProblem(c, http.StatusBadRequest, "invalid_request", "Invalid request", "The request body is invalid.", false, "")
			return
		}
		requestTimeout, timeoutErr := requesttimeout.Parse(payload.Timeout, options.Timeout, options.MaxTimeout)
		if timeoutErr != nil {
			writeProblem(c, http.StatusBadRequest, "invalid_request", "Invalid timeout", timeoutErr.Error(), false, "timeout")
			return
		}
		request := domain.ReadRequest{URL: payload.URL, Format: payload.Output.Format, MaxChars: payload.Output.MaxChars, Refresh: options.CacheBypass, RequestID: requestIDValue(c)}
		ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
		defer cancel()
		response, err := options.Reader.Read(ctx, request)
		if err != nil {
			options.Logger.Warn("read failed", "request_id", request.RequestID, "url", redactURL(payload.URL, options.LogURLQuery), "error", err)
			writeReadProblem(c, err)
			return
		}
		options.Logger.Info("read completed", "request_id", request.RequestID, "url", redactURL(payload.URL, options.LogURLQuery), "transport", response.Meta.Transport, "cached", response.Meta.Cached, "took_ms", response.Meta.TookMS)
		c.JSON(http.StatusOK, readEnvelope{
			RequestID: request.RequestID,
			Document:  readDocument{URL: response.URL, FinalURL: response.FinalURL, Title: response.Title, Author: response.Author, PublishedAt: response.PublishedAt, Language: response.Language, SourceType: response.SourceType, ContentType: response.ContentType, StatusCode: response.StatusCode, Content: response.Content, Format: response.ContentFormat, RetrievedAt: time.Now().UTC()},
			Meta:      readMeta{Cached: response.Meta.Cached, Transport: response.Meta.Transport, Truncated: response.Truncated, ContentLength: response.ContentLength, CacheAgeSeconds: response.Meta.CacheAgeSeconds, TookMS: response.Meta.TookMS},
			Warnings:  response.Warnings,
			Usage:     usage{Units: 1},
		})
	})
	return router, nil
}

func writeReadProblem(c *gin.Context, err error) {
	var readErr *domain.ReadError
	if !errors.As(err, &readErr) {
		writeProblem(c, http.StatusBadGateway, "upstream_failed", "WebFetch failed", "The target could not be fetched.", true, "")
		return
	}
	status, code, retryable := http.StatusBadGateway, string(readErr.Code), readErr.Retryable
	switch readErr.Code {
	case domain.ErrInvalidRequest:
		status = http.StatusBadRequest
	case domain.ErrUnsafeURL:
		status = http.StatusForbidden
	case domain.ErrUnsupportedContentType:
		status = http.StatusUnsupportedMediaType
	case domain.ErrContentTooLarge:
		status = http.StatusRequestEntityTooLarge
	case domain.ErrExtractionFailed:
		status = http.StatusUnprocessableEntity
	case domain.ErrFetchTimeout:
		status, code = http.StatusGatewayTimeout, "deadline_exceeded"
	}
	if code == "captcha_required" {
		status, retryable = http.StatusUnprocessableEntity, false
	}
	writeProblem(c, status, code, "WebFetch failed", readErr.Message, retryable, "")
}

func writeProblem(c *gin.Context, status int, code, title, detail string, retryable bool, parameter string) {
	c.Header("Content-Type", "application/problem+json")
	c.AbortWithStatusJSON(status, httpx.Problem{Type: "https://api.example.com/problems/" + strings.ReplaceAll(code, "_", "-"), Title: title, Status: status, Code: code, Detail: detail, RequestID: requestIDValue(c), Retryable: retryable, Parameter: parameter})
}
