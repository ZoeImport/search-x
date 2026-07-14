package transport

import (
	"context"
	"net/http"
	"time"

	"web-search-backend/internal/domain"
)

type Response struct {
	RequestURL string
	StatusCode int
	FinalURL   string
	Headers    http.Header
	Body       []byte
	Screenshot []byte
	Elapsed    time.Duration
}

type SearchTransport interface {
	Name() domain.TransportName
	Fetch(context.Context, domain.SearchRequest) (Response, error)
}
