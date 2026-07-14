package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/searchquality"
)

// SearchExecutor executes one search request through the existing search service boundary.
type SearchExecutor interface {
	// Search executes one search request.
	Search(context.Context, domain.SearchRequest) (domain.SearchResponse, error)
}

// ProviderSelector selects one whole Provider result set for an advanced search.
type ProviderSelector interface {
	// Select returns one result set and the observations used to select it.
	Select(context.Context, domain.SearchRequest) (domain.SearchResponse, domain.ProviderSelection, error)
}

// ProviderSelectorConfig configures deterministic Provider quality selection.
type ProviderSelectorConfig struct {
	// Providers lists automatic-mode Providers in priority order.
	Providers []domain.ProviderName
	// MaxConcurrent bounds simultaneous Provider searches.
	MaxConcurrent int
	// Budget bounds automatic Provider selection.
	Budget time.Duration
	// EarlyScore permits an early choice after every higher-priority Provider completes.
	EarlyScore float64
}

// QualityProviderSelector chooses the highest-quality whole Provider result set.
type QualityProviderSelector struct {
	searcher  SearchExecutor
	evaluator searchquality.QualityEvaluator
	config    ProviderSelectorConfig
}

// providerSelectionOutcome contains one internal Provider call and quality observation.
type providerSelectionOutcome struct {
	priority int
	provider domain.ProviderName
	response domain.SearchResponse
	quality  searchquality.Result
	err      error
}

// NewQualityProviderSelector validates dependencies and creates a quality selector.
func NewQualityProviderSelector(searcher SearchExecutor, evaluator searchquality.QualityEvaluator, config ProviderSelectorConfig) (*QualityProviderSelector, error) {
	if searcher == nil {
		return nil, fmt.Errorf("provider selector searcher is nil")
	}
	if evaluator == nil {
		return nil, fmt.Errorf("provider selector evaluator is nil")
	}
	if len(config.Providers) == 0 {
		return nil, fmt.Errorf("provider selector has no automatic providers")
	}
	if config.MaxConcurrent <= 0 {
		config.MaxConcurrent = 2
	}
	if config.Budget <= 0 {
		config.Budget = 10 * time.Second
	}
	if config.EarlyScore <= 0 || config.EarlyScore > 1 {
		config.EarlyScore = 0.8
	}
	seen := make(map[domain.ProviderName]struct{}, len(config.Providers))
	for _, providerName := range config.Providers {
		if providerName == "" || providerName == domain.ProviderNameAuto {
			return nil, fmt.Errorf("invalid automatic provider %q", providerName)
		}
		if _, exists := seen[providerName]; exists {
			return nil, fmt.Errorf("duplicate automatic provider %q", providerName)
		}
		seen[providerName] = struct{}{}
	}
	config.Providers = append([]domain.ProviderName(nil), config.Providers...)
	return &QualityProviderSelector{searcher: searcher, evaluator: evaluator, config: config}, nil
}

// Select searches an explicit Provider or compares automatic Provider result sets.
func (selector *QualityProviderSelector) Select(ctx context.Context, request domain.SearchRequest) (domain.SearchResponse, domain.ProviderSelection, error) {
	requestedProvider := request.Provider
	if requestedProvider == "" {
		requestedProvider = domain.ProviderNameAuto
		request.Provider = requestedProvider
	}
	if requestedProvider != domain.ProviderNameAuto {
		return selector.selectExplicit(ctx, request)
	}
	return selector.selectAutomatic(ctx, request)
}

func (selector *QualityProviderSelector) selectExplicit(ctx context.Context, request domain.SearchRequest) (domain.SearchResponse, domain.ProviderSelection, error) {
	selection := newProviderSelection(request.Provider, domain.SelectionReasonExplicit)
	response, err := selector.searcher.Search(ctx, request)
	if err != nil {
		selection.Failures = append(selection.Failures, providerFailure(request.Provider, err))
		return domain.SearchResponse{}, selection, err
	}
	if response.Provider == "" {
		response.Provider = request.Provider
	}
	annotateResultProviders(&response)
	qualityResult := selector.evaluator.Evaluate(request.Query, response.Results)
	selection.SelectedProvider = response.Provider
	selection.Qualities = append(selection.Qualities, providerQuality(response.Provider, response.Results, qualityResult))
	return response, selection, nil
}

func (selector *QualityProviderSelector) selectAutomatic(ctx context.Context, request domain.SearchRequest) (domain.SearchResponse, domain.ProviderSelection, error) {
	selectionCtx, cancel := context.WithTimeout(ctx, selector.config.Budget)
	defer cancel()
	selection := newProviderSelection(domain.ProviderNameAuto, domain.SelectionReasonQuality)
	outcomes := make(chan providerSelectionOutcome, len(selector.config.Providers))
	jobs := make(chan int, len(selector.config.Providers))
	for priority := range selector.config.Providers {
		jobs <- priority
	}
	close(jobs)
	workerCount := selector.config.MaxConcurrent
	if workerCount > len(selector.config.Providers) {
		workerCount = len(selector.config.Providers)
	}
	for worker := 0; worker < workerCount; worker++ {
		go selector.runProviderWorker(selectionCtx, request, jobs, outcomes)
	}

	completed := make([]bool, len(selector.config.Providers))
	observed := make([]providerSelectionOutcome, 0, len(selector.config.Providers))
	var best *providerSelectionOutcome
	for len(observed) < len(selector.config.Providers) {
		select {
		case outcome := <-outcomes:
			observed = append(observed, outcome)
			completed[outcome.priority] = true
			if outcome.err == nil && (best == nil || betterProviderOutcome(outcome, *best)) {
				candidate := outcome
				best = &candidate
			}
			if best != nil && best.quality.Score >= selector.config.EarlyScore && higherPrioritiesCompleted(completed, best.priority) {
				cancel()
				return selector.finishAutomaticSelection(selection, observed, *best)
			}
		case <-selectionCtx.Done():
			if best != nil {
				return selector.finishAutomaticSelection(selection, observed, *best)
			}
			selection = selector.addObservations(selection, observed)
			return domain.SearchResponse{}, selection, &domain.SearchError{
				Code: domain.ErrUpstreamTimeout, Message: "Provider 质量选择超时", Retryable: true, Original: selectionCtx.Err(),
			}
		}
	}
	if best == nil {
		return domain.SearchResponse{}, selector.addObservations(selection, observed), combinedProviderError(observed)
	}
	return selector.finishAutomaticSelection(selection, observed, *best)
}

