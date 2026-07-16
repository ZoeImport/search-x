package brave

import (
	"os"
	"testing"
)

func TestParseResults(t *testing.T) {
	results, err := Parse(braveFixture(t, "normal.html"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].URL != "https://go.dev/" || results[0].Snippet == "" || results[0].Rank != 1 {
		t.Fatalf("results=%+v", results)
	}
}

func TestParseEmptyResults(t *testing.T) {
	results, err := Parse(braveFixture(t, "empty.html"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if results == nil || len(results) != 0 {
		t.Fatalf("results=%+v", results)
	}
}

func TestParseRejectsChangedMarkup(t *testing.T) {
	if _, err := Parse([]byte("<html><body>changed</body></html>"), 10); err == nil {
		t.Fatal("expected changed markup error")
	}
}

func braveFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("../../../testdata/brave/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
