package duckduckgo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"web-search-backend/internal/domain"
)

func TestProviderFetchesQueryAndPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("q") != "go language" || request.URL.Query().Get("s") != "10" {
			t.Errorf("query=%s", request.URL.RawQuery)
		}
		if request.Header.Get("User-Agent") == "" || request.Header.Get("Accept") == "" {
			t.Errorf("headers=%v", request.Header)
		}
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = response.Write(fixture(t, "normal.html"))
	}))
	defer server.Close()

	provider, err := New(Config{BaseURL: server.URL, Timeout: time.Second, MaxBodyBytes: 1 << 20}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	got, err := provider.Search(context.Background(), domain.SearchRequest{
		Query: "go language", Provider: domain.ProviderNameDuckDuckGo, Limit: 10, Page: 2, Debug: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != domain.ProviderNameDuckDuckGo || got.Meta.Transport != domain.TransportNameDuckDuckGoHTTP || len(got.Results) != 2 {
		t.Fatalf("response=%+v", got)
	}
	if got.Debug == nil || len(got.Debug.Attempts) != 1 || got.Debug.Attempts[0].HTTPStatus != http.StatusOK {
		t.Fatalf("debug=%+v", got.Debug)
	}
}

func TestProviderClassifiesHTTPFailures(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		expected  domain.ErrorCode
		retryable bool
	}{
		{name: "rate limited", status: http.StatusTooManyRequests, expected: domain.ErrRateLimited, retryable: true},
		{name: "server error", status: http.StatusBadGateway, expected: domain.ErrProviderUnavailable, retryable: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.status)
			}))
			defer server.Close()
			provider, err := New(Config{BaseURL: server.URL, Timeout: time.Second, MaxBodyBytes: 1024}, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, gotErr := provider.Search(context.Background(), domain.SearchRequest{Query: "go", Limit: 10, Page: 1})
			var searchErr *domain.SearchError
			if !errors.As(gotErr, &searchErr) || searchErr.Code != test.expected || searchErr.Retryable != test.retryable {
				t.Fatalf("error=%T %+v", gotErr, gotErr)
			}
			if len(searchErr.Attempts) != 1 || searchErr.Attempts[0].HTTPStatus != test.status {
				t.Fatalf("attempts=%+v", searchErr.Attempts)
			}
		})
	}
}

func TestProviderClassifiesTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = response.Write(fixture(t, "normal.html"))
	}))
	defer server.Close()
	provider, err := New(Config{BaseURL: server.URL, Timeout: 10 * time.Millisecond, MaxBodyBytes: 1 << 20}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, gotErr := provider.Search(context.Background(), domain.SearchRequest{Query: "go", Limit: 10, Page: 1})
	var searchErr *domain.SearchError
	if !errors.As(gotErr, &searchErr) || searchErr.Code != domain.ErrUpstreamTimeout {
		t.Fatalf("error=%T %+v", gotErr, gotErr)
	}
}

func TestProviderRejectsChangedMarkup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("<html><body>changed</body></html>"))
	}))
	defer server.Close()
	provider, err := New(Config{BaseURL: server.URL, Timeout: time.Second, MaxBodyBytes: 1 << 20}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, gotErr := provider.Search(context.Background(), domain.SearchRequest{Query: "go", Limit: 10, Page: 1})
	var searchErr *domain.SearchError
	if !errors.As(gotErr, &searchErr) || searchErr.Code != domain.ErrUpstreamChanged {
		t.Fatalf("error=%T %+v", gotErr, gotErr)
	}
}

func TestProviderRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("<html><body>response larger than configured limit</body></html>"))
	}))
	defer server.Close()
	provider, err := New(Config{BaseURL: server.URL, Timeout: time.Second, MaxBodyBytes: 16}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, gotErr := provider.Search(context.Background(), domain.SearchRequest{Query: "go", Limit: 10, Page: 1})
	var searchErr *domain.SearchError
	if !errors.As(gotErr, &searchErr) || searchErr.Code != domain.ErrProviderUnavailable {
		t.Fatalf("error=%T %+v", gotErr, gotErr)
	}
	if len(searchErr.Attempts) != 1 || searchErr.Attempts[0].OriginalError == "" {
		t.Fatalf("attempts=%+v", searchErr.Attempts)
	}
}
