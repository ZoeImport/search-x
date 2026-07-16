package bing

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"web-search-backend/websearch/internal/domain"
	"web-search-backend/websearch/internal/transport"
)

type fakeTransport struct {
	response transport.Response
	err      error
}

type failingArtifacts struct{}

func (failingArtifacts) SaveHTML(string, string, []byte) (string, error) {
	return "", errors.New("save HTML failed")
}

func (failingArtifacts) SaveScreenshot(string, string, []byte) (string, error) {
	return "", errors.New("save screenshot failed")
}

func (f fakeTransport) Name() domain.TransportName {
	return domain.TransportNameBingChromedp
}

func (f fakeTransport) Fetch(context.Context, domain.SearchRequest) (transport.Response, error) {
	return f.response, f.err
}

func TestNewRejectsNilTransport(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("expected nil transport error")
	}
}

func TestBuildSearchURL(t *testing.T) {
	got, err := BuildSearchURL("https://www.bing.com/search", domain.SearchRequest{
		Query: "go language", Limit: 10, Page: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "q=go+language") || !strings.Contains(got, "count=10") || !strings.Contains(got, "first=11") {
		t.Fatalf("url=%s", got)
	}
}

func TestProviderReturnsParsedResults(t *testing.T) {
	provider, err := New(fakeTransport{response: transport.Response{
		RequestURL: "https://www.bing.com/search?q=go", FinalURL: "https://www.bing.com/search?q=go",
		StatusCode: 200, Body: fixture(t, "normal.html"), HeaderProfile: "chrome_macos_150",
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := provider.Search(context.Background(), domain.SearchRequest{
		Query: "go", Provider: domain.ProviderNameBing, Limit: 10, Page: 1, Debug: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != domain.ProviderNameBing || got.Meta.Transport != domain.TransportNameBingChromedp || len(got.Results) != 2 {
		t.Fatalf("response=%+v", got)
	}
	if got.Debug == nil || len(got.Debug.Attempts) != 1 || got.Debug.Attempts[0].Provider != domain.ProviderNameBing {
		t.Fatalf("debug=%+v", got.Debug)
	}
	if got.Debug.Attempts[0].HeaderProfile != "chrome_macos_150" {
		t.Fatalf("header_profile=%q", got.Debug.Attempts[0].HeaderProfile)
	}
}

func TestProviderClassifiesCaptcha(t *testing.T) {
	provider, err := New(fakeTransport{response: transport.Response{
		RequestURL: "https://www.bing.com/search?q=go", FinalURL: "https://www.bing.com/challenge",
		StatusCode: 200, Body: fixture(t, "captcha.html"),
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

func TestProviderClassifiesTurnstileChallengeWithoutChallengeURL(t *testing.T) {
	provider, err := New(fakeTransport{response: transport.Response{
		RequestURL: "https://www.bing.com/search?q=go", FinalURL: "https://www.bing.com/search?q=go",
		StatusCode: 200, Body: fixture(t, "turnstile.html"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, gotErr := provider.Search(context.Background(), domain.SearchRequest{Query: "go", Limit: 10, Page: 1})
	var searchErr *domain.SearchError
	if !errors.As(gotErr, &searchErr) || searchErr.Code != domain.ErrCaptchaRequired {
		t.Fatalf("error=%T %+v", gotErr, gotErr)
	}
	if len(searchErr.Attempts) != 1 || searchErr.Attempts[0].Classification != "captcha" {
		t.Fatalf("attempts=%+v", searchErr.Attempts)
	}
}

func TestProviderPreservesTransportError(t *testing.T) {
	original := errors.New("chrome stopped")
	provider, err := New(fakeTransport{err: original})
	if err != nil {
		t.Fatal(err)
	}
	_, gotErr := provider.Search(context.Background(), domain.SearchRequest{Query: "go", Limit: 10, Page: 1})
	var searchErr *domain.SearchError
	if !errors.As(gotErr, &searchErr) || searchErr.Code != domain.ErrProviderUnavailable || !strings.Contains(searchErr.Error(), original.Error()) {
		t.Fatalf("error=%T %+v", gotErr, gotErr)
	}
}

func TestProviderWarnsWhenDebugArtifactSaveFails(t *testing.T) {
	provider, err := New(fakeTransport{response: transport.Response{
		RequestURL: "https://www.bing.com/search?q=go", FinalURL: "https://www.bing.com/search?q=go",
		StatusCode: 200, Body: fixture(t, "normal.html"), Screenshot: []byte("png"),
	}}, failingArtifacts{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := provider.Search(context.Background(), domain.SearchRequest{
		Query: "go", Provider: domain.ProviderNameBing, Limit: 10, Page: 1, Debug: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Warnings) != 2 || got.Warnings[0].Code != "artifact_save_error" || got.Warnings[1].Code != "artifact_save_error" {
		t.Fatalf("warnings=%+v", got.Warnings)
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("../../../testdata/bing/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
