package baidu

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"web-search-backend/websearch/internal/domain"
	"web-search-backend/websearch/internal/transport"
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

func newTestProvider(t *testing.T, transports []transport.SearchTransport, artifacts ArtifactStore) *Provider {
	t.Helper()
	steps := make([]StrategyStep, 0, len(transports))
	for _, current := range transports {
		steps = append(steps, StrategyStep{Name: domain.BaiduStrategyNameHeaderPool, Transport: current})
	}
	chain, err := NewStrategyChain(ConservativeStrategyFallback{}, steps...)
	if err != nil {
		t.Fatal(err)
	}
	providerValue, err := NewProvider(chain, artifacts, NewBreaker(time.Now))
	if err != nil {
		t.Fatal(err)
	}
	return providerValue
}

func TestProviderFallsBackAndPreservesAttempts(t *testing.T) {
	desktopCalls, mobileCalls := 0, 0
	transports := []transport.SearchTransport{
		fakeTransport{name: "desktop_http", calls: &desktopCalls, err: errors.New("dial tcp: temporary failure")},
		fakeTransport{name: "mobile_http", calls: &mobileCalls, response: transport.Response{StatusCode: 200, FinalURL: "https://m.baidu.com/s", Headers: http.Header{}, Body: fixture(t, "mobile_normal.html"), HeaderProfile: "chrome_desktop_secondary"}},
	}
	artifacts := &fakeArtifacts{}
	p := newTestProvider(t, transports, artifacts)
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
	if got.Debug.Attempts[0].Classification != "network_error" {
		t.Fatalf("attempt=%#v", got.Debug.Attempts[0])
	}
	if got.Debug.Attempts[1].HeaderProfile != "chrome_desktop_secondary" {
		t.Fatalf("header_profile=%q", got.Debug.Attempts[1].HeaderProfile)
	}
}

func TestProviderFallsBackFromFixedSessionToHeaderPoolOnNetworkError(t *testing.T) {
	fixedCalls, pooledCalls := 0, 0
	chain, err := NewStrategyChain(ConservativeStrategyFallback{},
		StrategyStep{
			Name: domain.BaiduStrategyNameFixedSession,
			Transport: fakeTransport{name: domain.TransportNameBaiduSessionHTTP, calls: &fixedCalls,
				err: errors.New("fixed session connection reset")},
		},
		StrategyStep{
			Name: domain.BaiduStrategyNameHeaderPool,
			Transport: fakeTransport{name: domain.TransportNameDesktopHTTP, calls: &pooledCalls,
				response: transport.Response{StatusCode: 200, FinalURL: "https://www.baidu.com/s", Body: fixture(t, "desktop_normal.html")}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	providerValue, err := NewProvider(chain, &fakeArtifacts{}, NewBreaker(time.Now))
	if err != nil {
		t.Fatal(err)
	}
	response, err := providerValue.Search(context.Background(), domain.SearchRequest{Query: "go", Limit: 5, Page: 1, Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if fixedCalls != 1 || pooledCalls != 1 || response.Meta.Strategy != domain.BaiduStrategyNameHeaderPool {
		t.Fatalf("fixed=%d pooled=%d response=%+v", fixedCalls, pooledCalls, response)
	}
	if len(response.Debug.Attempts) != 2 || response.Debug.Attempts[0].Strategy != domain.BaiduStrategyNameFixedSession ||
		response.Debug.Attempts[1].Strategy != domain.BaiduStrategyNameHeaderPool {
		t.Fatalf("attempts=%+v", response.Debug.Attempts)
	}
}

func TestProviderStopsBaiduStrategyChainOnCaptcha(t *testing.T) {
	fixedCalls, pooledCalls := 0, 0
	chain, err := NewStrategyChain(ConservativeStrategyFallback{},
		StrategyStep{
			Name: domain.BaiduStrategyNameFixedSession,
			Transport: fakeTransport{name: domain.TransportNameBaiduSessionHTTP, calls: &fixedCalls,
				response: transport.Response{StatusCode: 200, FinalURL: "https://wappass.baidu.com/", Body: fixture(t, "captcha.html")}},
		},
		StrategyStep{
			Name: domain.BaiduStrategyNameHeaderPool,
			Transport: fakeTransport{name: domain.TransportNameDesktopHTTP, calls: &pooledCalls,
				response: transport.Response{StatusCode: 200, FinalURL: "https://www.baidu.com/s", Body: fixture(t, "desktop_normal.html")}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	providerValue, err := NewProvider(chain, &fakeArtifacts{}, NewBreaker(time.Now))
	if err != nil {
		t.Fatal(err)
	}
	_, searchErr := providerValue.Search(context.Background(), domain.SearchRequest{Query: "go", Limit: 5, Page: 1})
	var typed *domain.SearchError
	if !errors.As(searchErr, &typed) || typed.Code != domain.ErrCaptchaRequired {
		t.Fatalf("error=%v", searchErr)
	}
	if fixedCalls != 1 || pooledCalls != 0 {
		t.Fatalf("fixed=%d pooled=%d", fixedCalls, pooledCalls)
	}
}

func TestProviderReturnsClassifiedErrorWithAllAttempts(t *testing.T) {
	body := fixture(t, "captcha.html")
	transports := []transport.SearchTransport{
		fakeTransport{name: "desktop_http", response: transport.Response{StatusCode: 200, FinalURL: "https://wappass.baidu.com/", Body: body}},
		fakeTransport{name: "mobile_http", response: transport.Response{StatusCode: 200, FinalURL: "https://wappass.baidu.com/", Body: body}},
	}
	p := newTestProvider(t, transports, &fakeArtifacts{})
	_, err := p.Search(context.Background(), domain.SearchRequest{Query: "golang", Provider: "baidu", RequestID: "req_2", Limit: 10, Page: 1})
	var searchErr *domain.SearchError
	if !errors.As(err, &searchErr) {
		t.Fatalf("error=%T %v", err, err)
	}
	if searchErr.Code != domain.ErrCaptchaRequired || len(searchErr.Attempts) != 1 {
		t.Fatalf("searchErr=%#v", searchErr)
	}
	if !strings.Contains(searchErr.Error(), "status=200") || !strings.Contains(searchErr.Error(), "wappass.baidu.com") {
		t.Fatalf("original=%q", searchErr.Error())
	}
}

func TestProviderPreservesNetworkError(t *testing.T) {
	original := errors.New("dial tcp: connection refused")
	p := newTestProvider(t, []transport.SearchTransport{fakeTransport{name: "desktop_http", err: original}}, &fakeArtifacts{})
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
	plainProvider := newTestProvider(t,
		[]transport.SearchTransport{fakeTransport{name: "desktop_http", response: response}},
		plainArtifacts,
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
	debugProvider := newTestProvider(t,
		[]transport.SearchTransport{fakeTransport{name: "desktop_http", response: response}},
		debugArtifacts,
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
