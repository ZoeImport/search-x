package duckduckgo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"web-search-backend/websearch/internal/domain"
	"web-search-backend/websearch/internal/headerprofile"
)

func TestProviderFetchesQueryWithSelectedHeaderProfile(t *testing.T) {
	headerProfiles, err := headerprofile.NewStaticPool([]headerprofile.Profile{{
		Name: "ddg-profile", UserAgent: "ddg-agent", AcceptLanguage: "en-US,en;q=0.9",
		Headers: map[string]string{"Accept": "text/html", "X-Profile": "selected"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("q") != "go language" || request.URL.Query().Get("s") != "0" {
			t.Errorf("query=%s", request.URL.RawQuery)
		}
		if request.Header.Get("User-Agent") != "ddg-agent" || request.Header.Get("X-Profile") != "selected" {
			t.Errorf("headers=%v", request.Header)
		}
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = response.Write(fixture(t, "normal.html"))
	}))
	defer server.Close()

	provider, err := New(Config{BaseURL: server.URL, Timeout: time.Second, MaxBodyBytes: 1 << 20, HeaderProfiles: headerProfiles}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	got, err := provider.Search(context.Background(), domain.SearchRequest{
		Query: "go language", Provider: domain.ProviderNameDuckDuckGo, Limit: 10, Page: 1, Debug: true,
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
	if got.Debug.Attempts[0].HeaderProfile != "ddg-profile" {
		t.Fatalf("header_profile=%q", got.Debug.Attempts[0].HeaderProfile)
	}
}

func TestProviderRejectsPaginationWithoutOpaqueNextForm(t *testing.T) {
	provider, err := New(Config{BaseURL: "https://html.duckduckgo.com/html/", Timeout: time.Second, MaxBodyBytes: 1 << 20}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, gotErr := provider.Search(context.Background(), domain.SearchRequest{
		Query: "go language", Provider: domain.ProviderNameDuckDuckGo, Limit: 2, Page: 2,
	})
	var searchErr *domain.SearchError
	if !errors.As(gotErr, &searchErr) || searchErr.Code != domain.ErrInvalidRequest {
		t.Fatalf("error=%T %+v", gotErr, gotErr)
	}
}

func TestProviderUsesOpaqueNextFormForRealPagination(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestCount++
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch requestCount {
		case 1:
			if request.Method != http.MethodGet {
				t.Errorf("first method=%s", request.Method)
			}
			_, _ = response.Write([]byte(`
				<html><body><div id="links" class="results">
					<div class="result"><a class="result__a" href="https://go.dev/first">First</a></div>
					<div class="nav-link"><form action="/html/" method="post">
						<input name="q" value="go language" />
						<input name="s" value="10" />
						<input name="dc" value="11" />
						<input name="vqd" value="4-test-token" />
					</form></div>
				</div></body></html>`))
		case 2:
			if request.Method != http.MethodPost {
				t.Errorf("second method=%s", request.Method)
			}
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if request.Form.Get("q") != "go language" || request.Form.Get("s") != "10" || request.Form.Get("vqd") != "4-test-token" {
				t.Errorf("second form=%v", request.Form)
			}
			_, _ = response.Write([]byte(`
				<html><body><div id="links" class="results">
					<div class="result"><a class="result__a" href="https://go.dev/second">Second</a></div>
				</div></body></html>`))
		default:
			t.Fatalf("unexpected request %d", requestCount)
		}
	}))
	defer server.Close()

	provider, err := New(Config{BaseURL: server.URL, Timeout: time.Second, MaxBodyBytes: 1 << 20}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	first, err := provider.Search(context.Background(), domain.SearchRequest{
		Query: "go language", Provider: domain.ProviderNameDuckDuckGo, Limit: 1, Page: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.PaginationKnown || first.NextPageToken == "" || first.Results[0].URL != "https://go.dev/first" {
		t.Fatalf("first=%+v", first)
	}
	second, err := provider.Search(context.Background(), domain.SearchRequest{
		Query: "go language", Provider: domain.ProviderNameDuckDuckGo, Limit: 1, Page: 2,
		ProviderPageToken: first.NextPageToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.PaginationKnown || second.NextPageToken != "" || second.Results[0].URL != "https://go.dev/second" {
		t.Fatalf("second=%+v", second)
	}
}

func TestProviderIgnoresRegion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if _, present := request.URL.Query()["kl"]; present {
			t.Errorf("expected no kl param, query=%s", request.URL.RawQuery)
		}
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = response.Write(fixture(t, "normal.html"))
	}))
	defer server.Close()

	provider, err := New(Config{BaseURL: server.URL, Timeout: time.Second, MaxBodyBytes: 1 << 20}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Search(context.Background(), domain.SearchRequest{
		Query: "go language", Provider: domain.ProviderNameDuckDuckGo, Limit: 10, Page: 1, Region: "jp",
	}); err != nil {
		t.Fatal(err)
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

func TestProviderClassifiesDuckDuckGoHumanChallenge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = response.Write([]byte(`
			<html><body>
				<form id="challenge-form" action="//duckduckgo.com/anomaly.js">
					<div data-testid="anomaly-modal">Unfortunately, bots use DuckDuckGo too.</div>
				</form>
			</body></html>`))
	}))
	defer server.Close()

	provider, err := New(Config{BaseURL: server.URL, Timeout: time.Second, MaxBodyBytes: 1 << 20}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, gotErr := provider.Search(context.Background(), domain.SearchRequest{Query: "go", Limit: 10, Page: 1})
	var searchErr *domain.SearchError
	if !errors.As(gotErr, &searchErr) || searchErr.Code != domain.ErrCaptchaRequired || !searchErr.Retryable {
		t.Fatalf("error=%T %+v", gotErr, gotErr)
	}
	if len(searchErr.Attempts) != 1 || searchErr.Attempts[0].Classification != "captcha" {
		t.Fatalf("attempts=%+v", searchErr.Attempts)
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
