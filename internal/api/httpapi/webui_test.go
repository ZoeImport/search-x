package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouterServesEmbeddedWebUI(t *testing.T) {
	router := testRouter(t, &fakeSearcher{}, false)
	root := httptest.NewRecorder()
	router.ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	if root.Code != http.StatusTemporaryRedirect || root.Header().Get("Location") != "/ui/" {
		t.Fatalf("root status=%d location=%q", root.Code, root.Header().Get("Location"))
	}
	page := httptest.NewRecorder()
	router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/ui/", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Searchroom") {
		t.Fatalf("ui status=%d body=%s", page.Code, page.Body.String())
	}
}

func TestWebUIContainsCombinedSearchControls(t *testing.T) {
	router := testRouter(t, &fakeSearcher{}, false)
	page := httptest.NewRecorder()
	router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/ui/", nil))
	for _, marker := range []string{"contentEnabledInput", "candidateLimitInput", "brave"} {
		if !strings.Contains(page.Body.String(), marker) {
			t.Fatalf("missing UI marker %q", marker)
		}
	}
	script := httptest.NewRecorder()
	router.ServeHTTP(script, httptest.NewRequest(http.MethodGet, "/ui/app.js", nil))
	for _, marker := range []string{"original_rank", "selected_rank", `method: "POST"`} {
		if !strings.Contains(script.Body.String(), marker) {
			t.Fatalf("missing UI script marker %q", marker)
		}
	}
	stylesheet := httptest.NewRecorder()
	router.ServeHTTP(stylesheet, httptest.NewRequest(http.MethodGet, "/ui/app.css", nil))
	if !strings.Contains(stylesheet.Body.String(), ".notice { overflow-wrap: anywhere;") {
		t.Fatal("notice text must wrap long failure URLs")
	}
}
