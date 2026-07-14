package bootstrap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"web-search-backend/internal/config"
	"web-search-backend/internal/domain"
)

func TestNewBuildsEndToEndOfflineSearchApp(t *testing.T) {
	fixture, err := os.ReadFile("../../testdata/baidu/desktop_normal.html")
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(fixture)
	}))
	defer upstream.Close()

	temp := t.TempDir()
	cfg := config.Config{
		Debug: true, DebugDir: filepath.Join(temp, "debug"), DebugPreviewBytes: 32 * 1024,
		ChromeProfileDir: filepath.Join(temp, "profile"), ChromeHeadless: true,
		DesktopURL: upstream.URL, MobileURL: upstream.URL, UserAgent: "test-agent",
		TotalTimeout: 2 * time.Second, DesktopTimeout: time.Second, MobileTimeout: time.Second, ChromeTimeout: time.Second,
		FreshTTL: time.Minute, StaleTTL: time.Hour, ProviderRate: 1000, ProviderBurst: 100,
		ClientRate: 1000, ClientBurst: 100, CacheMaxItems: 10, MaxBodyBytes: 1 << 20,
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
	if response.Provider != "baidu" || response.Meta.Transport != "desktop_http" || len(response.Results) != 2 {
		t.Fatalf("response=%#v", response)
	}

	w = httptest.NewRecorder()
	app.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/search?q=golang&provider=other", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
