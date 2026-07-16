package transport

import (
	"context"
	"net/http"
	"time"

	"web-search-backend/websearch/internal/domain"
)

type Response struct {
	RequestURL string
	// HeaderProfile records the non-sensitive profile name selected for this request.
	HeaderProfile     string
	StatusCode        int
	FinalURL          string
	PageTitle         string
	Classification    domain.Classification
	Headers           http.Header
	Body              []byte
	Screenshot        []byte
	Elapsed           time.Duration
	SessionState      domain.BaiduSessionState
	SessionGeneration uint64
	SessionWait       time.Duration
	BlockedUntil      time.Time
}

type SearchTransport interface {
	Name() domain.TransportName
	Fetch(context.Context, domain.SearchRequest) (Response, error)
}
