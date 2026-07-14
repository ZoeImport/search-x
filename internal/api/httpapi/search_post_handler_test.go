package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"web-search-backend/internal/domain"
)

type contentSearcherStub struct {
	response domain.SearchContentResponse
	err      error
	request  domain.SearchContentRequest
	calls    int
}

func (stub *contentSearcherStub) Search(_ context.Context, request domain.SearchContentRequest) (domain.SearchContentResponse, error) {
	stub.calls++
	stub.request = request
	return stub.response, stub.err
}

func TestPostSearchWithoutContentUsesLightSearch(t *testing.T) {
	light := &fakeSearcher{response: domain.SearchResponse{Provider: domain.ProviderNameDuckDuckGo, Results: make([]domain.SearchResult, 0)}}
	combined := &contentSearcherStub{}
	router := testRouterWithOptions(t, light, Options{
		ContentSearcher: combined, TotalTimeout: time.Second, ClientRate: 100, ClientBurst: 100,
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/search", strings.NewReader(`{"query":"go","provider":"duckduckgo","limit":5}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || combined.calls != 0 {
		t.Fatalf("status=%d combined=%d body=%s", response.Code, combined.calls, response.Body.String())
	}
	if light.request.Query != "go" || light.request.Provider != domain.ProviderNameDuckDuckGo || light.request.Limit != 5 || light.request.Page != 1 {
		t.Fatalf("request=%+v", light.request)
	}
}

func TestPostSearchWithContentUsesCombinedSearch(t *testing.T) {
	light := &fakeSearcher{}
	combined := &contentSearcherStub{response: domain.SearchContentResponse{
		Results: make([]domain.SearchContentResult, 0), Failures: make([]domain.SearchContentFailure, 0), Warnings: make([]domain.Warning, 0),
	}}
	router := testRouterWithOptions(t, light, Options{
		ContentSearcher: combined, DebugToken: "token", TotalTimeout: time.Second, ClientRate: 100, ClientBurst: 100,
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/search", strings.NewReader(`{
		"query":"go","limit":5,"refresh":false,"debug":true,
		"content":{"enabled":true,"candidate_limit":8,"format":"markdown","max_chars":12000}
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Debug-Token", "token")
	request.Header.Set("X-Request-ID", "req_post_1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || combined.calls != 1 {
		t.Fatalf("status=%d combined=%d body=%s", response.Code, combined.calls, response.Body.String())
	}
	if combined.request.RequestID != "req_post_1" || !combined.request.Debug || !combined.request.Refresh || combined.request.Content.CandidateLimit != 8 {
		t.Fatalf("request=%+v", combined.request)
	}
	if light.request.Query != "" {
		t.Fatalf("light search was called: %+v", light.request)
	}
}

func TestPostSearchDebugRequiresToken(t *testing.T) {
	router := testRouterWithOptions(t, &fakeSearcher{}, Options{
		ContentSearcher: &contentSearcherStub{}, DebugToken: "token", TotalTimeout: time.Second, ClientRate: 100, ClientBurst: 100,
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/search", strings.NewReader(`{"query":"go","debug":true,"content":{"enabled":true}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "debug_unauthorized") || strings.Contains(response.Body.String(), "original_error") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPostSearchRejectsUnknownJSONField(t *testing.T) {
	router := testRouterWithOptions(t, &fakeSearcher{}, Options{
		ContentSearcher: &contentSearcherStub{}, TotalTimeout: time.Second, ClientRate: 100, ClientBurst: 100,
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/search", strings.NewReader(`{"query":"go","unexpected":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_request") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPostSearchDebugErrorIncludesRawReadAttempts(t *testing.T) {
	combined := &contentSearcherStub{err: &domain.SearchError{
		Code: domain.ErrInsufficientReadableResults, Message: "no content", Retryable: true,
		Original:     errors.New("all candidate reads failed"),
		ReadAttempts: []domain.ReadAttempt{{Stage: domain.ReadStageRead, OriginalError: "upstream HTTP 521"}},
	}}
	router := testRouterWithOptions(t, &fakeSearcher{}, Options{
		ContentSearcher: combined, DebugToken: "token", TotalTimeout: time.Second, ClientRate: 100, ClientBurst: 100,
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/search", strings.NewReader(`{"query":"go","debug":true,"content":{"enabled":true}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Debug-Token", "token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "all candidate reads failed") || !strings.Contains(response.Body.String(), "upstream HTTP 521") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
