package duckduckgo

import (
	"os"
	"testing"
)

func TestParseResultsAndResolveRedirectURL(t *testing.T) {
	got, err := Parse(fixture(t, "normal.html"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d results=%+v", len(got), got)
	}
	if got[0].Title != "The Go Programming Language" || got[0].URL != "https://go.dev/" || got[0].Rank != 1 {
		t.Fatalf("first=%+v", got[0])
	}
	if got[1].URL != "https://pkg.go.dev/" || got[1].Rank != 2 {
		t.Fatalf("second=%+v", got[1])
	}
}

func TestParseEmptyResults(t *testing.T) {
	got, err := Parse(fixture(t, "empty.html"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("results=%+v", got)
	}
}

func TestParseRejectsChangedMarkup(t *testing.T) {
	if _, err := Parse([]byte("<html><body>unexpected</body></html>"), 10); err == nil {
		t.Fatal("expected changed markup error")
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("../../../testdata/duckduckgo/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
