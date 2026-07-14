package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"web-search-backend/internal/config"
	"web-search-backend/internal/domain"
	"web-search-backend/internal/profilepool"
	"web-search-backend/internal/searchtrace"
)

func TestNewBuildsEndToEndOfflineSearchApp(t *testing.T) {
	baiduFixture, err := os.ReadFile("../../testdata/baidu/desktop_normal.html")
	if err != nil {
		t.Fatal(err)
	}
	duckFixture, err := os.ReadFile("../../testdata/duckduckgo/normal.html")
	if err != nil {
		t.Fatal(err)
	}
	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if request.URL.Path == "/article" {
			_, _ = w.Write([]byte("<html lang='zh-CN'><title>Read Article</title><article><h1>Read Article</h1><p>" + strings.Repeat("正文内容用于验证安全读取与 Markdown 转换。", 20) + "</p></article></html>"))
			return
		}
		if request.URL.Query().Get("q") == "combined" {
			_, _ = w.Write([]byte(`<html><body><div id="links" class="results"><div class="result"><h2 class="result__title"><a class="result__a" href="` + upstream.URL + `/article">Combined Article</a></h2><div class="result__snippet">Local readable article.</div></div></div></body></html>`))
			return
		}
		if request.URL.Query().Get("q") != "" {
			_, _ = w.Write(duckFixture)
			return
		}
		_, _ = w.Write(baiduFixture)
	}))
	defer upstream.Close()

	temp := t.TempDir()
	cfg := config.Config{
		Debug: true, DebugDir: filepath.Join(temp, "debug"), DebugPreviewBytes: 32 * 1024,
		ProfileManifestRoot:   filepath.Join(temp, "provider-manifests"),
		CapacityRouterEnabled: true, ProfilePoolEnabled: true,
		ChromeProfileDir: filepath.Join(temp, "profile"), BingProfileDir: filepath.Join(temp, "bing-profile"), BraveProfileDir: filepath.Join(temp, "brave-profile"), ChromeHeadless: true,
		DesktopURL: upstream.URL, MobileURL: upstream.URL,
		UserAgent:     "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.7871.115 Safari/537.36",
		DuckDuckGoURL: upstream.URL, BingURL: "https://www.bing.com/search", BraveURL: "https://search.brave.com/search",
		TotalTimeout: 5 * time.Second, DesktopTimeout: time.Second, MobileTimeout: time.Second, ChromeTimeout: time.Second,
		DuckDuckGoTimeout: time.Second, BingTimeout: time.Second, BraveTimeout: time.Second, ProviderBrowserSlots: 2,
		FreshTTL: time.Minute, StaleTTL: time.Hour, ProviderRate: 1000, ProviderBurst: 100,
		BaiduSessionMinInterval: time.Nanosecond, BaiduSessionMaxJitter: 0,
		BaiduCaptchaCooldown: time.Minute, BaiduRateLimitCooldown: time.Minute, BaiduFallbackReserve: 0,
		ClientRate: 1000, ClientBurst: 100, CacheMaxItems: 10, MaxBodyBytes: 1 << 20,
		ReadEnabled: true, ReadHTTPTimeout: time.Second,
		ReadFreshTTL: time.Minute, ReadStaleTTL: time.Hour, ReadCacheMaxItems: 10,
		ReadMaxBodyBytes: 1 << 20, ReadMaxRedirects: 2, ReadBrowserSlots: 3,
		ReadHostAllowlist: []string{"127.0.0.1"},
	}
	app, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/search?q=golang", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var response domain.SearchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Provider != "baidu" || response.Meta.Transport != domain.TransportNameBaiduSessionHTTP || len(response.Results) != 2 {
		t.Fatalf("response=%#v", response)
	}
	if response.Meta.RequestedProvider != domain.ProviderNameAuto {
		t.Fatalf("requested_provider=%s", response.Meta.RequestedProvider)
	}

	w = httptest.NewRecorder()
	app.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/search?q=golang&provider=duckduckgo", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Provider != domain.ProviderNameDuckDuckGo || response.Meta.RequestedProvider != domain.ProviderNameDuckDuckGo {
		t.Fatalf("response=%#v", response)
	}

	w = httptest.NewRecorder()
	app.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/search?q=golang&provider=other", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	readRequest := httptest.NewRequest(http.MethodPost, "/v1/read", strings.NewReader(`{"url":"`+upstream.URL+`/article","format":"markdown","max_chars":30000,"debug":false}`))
	readRequest.Header.Set("Content-Type", "application/json")
	app.Router.ServeHTTP(w, readRequest)
	if w.Code != http.StatusOK {
		t.Fatalf("read status=%d body=%s", w.Code, w.Body.String())
	}
	var readResponse domain.ReadResponse
	if err := json.Unmarshal(w.Body.Bytes(), &readResponse); err != nil {
		t.Fatal(err)
	}
	if readResponse.Title != "Read Article" || readResponse.Meta.Transport != domain.ReadTransportHTTP || !strings.Contains(readResponse.Content, "正文内容") {
		t.Fatalf("read response=%#v", readResponse)
	}

	w = httptest.NewRecorder()
	combinedRequest := httptest.NewRequest(http.MethodPost, "/v1/search", strings.NewReader(`{
		"query":"combined","provider":"duckduckgo","limit":1,
		"content":{"enabled":true,"candidate_limit":1,"format":"markdown","max_chars":30000}
	}`))
	combinedRequest.Header.Set("Content-Type", "application/json")
	app.Router.ServeHTTP(w, combinedRequest)
	if w.Code != http.StatusOK {
		t.Fatalf("combined status=%d body=%s", w.Code, w.Body.String())
	}
	var combinedResponse domain.SearchContentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &combinedResponse); err != nil {
		t.Fatal(err)
	}
	if combinedResponse.SelectedProvider != domain.ProviderNameDuckDuckGo || combinedResponse.ReadableCount != 1 ||
		len(combinedResponse.Results) != 1 || !strings.Contains(combinedResponse.Results[0].Content, "正文内容") {
		t.Fatalf("combined response=%#v", combinedResponse)
	}
}

