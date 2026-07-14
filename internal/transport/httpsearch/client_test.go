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

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	if _, err := New(Config{}, nil); err == nil {
		t.Fatal("expected validation error")
	}
}
