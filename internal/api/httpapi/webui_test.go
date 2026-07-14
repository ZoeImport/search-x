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
