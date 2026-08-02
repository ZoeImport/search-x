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

	"web-search-backend/webfetch/internal/domain"
)

type fakeReader struct{ request domain.ReadRequest }

func (fake *fakeReader) Read(_ context.Context, request domain.ReadRequest) (domain.ReadResponse, error) {
	fake.request = request
	return domain.ReadResponse{URL: request.URL, FinalURL: request.URL, Title: "Example", SourceType: domain.SourceTypeHTML, ContentType: "text/html", StatusCode: 200, Content: "# Example", ContentFormat: domain.OutputFormatMarkdown, ContentLength: 9, Meta: domain.ReadMeta{Transport: domain.ReadTransportHTTP, TookMS: 3}, Warnings: []domain.ReadWarning{}}, nil
}

func TestPOSTReadContract(t *testing.T) {
	reader := &fakeReader{}
	router, err := New(Options{Reader: reader, Ready: func() bool { return true }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/webfetch", strings.NewReader(`{"url":"https://example.com","timeout":"1500ms","output":{"format":"markdown","max_chars":30000}}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["request_id"] == "" {
		t.Fatal("request_id missing")
	}
	if reader.request.URL != "https://example.com" {
		t.Fatalf("url=%q", reader.request.URL)
	}
	document := body["document"].(map[string]any)
	if document["content_type"] != "text/html" || document["status_code"] != float64(200) {
		t.Fatalf("document=%v", document)
	}
	if body["usage"].(map[string]any)["units"] != float64(1) {
		t.Fatalf("usage=%v", body["usage"])
	}
}

func TestPOSTReadRejectsNonStrictJSON(t *testing.T) {
	reader := &fakeReader{}
	router, err := New(Options{Reader: reader, Ready: func() bool { return true }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"url":"https://example.com","unknown":true}`,
		`{"url":"https://example.com"}{"url":"https://example.org"}`,
		`{"url":"https://example.com","timeout":"1.5s"}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/v1/webfetch", strings.NewReader(body))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d, want %d", body, response.Code, http.StatusBadRequest)
		}
	}
}

func TestReadCORSPreflightForAllowedDemoOrigin(t *testing.T) {
	reader := &fakeReader{}
	router, err := New(Options{Reader: reader, Ready: func() bool { return true }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), AllowedOrigins: []string{"http://127.0.0.1:8090"}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodOptions, "/v1/webfetch", nil)
	request.Header.Set("Origin", "http://127.0.0.1:8090")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:8090" {
		t.Fatalf("status=%d allow-origin=%q", response.Code, response.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestAPIKeyAuthProtectsBusinessRoutes(t *testing.T) {
	reader := &fakeReader{}
	router, err := New(Options{Reader: reader, Ready: func() bool { return true }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, httptest.NewRequest(http.MethodPost, "/v1/webfetch", strings.NewReader(`{"url":"https://example.com"}`)))
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("no key status=%d body=%s", denied.Code, denied.Body.String())
	}
	allowed := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/webfetch", strings.NewReader(`{"url":"https://example.com"}`))
	request.Header.Set("Authorization", "Bearer test-key")
	router.ServeHTTP(allowed, request)
	if allowed.Code != http.StatusOK {
		t.Fatalf("with key status=%d body=%s", allowed.Code, allowed.Body.String())
	}
}

func TestAPIKeyAuthDisabledWhenEmpty(t *testing.T) {
	reader := &fakeReader{}
	router, err := New(Options{Reader: reader, Ready: func() bool { return true }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/webfetch", strings.NewReader(`{"url":"https://example.com"}`)))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
