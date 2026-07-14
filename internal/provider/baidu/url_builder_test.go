package baidu

import (
	"strings"
	"testing"

	"web-search-backend/internal/domain"
)

func TestBuildSearchURL(t *testing.T) {
	got, err := BuildSearchURL("https://www.baidu.com/s", domain.SearchRequest{
		Query: "中文 golang", Limit: 10, Page: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "wd=%E4%B8%AD%E6%96%87+golang") || !strings.Contains(got, "pn=10") || !strings.Contains(got, "rn=10") {
		t.Fatal(got)
	}
}

func TestBuildSearchURLOmitsPaginationParametersForFirstPage(t *testing.T) {
	got, err := BuildSearchURL("https://www.baidu.com/s?rn=99&pn=99", domain.SearchRequest{
		Query: "中文 golang", Limit: 5, Page: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "rn=") || strings.Contains(got, "pn=") {
		t.Fatal(got)
	}
}
