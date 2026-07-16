// Package site provides the extension seam for domain-specific read behavior.
package site

import (
	"context"
	"strings"

	"web-search-backend/webfetch/internal/domain"
	readpipe "web-search-backend/webfetch/internal/fetch"
)

// Rule maps a domain suffix and optional path prefix to a strategy. Rules are
// ranked by the longest matching domain, then the longest matching path.
type Rule struct {
	DomainSuffix string
	PathPrefix   string
	Strategy     readpipe.SiteStrategy
}

type Registry struct {
	rules    []Rule
	fallback readpipe.SiteStrategy
}

func NewRegistry(rules []Rule, fallback readpipe.SiteStrategy) *Registry {
	if fallback == nil {
		fallback = GenericStrategy{}
	}
	return &Registry{rules: append([]Rule(nil), rules...), fallback: fallback}
}

func (registry *Registry) Resolve(target domain.SafeTarget) readpipe.SiteStrategy {
	if target.URL == nil {
		return registry.fallback
	}
	host := strings.ToLower(strings.TrimSuffix(target.URL.Hostname(), "."))
	path := target.URL.EscapedPath()
	bestDomain, bestPath := -1, -1
	strategy := registry.fallback
	for _, rule := range registry.rules {
		domain := strings.ToLower(strings.Trim(strings.TrimSpace(rule.DomainSuffix), "."))
		if rule.Strategy == nil || domain == "" || (host != domain && !strings.HasSuffix(host, "."+domain)) || !strings.HasPrefix(path, rule.PathPrefix) {
			continue
		}
		if len(domain) > bestDomain || (len(domain) == bestDomain && len(rule.PathPrefix) > bestPath) {
			bestDomain, bestPath, strategy = len(domain), len(rule.PathPrefix), rule.Strategy
		}
	}
	return strategy
}

// GenericStrategy preserves current behavior for every site.
type GenericStrategy struct{}

func (GenericStrategy) Name() string { return "generic" }
func (GenericStrategy) Prepare(_ context.Context, target domain.SafeTarget) (domain.SafeTarget, error) {
	return target, nil
}

// TODO(read/site): add concrete strategies for resource types, domain-specific
// rendering, and verification-page detection. Captcha solving remains out of
// scope; introduce a separate ChallengeHandler interface before implementing it.
