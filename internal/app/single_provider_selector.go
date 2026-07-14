package app

import (
	"context"
	"fmt"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/searchquality"
)

// SingleProviderSelector evaluates the one result set selected by the capacity-aware route.
type SingleProviderSelector struct {
	searcher  SearchExecutor
	evaluator searchquality.QualityEvaluator
}

func NewSingleProviderSelector(searcher SearchExecutor, evaluator searchquality.QualityEvaluator) (*SingleProviderSelector, error) {
	if searcher == nil || evaluator == nil {
		return nil, fmt.Errorf("single provider selector dependencies are nil")
	}
	return &SingleProviderSelector{searcher: searcher, evaluator: evaluator}, nil
}

func (selector *SingleProviderSelector) Select(ctx context.Context, request domain.SearchRequest) (domain.SearchResponse, domain.ProviderSelection, error) {
	response, err := selector.searcher.Search(ctx, request)
	selection := newProviderSelection(request.Provider, domain.SelectionReasonExplicit)
	if request.Provider == "" || request.Provider == domain.ProviderNameAuto {
		selection.RequestedProvider = domain.ProviderNameAuto
		selection.Reason = domain.SelectionReasonQuality
	}
	if err != nil {
		selection.Failures = append(selection.Failures, providerFailure(request.Provider, err))
		return domain.SearchResponse{}, selection, err
	}
	annotateResultProviders(&response)
	qualityResult := selector.evaluator.Evaluate(request.Query, response.Results)
	selection.SelectedProvider = response.Provider
	selection.Qualities = append(selection.Qualities, providerQuality(response.Provider, response.Results, qualityResult))
	return response, selection, nil
}
