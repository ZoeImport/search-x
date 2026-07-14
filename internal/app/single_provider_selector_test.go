package app

import (
	"context"
	"testing"

	"web-search-backend/internal/domain"
)

func TestSingleProviderSelectorExecutesOnlyOneSearch(t *testing.T) {
	searcher := &selectorSearcherStub{responses: map[domain.ProviderName]domain.SearchResponse{
		domain.ProviderNameAuto: {Provider: domain.ProviderNameBaidu, Results: []domain.SearchResult{{Title: "Go", URL: "https://go.dev"}}},
	}}
	selector, err := NewSingleProviderSelector(searcher, selectorQualityStub{})
	if err != nil {
		t.Fatal(err)
	}
	response, selection, err := selector.Select(context.Background(), domain.SearchRequest{Query: "Go", Provider: domain.ProviderNameAuto})
	if err != nil {
		t.Fatal(err)
	}
	if len(searcher.calls) != 1 || response.Provider != domain.ProviderNameBaidu || selection.SelectedProvider != domain.ProviderNameBaidu {
		t.Fatalf("calls=%v response=%+v selection=%+v", searcher.calls, response, selection)
	}
}
