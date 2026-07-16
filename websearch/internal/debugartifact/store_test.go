package debugartifact

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactHeaders(t *testing.T) {
	headers := http.Header{
		"Cookie":              {"secret=1"},
		"Set-Cookie":          {"sid=2"},
		"Authorization":       {"Bearer secret"},
		"Proxy-Authorization": {"Basic secret"},
		"Content-Type":        {"text/html"},
	}
	got := RedactHeaders(headers)
	for _, key := range []string{"Cookie", "Set-Cookie", "Authorization", "Proxy-Authorization"} {
		if got.Get(key) != "[REDACTED]" {
			t.Fatalf("%s=%q", key, got.Get(key))
		}
	}
	if got.Get("Content-Type") != "text/html" {
		t.Fatalf("content-type=%q", got.Get("Content-Type"))
	}
}

func TestSaveHTMLAndScreenshot(t *testing.T) {
	store, err := New(t.TempDir(), 8)
	if err != nil {
		t.Fatal(err)
	}
	htmlPath, err := store.SaveHTML("req_1", "desktop_http", []byte("<html>raw</html>"))
	if err != nil {
		t.Fatal(err)
	}
	pngPath, err := store.SaveScreenshot("req_1", "chromedp", []byte("png"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(htmlPath) != ".html" || filepath.Ext(pngPath) != ".png" {
		t.Fatalf("paths=%q %q", htmlPath, pngPath)
	}
	body, err := os.ReadFile(htmlPath)
	if err != nil || string(body) != "<html>raw</html>" {
		t.Fatalf("body=%q err=%v", body, err)
	}
}

func TestSaveRejectsUnsafeSegments(t *testing.T) {
	store, err := New(t.TempDir(), 8)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveHTML("../escape", "desktop_http", []byte("x")); err == nil {
		t.Fatal("expected unsafe request ID error")
	}
}

func TestPreviewAndHash(t *testing.T) {
	preview, hash := PreviewAndHash([]byte("0123456789"), 4)
	if preview != "0123" || len(hash) != 64 || strings.Contains(hash, " ") {
		t.Fatalf("preview=%q hash=%q", preview, hash)
	}
}
