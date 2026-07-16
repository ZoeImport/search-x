package brave

import (
	"strings"
	"testing"

	"web-search-backend/websearch/internal/domain"
)

func TestBuildSearchURL(t *testing.T) {
	result, err := BuildSearchURL("https://search.brave.com/search", domain.SearchRequest{
		Query: "Go 语言", Limit: 10, Page: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "q=Go+%E8%AF%AD%E8%A8%80") || !strings.Contains(result, "source=web") || !strings.Contains(result, "offset=1") {
		t.Fatalf("url=%s", result)
	}
}

func TestBuildSearchURLIgnoresRegion(t *testing.T) {
	withRegion, err := BuildSearchURL("https://search.brave.com/search", domain.SearchRequest{
		Query: "golang", Limit: 10, Page: 1, Region: "jp",
	})
	if err != nil {
		t.Fatal(err)
	}
	withoutRegion, err := BuildSearchURL("https://search.brave.com/search", domain.SearchRequest{
		Query: "golang", Limit: 10, Page: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if withRegion != withoutRegion {
		t.Fatalf("region changed URL: with=%s without=%s", withRegion, withoutRegion)
	}
}
