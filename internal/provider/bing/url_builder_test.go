package bing

import (
	"net/url"
	"testing"

	"web-search-backend/internal/domain"
)

func TestBuildSearchURLWithoutRegionOmitsMkt(t *testing.T) {
	result, err := BuildSearchURL("https://www.bing.com/search", domain.SearchRequest{
		Query: "weather", Limit: 10, Page: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(result)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed.Query()["mkt"]; ok {
		t.Fatalf("expected no mkt param without region, url=%s", result)
	}
}

func TestBuildSearchURLWithKnownRegion(t *testing.T) {
	result, err := BuildSearchURL("https://www.bing.com/search", domain.SearchRequest{
		Query: "weather", Limit: 10, Page: 1, Region: "jp",
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(result)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("mkt"); got != "ja-JP" {
		t.Fatalf("mkt=%s", got)
	}
}

func TestBuildSearchURLWithUnknownRegionFallsBackToEnglish(t *testing.T) {
	result, err := BuildSearchURL("https://www.bing.com/search", domain.SearchRequest{
		Query: "weather", Limit: 10, Page: 1, Region: "br",
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(result)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("mkt"); got != "en-BR" {
		t.Fatalf("mkt=%s", got)
	}
}
