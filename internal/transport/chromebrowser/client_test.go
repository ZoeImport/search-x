package chromebrowser

import (
	"strings"
	"testing"
	"time"

	"web-search-backend/internal/domain"
)

func TestNewRejectsMissingProfileDir(t *testing.T) {
	if _, err := New(Config{Timeout: 10 * time.Second}); err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("err=%v", err)
	}
}

func TestNewCreatesReusableClientWithoutLaunchingChrome(t *testing.T) {
	client, err := New(Config{
		ProfileDir: t.TempDir(),
		BaseURL:    "https://www.baidu.com/s",
		Timeout:    10 * time.Second,
		Headless:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
}

func TestBuildSearchURL(t *testing.T) {
	got, err := buildSearchURL("https://www.baidu.com/s", domain.SearchRequest{
		Query: "中文 golang",
		Limit: 10,
		Page:  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "wd=%E4%B8%AD%E6%96%87+golang") || !strings.Contains(got, "pn=10") || !strings.Contains(got, "rn=10") {
		t.Fatal(got)
	}
}
