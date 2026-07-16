package searchplan

import (
	"strings"
	"testing"

	"web-search-backend/websearch/internal/domain"
)

func TestBraveCompilerUsesDocumentedOperators(t *testing.T) {
	request, err := Normalize(domain.SearchRequest{
		Query: "concurrency",
		Filters: domain.SearchFilters{
			IncludeDomains: []string{"Pkg.Go.Dev", "go.dev"},
			ExcludeDomains: []string{"example.com"},
		},
		QueryOptions: domain.SearchQueryOptions{
			ExactPhrases: []string{"structured concurrency"},
			AnyTerms:     []string{"golang", "go language"},
			ExcludeTerms: []string{"python"},
			TitleTerms:   []string{"tutorial"},
			FileTypes:    []string{".PDF", "html"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	query, err := Compile(request, domain.ProviderNameBrave)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`concurrency`, `"structured concurrency"`, `(golang OR "go language")`,
		`-python`, `intitle:tutorial`, `(filetype:html OR filetype:pdf)`,
		`(site:go.dev OR site:pkg.go.dev)`, `NOT site:example.com`,
	} {
		if !strings.Contains(query, expected) {
			t.Fatalf("query %q does not contain %q", query, expected)
		}
	}
}

func TestCompatibleProvidersApplyStrictCapabilities(t *testing.T) {
	request := domain.SearchRequest{QueryOptions: domain.SearchQueryOptions{TitleTerms: []string{"tutorial"}}}
	got := CompatibleProviders(request, []domain.ProviderName{
		domain.ProviderNameBaidu, domain.ProviderNameBing, domain.ProviderNameBrave, domain.ProviderNameDuckDuckGo,
	})
	if len(got) != 2 || got[0] != domain.ProviderNameBrave || got[1] != domain.ProviderNameDuckDuckGo {
		t.Fatalf("providers=%v", got)
	}
}

func TestDuckDuckGoRejectsUndocumentedFileType(t *testing.T) {
	request := domain.SearchRequest{Query: "manual", QueryOptions: domain.SearchQueryOptions{FileTypes: []string{"epub"}}}
	if _, err := Compile(request, domain.ProviderNameDuckDuckGo); err == nil {
		t.Fatal("expected unsupported file type")
	}
}

func TestDuckDuckGoRejectsMultipleFileTypesWhileBraveSupportsThem(t *testing.T) {
	request := domain.SearchRequest{Query: "manual", QueryOptions: domain.SearchQueryOptions{FileTypes: []string{"pdf", "html"}}}
	if _, err := Compile(request, domain.ProviderNameDuckDuckGo); err == nil {
		t.Fatal("expected DuckDuckGo multiple file type rejection")
	}
	if _, err := Compile(request, domain.ProviderNameBrave); err != nil {
		t.Fatalf("Brave compile: %v", err)
	}
}

func TestDomainOnlyCompilerStillNarrowsAndPostFilterGuaranteesDomains(t *testing.T) {
	request := domain.SearchRequest{
		Query:   "golang",
		Filters: domain.SearchFilters{IncludeDomains: []string{"go.dev"}, ExcludeDomains: []string{"private.go.dev"}},
	}
	query, err := Compile(request, domain.ProviderNameBing)
	if err != nil {
		t.Fatal(err)
	}
	if query != "golang site:go.dev -site:private.go.dev" {
		t.Fatalf("query=%q", query)
	}
}

func TestNormalizeRejectsConflictingDomains(t *testing.T) {
	_, err := Normalize(domain.SearchRequest{
		Filters: domain.SearchFilters{
			IncludeDomains: []string{"go.dev"},
			ExcludeDomains: []string{"GO.DEV"},
		},
	})
	if err == nil {
		t.Fatal("expected conflicting domain error")
	}
}

func TestFinalizeFiltersCanonicalizesAndDeduplicates(t *testing.T) {
	request := domain.SearchRequest{
		Limit:   10,
		Filters: domain.SearchFilters{IncludeDomains: []string{"go.dev"}, ExcludeDomains: []string{"private.go.dev"}},
	}
	results := Finalize(request, []domain.SearchResult{
		{URL: "https://GO.dev/doc/?utm_source=test#intro", Title: "first"},
		{URL: "https://go.dev/doc/", Title: "duplicate"},
		{URL: "https://private.go.dev/doc/", Title: "excluded"},
		{URL: "https://example.com/", Title: "outside"},
		{URL: "https://pkg.go.dev/context?gclid=x", Title: "subdomain"},
	})
	if len(results) != 2 {
		t.Fatalf("results=%#v", results)
	}
	if results[0].CanonicalURL != "https://go.dev/doc/" || results[0].Domain != "go.dev" || results[0].Rank != 1 {
		t.Fatalf("first=%#v", results[0])
	}
	if results[1].CanonicalURL != "https://pkg.go.dev/context" || results[1].Rank != 2 {
		t.Fatalf("second=%#v", results[1])
	}
}

func TestFingerprintChangesWithFilters(t *testing.T) {
	base := domain.SearchRequest{Query: "go", Limit: 10, Providers: []domain.ProviderName{domain.ProviderNameBing}}
	filtered := base
	filtered.Filters.IncludeDomains = []string{"go.dev"}
	if Fingerprint(base) == Fingerprint(filtered) {
		t.Fatal("filter must change request fingerprint")
	}
}
