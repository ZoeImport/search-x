package baidu

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/transport"
)

type fakeTransport struct {
	name     domain.TransportName
	response transport.Response
	err      error
	calls    *int
}

func (f fakeTransport) Name() domain.TransportName { return f.name }
func (f fakeTransport) Fetch(context.Context, domain.SearchRequest) (transport.Response, error) {
	if f.calls != nil {
		*f.calls++
	}
	return f.response, f.err
}

type fakeArtifacts struct {
	paths []string
}

func (f *fakeArtifacts) Preview(body []byte) (string, string)          { return string(body), "hash" }
func (f *fakeArtifacts) RedactHeaders(headers http.Header) http.Header { return headers.Clone() }
func (f *fakeArtifacts) SaveHTML(_, name string, _ []byte) (string, error) {
	path := name + ".html"
	f.paths = append(f.paths, path)
	return path, nil
}
func (f *fakeArtifacts) SaveScreenshot(_, name string, _ []byte) (string, error) {
	path := name + ".png"
	f.paths = append(f.paths, path)
	return path, nil
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("../../../testdata/baidu/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestProviderFallsBackAndPreservesAttempts(t *testing.T) {
	desktopCalls, mobileCalls := 0, 0
	transports := []transport.SearchTransport{
		fakeTransport{name: "desktop_http", calls: &desktopCalls, response: transport.Response{StatusCode: 429, FinalURL: "https://www.baidu.com/s", Headers: http.Header{}, Body: []byte("limited")}},
		fakeTransport{name: "mobile_http", calls: &mobileCalls, response: transport.Response{StatusCode: 200, FinalURL: "https://m.baidu.com/s", Headers: http.Header{}, Body: fixture(t, "mobile_normal.html")}},
	}
	artifacts := &fakeArtifacts{}
	p := NewProvider(transports, artifacts, NewBreaker(time.Now), nil)
	got, err := p.Search(context.Background(), domain.SearchRequest{Query: "golang", Provider: "baidu", RequestID: "req_1", Limit: 10, Page: 1, Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if desktopCalls != 1 || mobileCalls != 1 {
		t.Fatalf("calls desktop=%d mobile=%d", desktopCalls, mobileCalls)
	}
	if got.Meta.Transport != "mobile_http" || got.Meta.FallbackCount != 1 || got.Debug == nil || len(got.Debug.Attempts) != 2 {
		t.Fatalf("response=%#v", got)
	}
	if got.Debug.Attempts[0].Classification != "rate_limited" || got.Debug.Attempts[0].HTTPStatus != 429 {
		t.Fatalf("attempt=%#v", got.Debug.Attempts[0])
	}
}

func TestProviderReturnsClassifiedErrorWithAllAttempts(t *testing.T) {
	body := fixture(t, "captcha.html")
	transports := []transport.SearchTransport{
		fakeTransport{name: "desktop_http", response: transport.Response{StatusCode: 200, FinalURL: "https://wappass.baidu.com/", Body: body}},
		fakeTransport{name: "mobile_http", response: transport.Response{StatusCode: 200, FinalURL: "https://wappass.baidu.com/", Body: body}},
	}
	p := NewProvider(transports, &fakeArtifacts{}, NewBreaker(time.Now), nil)
	_, err := p.Search(context.Background(), domain.SearchRequest{Query: "golang", Provider: "baidu", RequestID: "req_2", Limit: 10, Page: 1})
	var searchErr *domain.SearchError
	if !errors.As(err, &searchErr) {
		t.Fatalf("error=%T %v", err, err)
	}
	if searchErr.Code != domain.ErrCaptchaRequired || len(searchErr.Attempts) != 2 {
		t.Fatalf("searchErr=%#v", searchErr)
	}
	if !strings.Contains(searchErr.Error(), "status=200") || !strings.Contains(searchErr.Error(), "wappass.baidu.com") {
		t.Fatalf("original=%q", searchErr.Error())
	}
}

func TestProviderPreservesNetworkError(t *testing.T) {
	original := errors.New("dial tcp: connection refused")
	p := NewProvider([]transport.SearchTransport{fakeTransport{name: "desktop_http", err: original}}, &fakeArtifacts{}, NewBreaker(time.Now), nil)
	_, err := p.Search(context.Background(), domain.SearchRequest{Query: "golang", Provider: "baidu", RequestID: "req_3", Limit: 10, Page: 1})
	var searchErr *domain.SearchError
	if !errors.As(err, &searchErr) || !strings.Contains(searchErr.Error(), original.Error()) || searchErr.Attempts[0].OriginalError != original.Error() {
		t.Fatalf("error=%#v", searchErr)
	}
}

func TestProviderRequestDebugControlsArtifacts(t *testing.T) {
	response := transport.Response{
		StatusCode: 200,
		FinalURL:   "https://www.baidu.com/s",
		Headers:    http.Header{"Set-Cookie": []string{"secret"}},
		Body:       fixture(t, "desktop_normal.html"),
	}
	plainArtifacts := &fakeArtifacts{}
	plainProvider := NewProvider(
		[]transport.SearchTransport{fakeTransport{name: "desktop_http", response: response}},
		plainArtifacts, NewBreaker(time.Now), nil,
	)
	plain, err := plainProvider.Search(context.Background(), domain.SearchRequest{
		Query: "golang", Provider: "baidu", RequestID: "req_plain", Limit: 10, Page: 1, Debug: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Debug != nil || len(plainArtifacts.paths) != 0 {
		t.Fatalf("plain debug=%#v artifacts=%#v", plain.Debug, plainArtifacts.paths)
	}

	debugArtifacts := &fakeArtifacts{}
	debugProvider := NewProvider(
		[]transport.SearchTransport{fakeTransport{name: "desktop_http", response: response}},
		debugArtifacts, NewBreaker(time.Now), nil,
	)
	debugResponse, err := debugProvider.Search(context.Background(), domain.SearchRequest{
		Query: "golang", Provider: "baidu", RequestID: "req_debug", Limit: 10, Page: 1, Debug: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if debugResponse.Debug == nil || len(debugResponse.Debug.Attempts) != 1 || len(debugArtifacts.paths) != 1 {
		t.Fatalf("debug=%#v artifacts=%#v", debugResponse.Debug, debugArtifacts.paths)
	}
}
