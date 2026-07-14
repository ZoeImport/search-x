package chromebrowser

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"web-search-backend/internal/domain"
)

func TestNewRejectsMissingProfileDir(t *testing.T) {
	if _, err := New(Config{Timeout: 10 * time.Second}, testURLBuilder); err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("err=%v", err)
	}
}

func TestClientUsesInjectedURLBuilder(t *testing.T) {
	builder := func(request domain.SearchRequest) (string, error) {
		return "https://www.bing.com/search?q=" + url.QueryEscape(request.Query), nil
	}
	client, err := New(Config{
		ProfileDir: t.TempDir(),
		Timeout:    10 * time.Second,
		Headless:   true,
	}, builder)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	got, err := client.buildURL(domain.SearchRequest{Query: "go language", Limit: 10, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://www.bing.com/search?q=go+language" {
		t.Fatalf("url=%s", got)
	}
}

func TestNewCreatesReusableClientWithoutLaunchingChrome(t *testing.T) {
	client, err := New(Config{
		ProfileDir: t.TempDir(),
		Timeout:    10 * time.Second,
		Headless:   true,
	}, testURLBuilder)
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
}

func TestNewUsesConfiguredTabCapacity(t *testing.T) {
	client, err := New(Config{
		ProfileDir:        t.TempDir(),
		Timeout:           10 * time.Second,
		Headless:          true,
		MaxConcurrentTabs: 3,
	}, testURLBuilder)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if cap(client.semaphore) != 3 {
		t.Fatalf("tab capacity = %d", cap(client.semaphore))
	}
}

func TestNewRejectsMissingURLBuilder(t *testing.T) {
	_, err := New(Config{ProfileDir: t.TempDir(), Timeout: 10 * time.Second}, nil)
	if err == nil || !strings.Contains(err.Error(), "builder") {
		t.Fatalf("err=%v", err)
	}
}

func testURLBuilder(_ domain.SearchRequest) (string, error) {
	return "https://example.com/search", nil
}
