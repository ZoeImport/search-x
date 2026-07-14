package brave

import (
	"context"
	"errors"
	"testing"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/transport"
)

type braveTransportStub struct {
	response transport.Response
	err      error
}

func (stub braveTransportStub) Name() domain.TransportName {
	return domain.TransportNameBraveChromedp
}

func (stub braveTransportStub) Fetch(context.Context, domain.SearchRequest) (transport.Response, error) {
	return stub.response, stub.err
}

func TestProviderReturnsParsedResults(t *testing.T) {
	provider, err := New(braveTransportStub{response: transport.Response{
		RequestURL: "https://search.brave.com/search?q=go", FinalURL: "https://search.brave.com/search?q=go",
		StatusCode: 200, Body: braveFixture(t, "normal.html"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := provider.Search(context.Background(), domain.SearchRequest{
		Query: "go", Provider: domain.ProviderNameBrave, Limit: 10, Page: 1, Debug: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != domain.ProviderNameBrave || response.Meta.Transport != domain.TransportNameBraveChromedp || len(response.Results) != 2 {
		t.Fatalf("response=%+v", response)
	}
	if response.Debug == nil || response.Debug.Attempts[0].Provider != domain.ProviderNameBrave {
		t.Fatalf("debug=%+v", response.Debug)
	}
}

func TestProviderClassifiesCaptcha(t *testing.T) {
	provider, err := New(braveTransportStub{response: transport.Response{
		RequestURL: "https://search.brave.com/search?q=go", FinalURL: "https://search.brave.com/challenge",
		StatusCode: 200, Body: braveFixture(t, "captcha.html"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, gotErr := provider.Search(context.Background(), domain.SearchRequest{Query: "go", Limit: 10, Page: 1})
	var searchErr *domain.SearchError
	if !errors.As(gotErr, &searchErr) || searchErr.Code != domain.ErrCaptchaRequired {
		t.Fatalf("error=%T %+v", gotErr, gotErr)
	}
}

func TestProviderPreservesTransportError(t *testing.T) {
	original := errors.New("chrome stopped")
	provider, err := New(braveTransportStub{err: original})
	if err != nil {
		t.Fatal(err)
	}
	_, gotErr := provider.Search(context.Background(), domain.SearchRequest{Query: "go", Limit: 10, Page: 1})
	var searchErr *domain.SearchError
	if !errors.As(gotErr, &searchErr) || searchErr.Code != domain.ErrProviderUnavailable || !errors.Is(searchErr, original) {
		t.Fatalf("error=%T %+v", gotErr, gotErr)
	}
}
