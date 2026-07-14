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

type fakeReader struct {
	response domain.ReadResponse
	err      error
	request  domain.ReadRequest
}

func (r *fakeReader) Read(_ context.Context, request domain.ReadRequest) (domain.ReadResponse, error) {
	r.request = request
	return r.response, r.err
}

func TestReadHandlerBindsRequestAndReturnsContent(t *testing.T) {
	reader := &fakeReader{response: domain.ReadResponse{
		URL: "https://example.com/article", FinalURL: "https://example.com/article",
		SourceType: domain.SourceTypeHTML, Content: "# Article", ContentFormat: domain.OutputFormatMarkdown,
	}}
	router := testRouterWithOptions(t, &fakeSearcher{}, Options{
		Reader: reader, Debug: false, TotalTimeout: time.Second, ClientRate: 100, ClientBurst: 100,
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/read", strings.NewReader(`{
		"url":"https://example.com/article","format":"markdown","max_chars":12000,"refresh":true,"debug":false
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "req_read_1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"content":"# Article"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if reader.request.URL != "https://example.com/article" || reader.request.Format != domain.OutputFormatMarkdown ||
		reader.request.MaxChars != 12000 || !reader.request.Refresh || reader.request.Debug || reader.request.RequestID != "req_read_1" {
		t.Fatalf("request=%#v", reader.request)
	}
}

func TestReadHandlerAuthorizedDebugForcesRefresh(t *testing.T) {
	reader := &fakeReader{response: domain.ReadResponse{Debug: &domain.ReadDebug{
		Attempts: []domain.ReadAttempt{{OriginalError: "raw read error"}},
	}}}
	router := testRouterWithOptions(t, &fakeSearcher{}, Options{
		Reader: reader, DebugToken: "token", TotalTimeout: time.Second, ClientRate: 100, ClientBurst: 100,
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/read", strings.NewReader(`{"url":"https://example.com","debug":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Debug-Token", "token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !reader.request.Debug || !reader.request.Refresh || !strings.Contains(response.Body.String(), "raw read error") {
		t.Fatalf("status=%d request=%#v body=%s", response.Code, reader.request, response.Body.String())
	}
}

func TestReadHandlerRejectsUnauthorizedDebug(t *testing.T) {
	router := testRouterWithOptions(t, &fakeSearcher{}, Options{
		Reader: &fakeReader{}, DebugToken: "token", TotalTimeout: time.Second, ClientRate: 100, ClientBurst: 100,
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/read", strings.NewReader(`{"url":"https://example.com","debug":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "debug_unauthorized") || strings.Contains(response.Body.String(), "original_error") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestReadHandlerMapsTypedErrorAndPreservesOriginalInDebug(t *testing.T) {
	reader := &fakeReader{err: &domain.ReadError{
		Code: domain.ErrUnsupportedContentType, Message: "暂不支持该资源格式", Retryable: false,
		Original: errors.New("content-type application/pdf"),
		Attempts: []domain.ReadAttempt{{Stage: domain.ReadStageDetect, OriginalError: "detected application/pdf"}},
	}}
	router := testRouterWithOptions(t, &fakeSearcher{}, Options{
		Reader: reader, Debug: true, DebugToken: "token", TotalTimeout: time.Second, ClientRate: 100, ClientBurst: 100,
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/read", strings.NewReader(`{"url":"https://example.com/file.pdf","debug":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Debug-Token", "token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType || !strings.Contains(response.Body.String(), "application/pdf") || !strings.Contains(response.Body.String(), "detected application/pdf") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestReadServerDebugDoesNotBypassToken(t *testing.T) {
	reader := &fakeReader{err: &domain.ReadError{
		Code: domain.ErrFetchFailed, Message: "读取失败", Retryable: true,
		Original: errors.New("secret dial address"), Attempts: []domain.ReadAttempt{{OriginalError: "secret attempt"}},
	}}
	router := testRouterWithOptions(t, &fakeSearcher{}, Options{
		Reader: reader, Debug: true, DebugToken: "token", TotalTimeout: time.Second, ClientRate: 100, ClientBurst: 100,
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/read", strings.NewReader(`{"url":"https://example.com"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "secret") || strings.Contains(response.Body.String(), "original_error") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
