package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"web-search-backend/websearch/internal/cursor"
	"web-search-backend/websearch/internal/domain"
)

type fakeSearcher struct {
	request  domain.SearchRequest
	warnings []domain.Warning
}

func (fake *fakeSearcher) Search(_ context.Context, request domain.SearchRequest) (domain.SearchResponse, error) {
	fake.request = request
	return domain.SearchResponse{Query: request.Query, Provider: domain.ProviderNameBing, Results: []domain.SearchResult{{Title: "Go", URL: "https://go.dev", Snippet: "Go language", Rank: 1, Provider: domain.ProviderNameBing}}, Meta: domain.Meta{TookMS: 4}, Warnings: fake.warnings}, nil
}

func TestPOSTSearchContractAndProviderVisibility(t *testing.T) {
	searcher := &fakeSearcher{warnings: []domain.Warning{{Code: domain.WarningCodeProviderFallback, Message: "provider baidu failed; using bing"}}}
	codec, _ := cursor.New("secret", time.Minute, time.Now)
	router, err := New(Options{Searcher: searcher, Cursor: codec, Ready: func() bool { return true }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), AllowRequestProviders: true, EnabledProviders: []string{"bing"}, ProviderVisibility: "hidden"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/websearch", strings.NewReader(`{"query":"go","limit":1,"timeout":"1500ms","routing":{"providers":["bing"]}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	results := body["results"].([]any)
	if _, ok := results[0].(map[string]any)["provider"]; ok {
		t.Fatal("provider must be hidden")
	}
	if _, ok := body["meta"].(map[string]any)["provider"]; ok { t.Fatal("meta provider must be hidden") }
	warnings := body["warnings"].([]any)
	if strings.Contains(strings.ToLower(warnings[0].(map[string]any)["message"].(string)), "bing") { t.Fatalf("warning leaked provider: %v", warnings) }
	if len(searcher.request.Providers) != 1 || searcher.request.Providers[0] != domain.ProviderNameBing {
		t.Fatalf("providers=%v", searcher.request.Providers)
	}
	if body["usage"].(map[string]any)["units"] != float64(1) {
		t.Fatalf("usage=%v", body["usage"])
	}
}

func TestPOSTSearchRejectsDuplicateProvidersAndInvalidTimeout(t *testing.T) {
	searcher := &fakeSearcher{}
	codec, _ := cursor.New("secret", time.Minute, time.Now)
	router, _ := New(Options{Searcher: searcher, Cursor: codec, Ready: func() bool { return true }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), AllowRequestProviders: true, EnabledProviders: []string{"baidu", "bing"}})
	for _, body := range []string{`{"query":"go","routing":{"providers":["bing","bing"]}}`, `{"query":"go","timeout":"1.5s"}`} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/websearch", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, response.Code, response.Body.String())
		}
	}
}

func TestCursorBindsOrderedProviderChainAndPinsProvider(t *testing.T) {
	searcher := &fakeSearcher{}
	codec, _ := cursor.New("secret", time.Minute, time.Now)
	router, _ := New(Options{Searcher: searcher, Cursor: codec, Ready: func() bool { return true }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), AllowRequestProviders: true, EnabledProviders: []string{"baidu", "bing"}, ProviderVisibility: "public"})
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/v1/websearch", strings.NewReader(`{"query":"go","limit":1,"routing":{"providers":["baidu","bing"]}}`)))
	if first.Code != http.StatusOK {
		t.Fatalf("first=%d %s", first.Code, first.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	token := body["page"].(map[string]any)["next_cursor"].(string)
	mismatch := httptest.NewRecorder()
	router.ServeHTTP(mismatch, httptest.NewRequest(http.MethodPost, "/v1/websearch", strings.NewReader(`{"query":"go","limit":1,"cursor":"`+token+`","routing":{"providers":["bing","baidu"]}}`)))
	if mismatch.Code != http.StatusBadRequest {
		t.Fatalf("mismatch=%d %s", mismatch.Code, mismatch.Body.String())
	}
	pinned := httptest.NewRecorder()
	router.ServeHTTP(pinned, httptest.NewRequest(http.MethodPost, "/v1/websearch", strings.NewReader(`{"query":"go","limit":1,"cursor":"`+token+`"}`)))
	if pinned.Code != http.StatusOK || searcher.request.Provider != domain.ProviderNameBing {
		t.Fatalf("pinned=%d request=%+v body=%s", pinned.Code, searcher.request, pinned.Body.String())
	}
}

func TestPOSTSearchRejectsUnknownField(t *testing.T) {
	searcher := &fakeSearcher{}
	codec, _ := cursor.New("secret", time.Minute, time.Now)
	router, _ := New(Options{Searcher: searcher, Cursor: codec, Ready: func() bool { return true }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), EnabledProviders: []string{"bing"}})
	request := httptest.NewRequest(http.MethodPost, "/v1/websearch", strings.NewReader(`{"query":"go","unknown":true}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestPOSTSearchRejectsTrailingJSON(t *testing.T) {
	searcher := &fakeSearcher{}
	codec, _ := cursor.New("secret", time.Minute, time.Now)
	router, _ := New(Options{Searcher: searcher, Cursor: codec, Ready: func() bool { return true }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), EnabledProviders: []string{"bing"}})
	request := httptest.NewRequest(http.MethodPost, "/v1/websearch", strings.NewReader(`{"query":"go"}{"query":"rust"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestSearchCORSPreflightForAllowedDemoOrigin(t *testing.T) {
	searcher := &fakeSearcher{}
	codec, _ := cursor.New("secret", time.Minute, time.Now)
	router, _ := New(Options{Searcher: searcher, Cursor: codec, Ready: func() bool { return true }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), EnabledProviders: []string{"bing"}, AllowedOrigins: []string{"http://127.0.0.1:8090"}})
	request := httptest.NewRequest(http.MethodOptions, "/v1/websearch", nil)
	request.Header.Set("Origin", "http://127.0.0.1:8090")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:8090" {
		t.Fatalf("status=%d allow-origin=%q", response.Code, response.Header().Get("Access-Control-Allow-Origin"))
	}
}
