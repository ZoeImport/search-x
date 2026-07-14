package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/searchquality"
)

type selectorSearcherStub struct {
	mu        sync.Mutex
	responses map[domain.ProviderName]domain.SearchResponse
	errors    map[domain.ProviderName]error
	delays    map[domain.ProviderName]time.Duration
	calls     []domain.ProviderName
}

func (stub *selectorSearcherStub) Search(_ context.Context, request domain.SearchRequest) (domain.SearchResponse, error) {
	stub.mu.Lock()
	stub.calls = append(stub.calls, request.Provider)
	stub.mu.Unlock()
	if delay := stub.delays[request.Provider]; delay > 0 {
		time.Sleep(delay)
	}
	if err := stub.errors[request.Provider]; err != nil {
		return domain.SearchResponse{}, err
	}
	return stub.responses[request.Provider], nil
}

func (stub *selectorSearcherStub) calledProviders() []domain.ProviderName {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return append([]domain.ProviderName(nil), stub.calls...)
}

type selectorQualityStub struct {
	scores  map[string]float64
	domains map[string]int
}

func (selectorQualityStub) Name() domain.ImplementationName {
	return domain.ImplementationNameUnicodeSearchQuality
}

func (stub selectorQualityStub) Evaluate(_ string, results []domain.SearchResult) searchquality.Result {
	if len(results) == 0 {
		return searchquality.Result{Items: make([]searchquality.ItemScore, 0)}
	}
	key := results[0].Title
	items := make([]searchquality.ItemScore, len(results))
	for index, result := range results {
		items[index] = searchquality.ItemScore{OriginalRank: result.Rank, Score: stub.scores[key]}
	}
	return searchquality.Result{Score: stub.scores[key], Items: items, UniqueDomainCount: stub.domains[key]}
}

