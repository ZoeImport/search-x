package site

import (
	"context"
	"net/url"
	"testing"

	"web-search-backend/webfetch/internal/domain"
)

type namedStrategy string

func (strategy namedStrategy) Name() string { return string(strategy) }
func (strategy namedStrategy) Prepare(_ context.Context, target domain.SafeTarget) (domain.SafeTarget, error) {
	return target, nil
}

func TestRegistryUsesLongestDomainThenPathMatch(t *testing.T) {
	t.Parallel()
	targetURL, _ := url.Parse("https://news.example.com/articles/42")
	registry := NewRegistry([]Rule{
		{DomainSuffix: "example.com", Strategy: namedStrategy("parent")},
		{DomainSuffix: "news.example.com", Strategy: namedStrategy("host")},
		{DomainSuffix: "news.example.com", PathPrefix: "/articles/", Strategy: namedStrategy("article")},
	}, nil)
	if got := registry.Resolve(domain.SafeTarget{URL: targetURL}).Name(); got != "article" {
		t.Fatalf("Resolve() = %q, want article", got)
	}
}
