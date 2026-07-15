package domain

import "testing"

func TestSearchContentRequestNormalizeAppliesDefaults(t *testing.T) {
	request, err := (SearchContentRequest{
		Query:   "  Go   语言  ",
		Limit:   5,
		Content: ContentOptions{Enabled: true},
	}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if request.Query != "Go 语言" {
		t.Fatalf("query = %q", request.Query)
	}
	if request.Provider != ProviderNameAuto {
		t.Fatalf("provider = %q", request.Provider)
	}
	if request.Content.Format != OutputFormatMarkdown {
		t.Fatalf("format = %q", request.Content.Format)
	}
	if request.Content.MaxChars != DefaultReadMaxChars {
		t.Fatalf("max chars = %d", request.Content.MaxChars)
	}
}

func TestSearchContentRequestNormalizeDebugForcesRefresh(t *testing.T) {
	request, err := (SearchContentRequest{Query: "go", Debug: true}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if !request.Refresh {
		t.Fatal("debug request did not force refresh")
	}
}

func TestSearchContentRequestNormalizeRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		request SearchContentRequest
	}{
		{name: "empty query", request: SearchContentRequest{}},
		{name: "limit too large", request: SearchContentRequest{Query: "go", Limit: 11}},
		{name: "invalid format", request: SearchContentRequest{Query: "go", Content: ContentOptions{Enabled: true, Format: OutputFormat("html")}}},
		{name: "max chars too small", request: SearchContentRequest{Query: "go", Content: ContentOptions{Enabled: true, MaxChars: MinReadMaxChars - 1}}},
		{name: "invalid region", request: SearchContentRequest{Query: "go", Region: "usa"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.request.Normalize(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestSearchContentRequestNormalizeLowercasesRegion(t *testing.T) {
	request, err := (SearchContentRequest{Query: "go", Region: "  JP  "}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if request.Region != "jp" {
		t.Fatalf("region = %q", request.Region)
	}
}