func TestBrowserPoolFactoryUsesDistinctProfileDirectories(t *testing.T) {
	root := t.TempDir()
	var directories []string
	pool, err := buildBrowserPool(domain.ProviderNameBing, 5, 2, filepath.Join(root, "profiles"), filepath.Join(root, "manifests"), 100, 100, func(profileDir string) (profilepool.Searcher, func() error, error) {
		directories = append(directories, profileDir)
		return &fakeProfileSearcher{provider: domain.ProviderNameBing}, func() error { return nil }, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	seen := map[string]bool{}
	for _, directory := range directories {
		seen[directory] = true
	}
	if len(directories) != 5 || len(seen) != 5 {
		t.Fatalf("directories=%v", directories)
	}
	for index := 1; index <= 5; index++ {
		expected := filepath.Join(root, "profiles", fmt.Sprintf("bing-%04d", index))
		if !seen[expected] {
			t.Fatalf("missing %s", expected)
		}
	}
}

type fakeProfileSearcher struct{ provider domain.ProviderName }

func (searcher *fakeProfileSearcher) Name() domain.ProviderName { return searcher.provider }
func (searcher *fakeProfileSearcher) Search(context.Context, domain.SearchRequest) (domain.SearchResponse, error) {
	return domain.SearchResponse{Provider: searcher.provider}, nil
}

func TestStrictTraceFailureMakesRuntimeNotReady(t *testing.T) {
	root := t.TempDir()
	spool, err := searchtrace.OpenSpool(filepath.Join(root, "spool"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := searchtrace.OpenStore(filepath.Join(root, "trace.db"))
	if err != nil {
		t.Fatal(err)
	}
	tracer, err := searchtrace.NewManager(spool, store, searchtrace.ManagerConfig{FailureMode: searchtrace.FailureModeStrict})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := profilepool.NewProviderProfile("ready", 1, &fakeProfileSearcher{provider: domain.ProviderNameBaidu}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := profilepool.New([]profilepool.Profile{profile}, profilepool.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runtimeValue := &searchRuntime{pools: []*profilepool.Pool{pool}, tracer: tracer}
	if !runtimeValue.Ready() {
		t.Fatal("runtime should initially be ready")
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Append(context.Background(), searchtrace.Event{TraceID: "failed", RequestID: "failed", Type: "request_started"}); err == nil {
		t.Fatal("expected strict append error")
	}
	if runtimeValue.Ready() {
		t.Fatal("strict trace failure must fail readiness")
	}
	_ = tracer.Close()
}
