package reader

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/transport"
)

type fakeBrowserFetcher struct {
	response transport.Response
	err      error
}

func (fetcher fakeBrowserFetcher) FetchURL(context.Context, string) (transport.Response, error) {
	return fetcher.response, fetcher.err
}

type fakeBrowserURLPolicy struct {
	target domain.SafeTarget
	err    error
	calls  int
}

func (policy *fakeBrowserURLPolicy) ValidateAndResolve(context.Context, string) (domain.SafeTarget, error) {
	policy.calls++
	return policy.target, policy.err
}

func TestBrowserReaderReturnsRenderedDOMAndRevalidatesFinalURL(t *testing.T) {
	requestedURL, err := url.Parse("https://example.com/article")
	if err != nil {
		t.Fatal(err)
	}
	finalURL, err := url.Parse("https://www.example.com/article")
	if err != nil {
		t.Fatal(err)
	}
	policy := &fakeBrowserURLPolicy{target: domain.SafeTarget{URL: finalURL}}
	fetcher := fakeBrowserFetcher{response: transport.Response{
		RequestURL: requestedURL.String(), FinalURL: finalURL.String(), StatusCode: http.StatusOK,
		Headers: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:    []byte("<html><body><article>rendered body</article></body></html>"),
	}}
	browserReader, err := NewBrowserReader(policy, fetcher, 1024)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := browserReader.Read(context.Background(), domain.SafeTarget{URL: requestedURL})
	if err != nil {
		t.Fatal(err)
	}
	if browserReader.Name() != domain.ImplementationNameChromedpReader || resource.Transport != domain.ReadTransportChromedp {
		t.Fatalf("reader=%s transport=%s", browserReader.Name(), resource.Transport)
	}
	if resource.URL != requestedURL.String() || resource.FinalURL != finalURL.String() || resource.ContentType != "text/html" || string(resource.Body) == "" {
		t.Fatalf("resource=%#v", resource)
	}
	if policy.calls != 1 {
		t.Fatalf("policy calls=%d, want 1 final URL revalidation", policy.calls)
	}
}
