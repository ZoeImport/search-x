package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"web-search-backend/internal/domain"
)

type fakeSearcher struct {
	response domain.SearchResponse
	err      error
	request  domain.SearchRequest
}

func (f *fakeSearcher) Search(_ context.Context, request domain.SearchRequest) (domain.SearchResponse, error) {
	f.request = request
	return f.response, f.err
}

func testRouter(t *testing.T, searcher Searcher, debug bool) *gin.Engine {
	t.Helper()
	return testRouterWithOptions(t, searcher, Options{Debug: debug, TotalTimeout: time.Second, ClientRate: 100, ClientBurst: 100})
}

func testRouterWithOptions(t *testing.T, searcher Searcher, options Options) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router, err := NewRouter(searcher, options)
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func TestSearchRefreshAndAuthorizedDebug(t *testing.T) {
	searcher := &fakeSearcher{response: domain.SearchResponse{
		Debug: &domain.Debug{Attempts: []domain.Attempt{{Transport: "desktop_http"}}},
	}}
	router := testRouterWithOptions(t, searcher, Options{
		Debug: false, DebugToken: "token", TotalTimeout: time.Second, ClientRate: 100, ClientBurst: 100,
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/search?q=golang&refresh=true&debug=true", nil)
	request.Header.Set("X-Debug-Token", "token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, request)
	if w.Code != http.StatusOK || !searcher.request.Refresh || !searcher.request.Debug || !strings.Contains(w.Body.String(), "desktop_http") {
		t.Fatalf("status=%d request=%#v body=%s", w.Code, searcher.request, w.Body.String())
	}
}

func TestSearchDebugRequiresToken(t *testing.T) {
	for _, test := range []struct {
		name        string
		serverToken string
		headerToken string
	}{
		{name: "missing header", serverToken: "token"},
		{name: "wrong header", serverToken: "token", headerToken: "wrong"},
		{name: "server token not configured", headerToken: "token"},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := testRouterWithOptions(t, &fakeSearcher{}, Options{
				Debug: false, DebugToken: test.serverToken, TotalTimeout: time.Second, ClientRate: 100, ClientBurst: 100,
			})
			request := httptest.NewRequest(http.MethodGet, "/v1/search?q=golang&debug=true", nil)
			request.Header.Set("X-Debug-Token", test.headerToken)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, request)
			if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "debug_unauthorized") || strings.Contains(w.Body.String(), "original_error") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestSearchDebugFalseOverridesDevelopmentDefault(t *testing.T) {
	searcher := &fakeSearcher{response: domain.SearchResponse{
		Debug: &domain.Debug{Attempts: []domain.Attempt{{OriginalError: "secret"}}},
	}}
	router := testRouterWithOptions(t, searcher, Options{
		Debug: true, DebugToken: "token", TotalTimeout: time.Second, ClientRate: 100, ClientBurst: 100,
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/search?q=golang&debug=false", nil))
	if w.Code != http.StatusOK || searcher.request.Debug || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "debug") {
		t.Fatalf("status=%d request=%#v body=%s", w.Code, searcher.request, w.Body.String())
	}
}

func TestSearchRejectsInvalidBooleanParameter(t *testing.T) {
	router := testRouter(t, &fakeSearcher{}, false)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/search?q=golang&refresh=not-a-bool", nil))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_request") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestSearchValidation(t *testing.T) {
	router := testRouter(t, &fakeSearcher{}, true)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/search", nil))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_request") || !strings.Contains(w.Body.String(), "original_error") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestSearchBindsDefaultsAndReturnsSuccess(t *testing.T) {
	searcher := &fakeSearcher{response: domain.SearchResponse{
		Provider: "baidu",
		Results:  []domain.SearchResult{{Title: "Go", URL: "https://go.dev/", Rank: 1}},
		Warnings: []domain.Warning{},
	}}
	router := testRouter(t, searcher, true)
	w := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/search?q=golang", nil)
	request.Header.Set("X-Request-ID", "req_client_1")
	router.ServeHTTP(w, request)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if searcher.request.Provider != domain.ProviderNameAuto || searcher.request.Limit != 10 || searcher.request.Page != 1 || searcher.request.RequestID != "req_client_1" {
		t.Fatalf("request=%#v", searcher.request)
	}
	if w.Header().Get("X-Request-ID") != "req_client_1" {
		t.Fatalf("response request ID=%q", w.Header().Get("X-Request-ID"))
	}
}

func TestSearchErrorReturnsRequestedProvider(t *testing.T) {
	upstream := &domain.SearchError{
		Code: domain.ErrProviderUnavailable, Message: "unavailable", Retryable: true, Original: errors.New("upstream failed"),
	}
	router := testRouter(t, &fakeSearcher{err: upstream}, false)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/search?q=golang&provider=duckduckgo", nil))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"provider":"duckduckgo"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSearchReturnsOriginalErrorAndAttempts(t *testing.T) {
	upstream := &domain.SearchError{
		Code:      domain.ErrCaptchaRequired,
		Message:   "百度返回安全验证页面",
		Retryable: true,
		Original:  errors.New("status=200 final_url=https://wappass.baidu.com"),
		Attempts: []domain.Attempt{{
			Transport: "desktop_http", Classification: "captcha", OriginalError: "captcha selector detected",
		}},
		Artifacts: []string{"var/debug/req_1/desktop_http.html"},
	}
	router := testRouter(t, &fakeSearcher{err: upstream}, true)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/search?q=golang", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "status=200") || !strings.Contains(w.Body.String(), "desktop_http") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestProductionResponseHidesRawError(t *testing.T) {
	upstream := &domain.SearchError{
		Code: domain.ErrCaptchaRequired, Message: "captcha", Retryable: true,
		Original: errors.New("secret upstream detail"), Attempts: []domain.Attempt{{OriginalError: "secret attempt"}},
	}
	router := testRouter(t, &fakeSearcher{err: upstream}, false)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/search?q=golang", nil))
	if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "debug") || strings.Contains(w.Body.String(), "original_error") {
		t.Fatalf("body=%s", w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
}
