package transport

import (
	"net/http"
	"time"
)

type Response struct {
	RequestURL string
	// HeaderProfile records the non-sensitive profile name selected for this request.
	HeaderProfile string
	StatusCode    int
	FinalURL      string
	PageTitle     string
	Headers       http.Header
	Body          []byte
	Screenshot    []byte
	Elapsed       time.Duration
}
