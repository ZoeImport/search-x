package app

import (
	"context"
	"errors"
	"testing"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/searchquality"
)

type contentSelectorStub struct {
	request   domain.SearchRequest
	response  domain.SearchResponse
	selection domain.ProviderSelection
	err       error
}

func (stub *contentSelectorStub) Select(_ context.Context, request domain.SearchRequest) (domain.SearchResponse, domain.ProviderSelection, error) {
	stub.request = request
	return stub.response, stub.selection, stub.err
}

func TestSearchContentServiceOversamplesAndKeepsBothRanks(t *testing.T) {
	selector := &contentSelectorStub{
		response: domain.SearchResponse{Provider: domain.ProviderNameDuckDuckGo, Results: rankedCandidates(12)},
		selection: domain.ProviderSelection{
			RequestedProvider: domain.ProviderNameAuto, SelectedProvider: domain.ProviderNameDuckDuckGo,
			Reason: domain.SelectionReasonQuality, Qualities: make([]domain.ProviderQuality, 0), Failures: make([]domain.ProviderFailure, 0),
		},
	}
	reader := &scheduledReaderStub{failures: map[string]error{
		candidateURL(1): readFailure("rank 1 failed"),
		candidateURL(2): readFailure("rank 2 failed"),
		candidateURL(3): readFailure("rank 3 failed"),
	}}
	scheduler, err := NewReadScheduler(reader, 8)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewSearchContentService(selector, scheduler, searchquality.NewEvaluator(searchquality.DefaultConfig()), nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := service.Search(context.Background(), domain.SearchContentRequest{
		Query: "go", Provider: domain.ProviderNameAuto, Limit: 5,
		Content: domain.ContentOptions{Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selector.request.Limit != 12 || response.CandidateCount != 12 || response.ReadableCount != 5 {
		t.Fatalf("search request=%+v response=%+v", selector.request, response)
	}
	if len(response.Results) != 5 || response.Results[0].OriginalRank != 4 || response.Results[0].SelectedRank != 1 {
		t.Fatalf("results=%+v", response.Results)
	}
	if response.Results[4].OriginalRank != 8 || response.Results[4].SelectedRank != 5 {
		t.Fatalf("results=%+v", response.Results)
	}
	if len(response.Failures) != 3 || response.Failures[0].OriginalRank != 1 {
		t.Fatalf("failures=%+v", response.Failures)
	}
}

func TestSearchContentServiceReturnsPartialSuccess(t *testing.T) {
	candidates := rankedCandidates(4)
	selector := &contentSelectorStub{
		response:  domain.SearchResponse{Provider: domain.ProviderNameBaidu, Results: candidates},
		selection: domain.ProviderSelection{RequestedProvider: domain.ProviderNameAuto, SelectedProvider: domain.ProviderNameBaidu},
	}
	reader := &scheduledReaderStub{failures: map[string]error{
		candidateURL(2): readFailure("rank 2 failed"),
		candidateURL(3): readFailure("rank 3 failed"),
		candidateURL(4): readFailure("rank 4 failed"),
	}}
	scheduler, _ := NewReadScheduler(reader, 4)
	service, _ := NewSearchContentService(selector, scheduler, searchquality.NewEvaluator(searchquality.DefaultConfig()), nil)

	response, err := service.Search(context.Background(), domain.SearchContentRequest{
		Query: "go", Limit: 3, Content: domain.ContentOptions{Enabled: true, CandidateLimit: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !response.Meta.Partial || response.ReadableCount != 1 || len(response.Warnings) != 1 || response.Warnings[0].Code != domain.WarningCodePartialReadableResults {
		t.Fatalf("response=%+v", response)
	}
}

func TestSearchContentServiceReturnsTypedErrorWhenEveryReadFails(t *testing.T) {
	selector := &contentSelectorStub{
		response:  domain.SearchResponse{Provider: domain.ProviderNameBaidu, Results: rankedCandidates(2)},
		selection: domain.ProviderSelection{RequestedProvider: domain.ProviderNameAuto, SelectedProvider: domain.ProviderNameBaidu},
	}
	reader := &scheduledReaderStub{failures: map[string]error{
		candidateURL(1): readFailure("rank 1 failed"), candidateURL(2): readFailure("rank 2 failed"),
	}}
	scheduler, _ := NewReadScheduler(reader, 2)
	service, _ := NewSearchContentService(selector, scheduler, searchquality.NewEvaluator(searchquality.DefaultConfig()), nil)

	_, gotErr := service.Search(context.Background(), domain.SearchContentRequest{
		Query: "go", Limit: 2, Content: domain.ContentOptions{Enabled: true, CandidateLimit: 2},
	})
	var searchErr *domain.SearchError
	if !errors.As(gotErr, &searchErr) || searchErr.Code != domain.ErrInsufficientReadableResults {
		t.Fatalf("error=%T %+v", gotErr, gotErr)
	}
}