func TestQualityProviderSelectorChoosesHighestQualityResultSet(t *testing.T) {
	searcher := &selectorSearcherStub{responses: map[domain.ProviderName]domain.SearchResponse{
		domain.ProviderNameBaidu:      selectorResponse(domain.ProviderNameBaidu, "low", 3),
		domain.ProviderNameDuckDuckGo: selectorResponse(domain.ProviderNameDuckDuckGo, "high", 2),
	}}
	selector := mustSelector(t, searcher, selectorQualityStub{scores: map[string]float64{"low": 0.2, "high": 0.9}},
		domain.ProviderNameBaidu, domain.ProviderNameDuckDuckGo)

	response, selection, err := selector.Select(context.Background(), domain.SearchRequest{
		Query: "Go 语言并发模型", Provider: domain.ProviderNameAuto, Limit: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != domain.ProviderNameDuckDuckGo || selection.SelectedProvider != domain.ProviderNameDuckDuckGo {
		t.Fatalf("response=%+v selection=%+v", response, selection)
	}
	if selection.Reason != domain.SelectionReasonQuality || len(selection.Qualities) != 2 {
		t.Fatalf("selection=%+v", selection)
	}
}

func TestQualityProviderSelectorToleratesProviderFailure(t *testing.T) {
	searcher := &selectorSearcherStub{
		responses: map[domain.ProviderName]domain.SearchResponse{
			domain.ProviderNameDuckDuckGo: selectorResponse(domain.ProviderNameDuckDuckGo, "usable", 2),
		},
		errors: map[domain.ProviderName]error{
			domain.ProviderNameBaidu: &domain.SearchError{Code: domain.ErrCaptchaRequired, Retryable: true, Original: errors.New("baidu captcha")},
		},
	}
	selector := mustSelector(t, searcher, selectorQualityStub{scores: map[string]float64{"usable": 0.6}},
		domain.ProviderNameBaidu, domain.ProviderNameDuckDuckGo)

	response, selection, err := selector.Select(context.Background(), domain.SearchRequest{Query: "go", Provider: domain.ProviderNameAuto, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != domain.ProviderNameDuckDuckGo || len(selection.Failures) != 1 {
		t.Fatalf("response=%+v selection=%+v", response, selection)
	}
	if selection.Failures[0].Code != domain.ErrCaptchaRequired || selection.Failures[0].OriginalError != "baidu captcha" {
		t.Fatalf("failure=%+v", selection.Failures[0])
	}
}

func TestQualityProviderSelectorExplicitProviderDoesNotCallOthers(t *testing.T) {
	searcher := &selectorSearcherStub{responses: map[domain.ProviderName]domain.SearchResponse{
		domain.ProviderNameBing: selectorResponse(domain.ProviderNameBing, "bing", 1),
	}}
	selector := mustSelector(t, searcher, selectorQualityStub{scores: map[string]float64{"bing": 0.4}},
		domain.ProviderNameBaidu, domain.ProviderNameBing)

	_, selection, err := selector.Select(context.Background(), domain.SearchRequest{Query: "go", Provider: domain.ProviderNameBing, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Reason != domain.SelectionReasonExplicit {
		t.Fatalf("reason=%q", selection.Reason)
	}
	calls := searcher.calledProviders()
	if len(calls) != 1 || calls[0] != domain.ProviderNameBing {
		t.Fatalf("calls=%v", calls)
	}
}

func TestQualityProviderSelectorAnnotatesExplicitProviderWhenSearcherOmitsIt(t *testing.T) {
	searcher := &selectorSearcherStub{responses: map[domain.ProviderName]domain.SearchResponse{
		domain.ProviderNameBing: {Results: []domain.SearchResult{{Title: "bing", URL: "https://example.com/bing", Rank: 1}}},
	}}
	selector := mustSelector(t, searcher, selectorQualityStub{scores: map[string]float64{"bing": 0.4}}, domain.ProviderNameBing)

	response, selection, err := selector.Select(context.Background(), domain.SearchRequest{Query: "go", Provider: domain.ProviderNameBing, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != domain.ProviderNameBing || selection.SelectedProvider != domain.ProviderNameBing {
		t.Fatalf("response=%+v selection=%+v", response, selection)
	}
}

func TestQualityProviderSelectorUsesStableTieBreakers(t *testing.T) {
	searcher := &selectorSearcherStub{responses: map[domain.ProviderName]domain.SearchResponse{
		domain.ProviderNameBaidu: selectorResponse(domain.ProviderNameBaidu, "same-baidu", 2),
		domain.ProviderNameBing:  selectorResponse(domain.ProviderNameBing, "same-bing", 3),
	}}
	quality := selectorQualityStub{
		scores:  map[string]float64{"same-baidu": 0.5, "same-bing": 0.5},
		domains: map[string]int{"same-baidu": 2, "same-bing": 1},
	}
	selector := mustSelector(t, searcher, quality, domain.ProviderNameBaidu, domain.ProviderNameBing)

	response, _, err := selector.Select(context.Background(), domain.SearchRequest{Query: "go", Provider: domain.ProviderNameAuto, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != domain.ProviderNameBing {
		t.Fatalf("provider=%q", response.Provider)
	}
}

func TestQualityProviderSelectorPreservesObservedFailuresOnBudgetTimeout(t *testing.T) {
	searcher := &selectorSearcherStub{
		responses: map[domain.ProviderName]domain.SearchResponse{},
		errors: map[domain.ProviderName]error{
			domain.ProviderNameBaidu: errors.New("baidu unavailable"),
		},
		delays: map[domain.ProviderName]time.Duration{
			domain.ProviderNameDuckDuckGo: 100 * time.Millisecond,
		},
	}
	selector, err := NewQualityProviderSelector(searcher, selectorQualityStub{}, ProviderSelectorConfig{
		Providers:     []domain.ProviderName{domain.ProviderNameBaidu, domain.ProviderNameDuckDuckGo},
		MaxConcurrent: 1, Budget: 20 * time.Millisecond, EarlyScore: 0.8,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, selection, err := selector.Select(context.Background(), domain.SearchRequest{Query: "go", Provider: domain.ProviderNameAuto, Limit: 5})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if len(selection.Failures) != 1 || selection.Failures[0].Provider != domain.ProviderNameBaidu {
		t.Fatalf("selection=%+v", selection)
	}
}

func mustSelector(t *testing.T, searcher SearchExecutor, evaluator searchquality.QualityEvaluator, providers ...domain.ProviderName) *QualityProviderSelector {
	t.Helper()
	selector, err := NewQualityProviderSelector(searcher, evaluator, ProviderSelectorConfig{
		Providers: providers, MaxConcurrent: 2, Budget: time.Second, EarlyScore: 0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	return selector
}

func selectorResponse(provider domain.ProviderName, title string, count int) domain.SearchResponse {
	results := make([]domain.SearchResult, count)
	for index := range results {
		results[index] = domain.SearchResult{
			Title: title, URL: "https://example.com/" + title, Rank: index + 1, Provider: provider,
		}
	}
	return domain.SearchResponse{Provider: provider, Results: results}
}
