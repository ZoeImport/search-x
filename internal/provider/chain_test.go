package provider

import (
	"context"
	"errors"
	"testing"

	"web-search-backend/internal/domain"
)

type chainStub struct {
	name     domain.ProviderName
	response domain.SearchResponse
	err      error
	calls    int
}

func (s *chainStub) Name() domain.ProviderName {
	return s.name
}

func (s *chainStub) Search(_ context.Context, request domain.SearchRequest) (domain.SearchResponse, error) {
	s.calls++
	if request.Provider != s.name {
		return domain.SearchResponse{}, &domain.SearchError{
			Code: domain.ErrInvalidRequest, Message: "provider request mismatch", Retryable: false,
			Original: errors.New("provider request mismatch"),
		}
	}
	return s.response, s.err
}

func TestChainReturnsFirstProviderSuccess(t *testing.T) {
	first := &chainStub{name: domain.ProviderNameBaidu, response: domain.SearchResponse{
		Provider: domain.ProviderNameBaidu, Results: []domain.SearchResult{{Title: "Go"}},
	}}
	second := &chainStub{name: domain.ProviderNameDuckDuckGo}
	chain, err := NewChain(domain.ProviderNameAuto, first, second)
	if err != nil {
		t.Fatal(err)
	}
	got, err := chain.Search(context.Background(), domain.SearchRequest{Provider: domain.ProviderNameAuto})
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != domain.ProviderNameBaidu || got.Meta.RequestedProvider != domain.ProviderNameAuto {
		t.Fatalf("response=%+v", got)
	}
	if first.calls != 1 || second.calls != 0 {
		t.Fatalf("calls first=%d second=%d", first.calls, second.calls)
	}
}

func TestChainFallsBackOnRetryableProviderError(t *testing.T) {
	first := &chainStub{name: domain.ProviderNameBaidu, err: &domain.SearchError{
		Code: domain.ErrCaptchaRequired, Message: "captcha", Retryable: true, Original: errors.New("baidu captcha"),
		Attempts:  []domain.Attempt{{Transport: domain.TransportNameDesktopHTTP, OriginalError: "baidu captcha"}},
		Artifacts: []string{"baidu.html"},
	}}
	second := &chainStub{name: domain.ProviderNameDuckDuckGo, response: domain.SearchResponse{
		Provider: domain.ProviderNameDuckDuckGo,
		Results:  []domain.SearchResult{{Title: "Go"}},
		Debug: &domain.Debug{
			Attempts:     []domain.Attempt{{Transport: domain.TransportNameDuckDuckGoHTTP}},
			RawArtifacts: []string{"duck.html"},
		},
	}}
	chain, err := NewChain(domain.ProviderNameAuto, first, second)
	if err != nil {
		t.Fatal(err)
	}
	got, err := chain.Search(context.Background(), domain.SearchRequest{Provider: domain.ProviderNameAuto, Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != domain.ProviderNameDuckDuckGo || got.Meta.ProviderFallbackCount != 1 || !got.Meta.Degraded {
		t.Fatalf("response=%+v", got)
	}
	if len(got.Warnings) != 1 || got.Warnings[0].Code != "provider_fallback" {
		t.Fatalf("warnings=%+v", got.Warnings)
	}
	if got.Debug == nil || len(got.Debug.Attempts) != 2 {
		t.Fatalf("debug=%+v", got.Debug)
	}
	if len(got.Debug.RawArtifacts) != 2 || got.Debug.RawArtifacts[0] != "baidu.html" || got.Debug.RawArtifacts[1] != "duck.html" {
		t.Fatalf("artifacts=%+v", got.Debug.RawArtifacts)
	}
	if got.Debug.Attempts[0].Provider != domain.ProviderNameBaidu || got.Debug.Attempts[1].Provider != domain.ProviderNameDuckDuckGo {
		t.Fatalf("attempts=%+v", got.Debug.Attempts)
	}
}

func TestChainStopsOnNonRetryableProviderError(t *testing.T) {
	first := &chainStub{name: domain.ProviderNameBaidu, err: &domain.SearchError{
		Code: domain.ErrInvalidRequest, Message: "bad input", Retryable: false, Original: errors.New("bad input"),
	}}
	second := &chainStub{name: domain.ProviderNameDuckDuckGo}
	chain, err := NewChain(domain.ProviderNameAuto, first, second)
	if err != nil {
		t.Fatal(err)
	}
	_, gotErr := chain.Search(context.Background(), domain.SearchRequest{Provider: domain.ProviderNameAuto})
	if gotErr == nil || second.calls != 0 {
		t.Fatalf("err=%v second_calls=%d", gotErr, second.calls)
	}
}

func TestChainAggregatesAllProviderFailures(t *testing.T) {
	first := &chainStub{name: domain.ProviderNameBaidu, err: &domain.SearchError{
		Code: domain.ErrCaptchaRequired, Retryable: true, Original: errors.New("baidu captcha"),
		Attempts: []domain.Attempt{{Transport: domain.TransportNameDesktopHTTP, OriginalError: "baidu captcha"}},
	}}
	second := &chainStub{name: domain.ProviderNameDuckDuckGo, err: &domain.SearchError{
		Code: domain.ErrRateLimited, Retryable: true, Original: errors.New("duck rate limited"),
		Attempts: []domain.Attempt{{Transport: domain.TransportNameDuckDuckGoHTTP, OriginalError: "duck rate limited"}},
	}}
	chain, err := NewChain(domain.ProviderNameAuto, first, second)
	if err != nil {
		t.Fatal(err)
	}
	_, gotErr := chain.Search(context.Background(), domain.SearchRequest{Provider: domain.ProviderNameAuto, Debug: true})
	var searchErr *domain.SearchError
	if !errors.As(gotErr, &searchErr) {
		t.Fatalf("error=%T %v", gotErr, gotErr)
	}
	if searchErr.Code != domain.ErrProviderUnavailable || len(searchErr.Attempts) != 2 {
		t.Fatalf("search_error=%+v", searchErr)
	}
	if searchErr.Attempts[0].Provider != domain.ProviderNameBaidu || searchErr.Attempts[1].Provider != domain.ProviderNameDuckDuckGo {
		t.Fatalf("attempts=%+v", searchErr.Attempts)
	}
}

func TestNewChainRejectsInvalidMembers(t *testing.T) {
	if _, err := NewChain("", &chainStub{name: domain.ProviderNameBaidu}); err == nil {
		t.Fatal("expected empty chain name error")
	}
	if _, err := NewChain(domain.ProviderNameAuto); err == nil {
		t.Fatal("expected empty members error")
	}
	if _, err := NewChain(domain.ProviderNameAuto, nil); err == nil {
		t.Fatal("expected nil member error")
	}
}
