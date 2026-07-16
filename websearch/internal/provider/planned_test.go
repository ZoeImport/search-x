package provider

import (
	"context"
	"testing"

	"web-search-backend/websearch/internal/domain"
)

type plannedStub struct {
	name    domain.ProviderName
	request domain.SearchRequest
	results []domain.SearchResult
}

func (stub *plannedStub) Name() domain.ProviderName { return stub.name }
func (stub *plannedStub) Search(_ context.Context, request domain.SearchRequest) (domain.SearchResponse, error) {
	stub.request = request
	return domain.SearchResponse{
		Query: request.Query, Provider: stub.name, Results: append([]domain.SearchResult(nil), stub.results...),
	}, nil
}

func TestPlannedProviderCompilesPerProviderAndFinalizesResults(t *testing.T) {
	upstream := &plannedStub{
		name: domain.ProviderNameBrave,
		results: []domain.SearchResult{
			{URL: "https://go.dev/doc/?utm_source=test", Title: "first"},
			{URL: "https://go.dev/doc/", Title: "duplicate"},
			{URL: "https://example.com/", Title: "outside"},
		},
	}
	planned := NewPlanned(upstream)
	response, err := planned.Search(context.Background(), domain.SearchRequest{
		Query: "context", Limit: 10,
		Filters:      domain.SearchFilters{IncludeDomains: []string{"go.dev"}},
		QueryOptions: domain.SearchQueryOptions{TitleTerms: []string{"guide"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if upstream.request.Query != "context" || upstream.request.ProviderQuery != "context intitle:guide site:go.dev" {
		t.Fatalf("request=%+v", upstream.request)
	}
	if response.Query != "context" || len(response.Results) != 1 {
		t.Fatalf("response=%+v", response)
	}
	if response.Results[0].CanonicalURL != "https://go.dev/doc/" || response.Results[0].Domain != "go.dev" {
		t.Fatalf("result=%+v", response.Results[0])
	}
}

func TestPlannedProviderRejectsUnsupportedQueryOptions(t *testing.T) {
	planned := NewPlanned(&plannedStub{name: domain.ProviderNameBing})
	_, err := planned.Search(context.Background(), domain.SearchRequest{
		Query: "context", QueryOptions: domain.SearchQueryOptions{TitleTerms: []string{"guide"}},
	})
	if err == nil {
		t.Fatal("expected unsupported query option error")
	}
	searchErr, ok := err.(*domain.SearchError)
	if !ok || searchErr.Code != domain.ErrInvalidRequest || searchErr.Retryable {
		t.Fatalf("err=%#v", err)
	}
}