func (selector *QualityProviderSelector) runProviderWorker(ctx context.Context, request domain.SearchRequest, jobs <-chan int, outcomes chan<- providerSelectionOutcome) {
	for priority := range jobs {
		if ctx.Err() != nil {
			return
		}
		providerName := selector.config.Providers[priority]
		providerRequest := request
		providerRequest.Provider = providerName
		response, err := selector.searcher.Search(ctx, providerRequest)
		outcome := providerSelectionOutcome{priority: priority, provider: providerName, response: response, err: err}
		if err == nil {
			if response.Provider == "" {
				response.Provider = providerName
			}
			annotateResultProviders(&response)
			outcome.response = response
			outcome.quality = selector.evaluator.Evaluate(request.Query, response.Results)
		}
		outcomes <- outcome
	}
}

func (selector *QualityProviderSelector) finishAutomaticSelection(selection domain.ProviderSelection, outcomes []providerSelectionOutcome, best providerSelectionOutcome) (domain.SearchResponse, domain.ProviderSelection, error) {
	selection = selector.addObservations(selection, outcomes)
	selection.SelectedProvider = best.response.Provider
	return best.response, selection, nil
}

func (selector *QualityProviderSelector) addObservations(selection domain.ProviderSelection, outcomes []providerSelectionOutcome) domain.ProviderSelection {
	sortedOutcomes := append([]providerSelectionOutcome(nil), outcomes...)
	sort.Slice(sortedOutcomes, func(first, second int) bool {
		return sortedOutcomes[first].priority < sortedOutcomes[second].priority
	})
	for _, outcome := range sortedOutcomes {
		if outcome.err != nil {
			selection.Failures = append(selection.Failures, providerFailure(outcome.provider, outcome.err))
			continue
		}
		selection.Qualities = append(selection.Qualities, providerQuality(outcome.response.Provider, outcome.response.Results, outcome.quality))
	}
	return selection
}

func newProviderSelection(requested domain.ProviderName, reason domain.SelectionReason) domain.ProviderSelection {
	return domain.ProviderSelection{
		RequestedProvider: requested, Reason: reason,
		Qualities: make([]domain.ProviderQuality, 0), Failures: make([]domain.ProviderFailure, 0),
	}
}

func providerQuality(providerName domain.ProviderName, results []domain.SearchResult, result searchquality.Result) domain.ProviderQuality {
	return domain.ProviderQuality{
		Provider: providerName, Score: result.Score,
		ResultCount: len(results), UniqueDomainCount: result.UniqueDomainCount,
	}
}

func providerFailure(providerName domain.ProviderName, err error) domain.ProviderFailure {
	failure := domain.ProviderFailure{Provider: providerName, Code: domain.ErrProviderUnavailable, Retryable: true}
	var searchErr *domain.SearchError
	if errors.As(err, &searchErr) {
		failure.Code = searchErr.Code
		failure.Retryable = searchErr.Retryable
		if searchErr.Original != nil {
			failure.OriginalError = searchErr.Original.Error()
		} else {
			failure.OriginalError = searchErr.Error()
		}
		return failure
	}
	if err != nil {
		failure.OriginalError = err.Error()
	}
	return failure
}

func betterProviderOutcome(candidate, current providerSelectionOutcome) bool {
	if candidate.quality.Score != current.quality.Score {
		return candidate.quality.Score > current.quality.Score
	}
	if len(candidate.response.Results) != len(current.response.Results) {
		return len(candidate.response.Results) > len(current.response.Results)
	}
	if candidate.quality.UniqueDomainCount != current.quality.UniqueDomainCount {
		return candidate.quality.UniqueDomainCount > current.quality.UniqueDomainCount
	}
	return candidate.priority < current.priority
}

func higherPrioritiesCompleted(completed []bool, priority int) bool {
	for index := 0; index < priority; index++ {
		if !completed[index] {
			return false
		}
	}
	return true
}

func combinedProviderError(outcomes []providerSelectionOutcome) error {
	errorsList := make([]error, 0, len(outcomes))
	attempts := make([]domain.Attempt, 0)
	retryable := false
	for _, outcome := range outcomes {
		if outcome.err == nil {
			continue
		}
		errorsList = append(errorsList, outcome.err)
		var searchErr *domain.SearchError
		if errors.As(outcome.err, &searchErr) {
			retryable = retryable || searchErr.Retryable
			attempts = append(attempts, searchErr.Attempts...)
		}
	}
	return &domain.SearchError{
		Code: domain.ErrProviderUnavailable, Message: "所有 Provider 均不可用", Retryable: retryable,
		Original: errors.Join(errorsList...), Attempts: attempts,
	}
}
