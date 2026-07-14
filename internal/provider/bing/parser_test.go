package bing

import "testing"

func TestParseResults(t *testing.T) {
	got, err := Parse(fixture(t, "normal.html"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].URL != "https://go.dev/" || got[0].Snippet == "" || got[0].Rank != 1 {
		t.Fatalf("results=%+v", got)
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
	if _, err := Parse([]byte("<html><body>changed</body></html>"), 10); err == nil {
		t.Fatal("expected changed markup error")
	}
}
