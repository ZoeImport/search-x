package httpsearch

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/headerprofile"
)

func TestClientFetchPreservesRawResponseAndQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("wd"); got != "中文 golang" {
			t.Fatalf("wd=%q", got)
		}
		if got := r.URL.Query().Get("pn"); got != "10" {
			t.Fatalf("pn=%q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "demo-agent" {
			t.Fatalf("user-agent=%q", got)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `<html><div id="content_left"></div></html>`)
	}))
	defer server.Close()

	client, err := New(Config{
		Name:         "desktop_http",
		BaseURL:      server.URL,
		UserAgent:    "demo-agent",
		Timeout:      time.Second,
		MaxBodyBytes: 1024,
	}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Fetch(context.Background(), domain.SearchRequest{Query: "中文 golang", Limit: 10, Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got.StatusCode != http.StatusTooManyRequests || got.RequestURL == "" || got.FinalURL == "" {
		t.Fatalf("response=%#v", got)
	}
	if !bytes.Contains(got.Body, []byte("content_left")) {
		t.Fatalf("body=%q", got.Body)
	}
}

func TestClientFetchOmitsPaginationParametersForFirstPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Has("pn") || request.URL.Query().Has("rn") {
			t.Fatalf("unexpected first-page query: %s", request.URL.RawQuery)
		}
		_, _ = io.WriteString(writer, `<html><div id="content_left"></div></html>`)
	}))
	defer server.Close()
	client, err := New(Config{Name: "desktop_http", BaseURL: server.URL + "?pn=9&rn=9", Timeout: time.Second, MaxBodyBytes: 1024}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Fetch(context.Background(), domain.SearchRequest{Query: "go", Limit: 5, Page: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestClientFetchRejectsOversizedBodyAndReturnsPartialResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 65))
	}))
	defer server.Close()
	client, err := New(Config{Name: "desktop_http", BaseURL: server.URL, Timeout: time.Second, MaxBodyBytes: 64}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Fetch(context.Background(), domain.SearchRequest{Query: "q", Limit: 10, Page: 1})
	if err == nil || !strings.Contains(err.Error(), "exceeds 64 bytes") {
		t.Fatalf("response=%#v err=%v", got, err)
	}
	if len(got.Body) != 64 || got.StatusCode != http.StatusOK {
		t.Fatalf("response=%#v", got)
	}
}

func TestClientFetchUsesStickyHeaderProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "pooled-agent" {
			t.Fatalf("user-agent=%q", got)
		}
		if got := r.Header.Get("Accept-Language"); got != "en-US,en;q=0.9" {
			t.Fatalf("accept-language=%q", got)
		}
		if got := r.Header.Get("X-Test-Profile"); got != "profile-value" {
			t.Fatalf("x-test-profile=%q", got)
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()
	pool, err := headerprofile.NewStaticPool([]headerprofile.Profile{{
		Name: "pooled", UserAgent: "pooled-agent", AcceptLanguage: "en-US,en;q=0.9",
		Headers: map[string]string{"Accept": "text/html", "X-Test-Profile": "profile-value"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{
		Name: "desktop_http", BaseURL: server.URL, Timeout: time.Second,
		MaxBodyBytes: 1024, HeaderProfiles: pool,
	}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Fetch(context.Background(), domain.SearchRequest{RequestID: "request-1", Query: "go", Limit: 10, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.HeaderProfile != "pooled" {
		t.Fatalf("header profile=%q", got.HeaderProfile)
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	if _, err := New(Config{}, nil); err == nil {
		t.Fatal("expected validation error")
	}
}
