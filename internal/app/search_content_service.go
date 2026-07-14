package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/searchquality"
)

// SearchContentOrchestrator executes an advanced search with optional readable bodies.
type SearchContentOrchestrator interface {
	// Search selects a Provider result set and returns usable bodies.
	Search(context.Context, domain.SearchContentRequest) (domain.SearchContentResponse, error)
}

// SearchContentService coordinates Provider selection, candidate reading, and rank-preserving output.
type SearchContentService struct {
	selector  ProviderSelector
	scheduler CandidateScheduler
	evaluator searchquality.QualityEvaluator
	now       func() time.Time
}

// NewSearchContentService validates dependencies and creates a combined search service.
func NewSearchContentService(selector ProviderSelector, scheduler CandidateScheduler, evaluator searchquality.QualityEvaluator, now func() time.Time) (*SearchContentService, error) {
	if selector == nil || scheduler == nil || evaluator == nil {
		return nil, fmt.Errorf("search content service dependencies must not be nil")
	}
	if now == nil {
		now = time.Now
	}
	return &SearchContentService{selector: selector, scheduler: scheduler, evaluator: evaluator, now: now}, nil
}

// Search oversamples candidates and returns the highest-ranked usable bodies.
func (service *SearchContentService) Search(ctx context.Context, request domain.SearchContentRequest) (domain.SearchContentResponse, error) {
	started := service.now()
	normalized, err := request.Normalize()
	if err != nil {
		return domain.SearchContentResponse{}, contentSearchError(domain.ErrInvalidRequest, "组合搜索参数错误", false, err, nil)
	}
	if !normalized.Content.Enabled {
		err = errors.New("content.enabled must be true for SearchContentService")
		return domain.SearchContentResponse{}, contentSearchError(domain.ErrInvalidRequest, "组合搜索未启用正文读取", false, err, nil)
	}
	candidateLimit, err := CandidateLimit(normalized.Limit, normalized.Content.CandidateLimit)
	if err != nil {
		return domain.SearchContentResponse{}, contentSearchError(domain.ErrInvalidRequest, "候选数量参数错误", false, err, nil)
	}

	searchStarted := service.now()
	searchResponse, selection, err := service.selector.Select(ctx, domain.SearchRequest{
		Query: normalized.Query, Provider: normalized.Provider, RequestID: normalized.RequestID,
		Limit: candidateLimit, Page: 1, Refresh: normalized.Refresh, Debug: normalized.Debug,
	})
	if err != nil {
		return domain.SearchContentResponse{}, err
	}
	searchTook := service.now().Sub(searchStarted)
	annotateResultProviders(&searchResponse)
	qualityResult := service.evaluator.Evaluate(normalized.Query, searchResponse.Results)
	scoreByURL := make(map[string]float64, len(searchResponse.Results))
	for index, searchResult := range searchResponse.Results {
		if index < len(qualityResult.Items) {
			scoreByURL[canonicalCandidateKey(searchResult.URL)] = qualityResult.Items[index].Score
		}
	}

	readStarted := service.now()
	readResults := service.scheduler.Schedule(ctx, ReadScheduleRequest{
		Candidates: searchResponse.Results, TargetCount: normalized.Limit, Content: normalized.Content,
		Refresh: normalized.Refresh, Debug: normalized.Debug, RequestID: normalized.RequestID,
	})
	readTook := service.now().Sub(readStarted)
	response := domain.SearchContentResponse{
		Query: normalized.Query, RequestedProvider: normalized.Provider, SelectedProvider: searchResponse.Provider,
		CandidateCount: uniqueCandidateCount(searchResponse.Results),
		Results:        make([]domain.SearchContentResult, 0, normalized.Limit),
		Failures:       make([]domain.SearchContentFailure, 0),
		Warnings:       append([]domain.Warning(nil), searchResponse.Warnings...),
		Meta: domain.SearchContentMeta{
			SearchTookMS: searchTook.Milliseconds(), ReadTookMS: readTook.Milliseconds(),
			TookMS: service.now().Sub(started).Milliseconds(), RequestID: normalized.RequestID,
		},
	}
	readAttempts := make(map[string][]domain.ReadAttempt)
	allReadAttempts := make([]domain.ReadAttempt, 0)
	readErrors := make([]error, 0)
	for _, readResult := range readResults {
		if readResult.Err != nil {
			response.Failures = append(response.Failures, searchContentFailure(readResult, normalized.Debug))
			readErrors = append(readErrors, readResult.Err)
			attempts := attemptsFromReadError(readResult.Err)
			readAttempts[readResult.Candidate.URL] = attempts
			allReadAttempts = append(allReadAttempts, attempts...)
			continue
		}
		if len(response.Results) >= normalized.Limit {
			continue
		}
		if readResult.Response.Debug != nil {
			attempts := append([]domain.ReadAttempt(nil), readResult.Response.Debug.Attempts...)
			readAttempts[readResult.Candidate.URL] = attempts
			allReadAttempts = append(allReadAttempts, attempts...)
		}
		response.Results = append(response.Results, successfulSearchContentResult(
			readResult, len(response.Results)+1, scoreByURL[canonicalCandidateKey(readResult.Candidate.URL)],
		))
	}
	response.ReadableCount = len(response.Results)
	if response.ReadableCount == 0 {
		return domain.SearchContentResponse{}, contentSearchError(
			domain.ErrInsufficientReadableResults, "没有候选网页能够提取有效正文", true,
			errors.Join(readErrors...), allReadAttempts,
		)
	}
	if response.ReadableCount < normalized.Limit {
		response.Meta.Partial = true
		response.Warnings = append(response.Warnings, domain.Warning{
			Code: domain.WarningCodePartialReadableResults, Message: "可读正文数量少于请求数量",
		})
	}
	if normalized.Debug {
		response.Debug = &domain.SearchContentDebug{Selection: selection, ReadAttempts: readAttempts}
	}
	return response, nil
}

func successfulSearchContentResult(result CandidateReadResult, selectedRank int, relevance float64) domain.SearchContentResult {
	title := result.Candidate.Title
	if title == "" {
		title = result.Response.Title
	}
	return domain.SearchContentResult{
		OriginalRank: result.OriginalRank, SelectedRank: selectedRank, Provider: result.Candidate.Provider,
		Title: title, URL: result.Candidate.URL, Snippet: result.Candidate.Snippet, RelevanceScore: relevance,
		ReadStatus: domain.ReadStatusSuccess, ReadTransport: result.Response.Meta.Transport,
		Content: result.Response.Content, ContentFormat: result.Response.ContentFormat,
		ContentLength: result.Response.ContentLength, Truncated: result.Response.Truncated,
	}
}

func searchContentFailure(result CandidateReadResult, debug bool) domain.SearchContentFailure {
	failure := domain.SearchContentFailure{
		OriginalRank: result.OriginalRank, Provider: result.Candidate.Provider, URL: result.Candidate.URL,
		Code: readErrorCode(result.Err), Retryable: true,
	}
	var readErr *domain.ReadError
	if errors.As(result.Err, &readErr) {
		failure.Code = readErr.Code
		failure.Retryable = readErr.Retryable
	}
	if debug && result.Err != nil {
		failure.OriginalError = result.Err.Error()
	}
	return failure
}

func contentSearchError(code domain.ErrorCode, message string, retryable bool, original error, attempts []domain.ReadAttempt) error {
	return &domain.SearchError{
		Code: code, Message: message, Retryable: retryable, Original: original,
		ReadAttempts: append([]domain.ReadAttempt(nil), attempts...),
	}
}
