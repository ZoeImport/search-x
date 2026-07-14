package provider

import (
	"context"
	"errors"
	"fmt"

	"web-search-backend/internal/domain"
)

// Chain executes an ordered list of providers until one succeeds.
type Chain struct {
	name    domain.ProviderName
	members []Provider
}

// NewChain creates an immutable provider fallback chain.
func NewChain(name domain.ProviderName, members ...Provider) (*Chain, error) {
	if name == "" {
		return nil, fmt.Errorf("provider chain name is empty")
	}
	if len(members) == 0 {
		return nil, fmt.Errorf("provider chain members are empty")
	}
	seen := make(map[domain.ProviderName]struct{}, len(members))
	for index, member := range members {
		if member == nil {
			return nil, fmt.Errorf("provider chain member %d is nil", index)
		}
		memberName := member.Name()
		if memberName == "" {
			return nil, fmt.Errorf("provider chain member %d name is empty", index)
		}
		if _, exists := seen[memberName]; exists {
			return nil, fmt.Errorf("provider chain member %q is duplicated", memberName)
		}
		seen[memberName] = struct{}{}
	}
	return &Chain{name: name, members: append([]Provider(nil), members...)}, nil
}

// Name returns the registry name of the provider chain.
func (c *Chain) Name() domain.ProviderName {
	return c.name
}

// Search executes members sequentially and returns the first successful response.
func (c *Chain) Search(ctx context.Context, request domain.SearchRequest) (domain.SearchResponse, error) {
	attempts := make([]domain.Attempt, 0)
	artifacts := make([]string, 0)
	causes := make([]error, 0, len(c.members))
	failedProviders := make([]domain.ProviderName, 0, len(c.members))

	for memberIndex, member := range c.members {
		if err := ctx.Err(); err != nil {
			return domain.SearchResponse{}, err
		}

		memberRequest := request
		memberRequest.Provider = member.Name()
		response, err := member.Search(ctx, memberRequest)
		if err == nil {
			return c.prepareSuccess(response, request.Debug, member, memberIndex, failedProviders, attempts, artifacts), nil
		}

		if ctxErr := ctx.Err(); ctxErr != nil {
			return domain.SearchResponse{}, ctxErr
		}
		searchErr, ok := asSearchError(err)
		if !ok {
			return domain.SearchResponse{}, fmt.Errorf("provider %s failed: %w", member.Name(), err)
		}
		attempts = append(attempts, annotateAttempts(member.Name(), searchErr.Attempts)...)
		artifacts = append(artifacts, searchErr.Artifacts...)
		causes = append(causes, fmt.Errorf("provider %s: %w", member.Name(), searchErr))
		failedProviders = append(failedProviders, member.Name())
		if !allowsFallback(searchErr) {
			stopped := *searchErr
			stopped.Attempts = attempts
			stopped.Artifacts = artifacts
			return domain.SearchResponse{}, &stopped
		}
	}

	return domain.SearchResponse{}, &domain.SearchError{
		Code:      domain.ErrProviderUnavailable,
		Message:   "所有搜索 Provider 均不可用",
		Retryable: true,
		Original:  errors.Join(causes...),
		Attempts:  attempts,
		Artifacts: artifacts,
	}
}

func (c *Chain) prepareSuccess(
	response domain.SearchResponse,
	debug bool,
	member Provider,
	memberIndex int,
	failedProviders []domain.ProviderName,
	previousAttempts []domain.Attempt,
	previousArtifacts []string,
) domain.SearchResponse {
	response.Provider = member.Name()
	response.Meta.RequestedProvider = c.name
	response.Meta.ProviderFallbackCount = memberIndex
	if memberIndex > 0 {
		response.Meta.Degraded = true
		for _, failedProvider := range failedProviders {
			response.Warnings = append(response.Warnings, domain.Warning{
				Code:    "provider_fallback",
				Message: fmt.Sprintf("provider %s failed; using %s", failedProvider, member.Name()),
			})
		}
	}
	if !debug {
		response.Debug = nil
		return response
	}
	currentAttempts := make([]domain.Attempt, 0)
	artifacts := append([]string(nil), previousArtifacts...)
	if response.Debug != nil {
		currentAttempts = annotateAttempts(member.Name(), response.Debug.Attempts)
		artifacts = append(artifacts, response.Debug.RawArtifacts...)
	}
	response.Debug = &domain.Debug{
		Attempts:     append(append([]domain.Attempt(nil), previousAttempts...), currentAttempts...),
		RawArtifacts: artifacts,
	}
	return response
}

func annotateAttempts(providerName domain.ProviderName, attempts []domain.Attempt) []domain.Attempt {
	annotated := make([]domain.Attempt, len(attempts))
	copy(annotated, attempts)
	for index := range annotated {
		annotated[index].Provider = providerName
	}
	return annotated
}

func asSearchError(err error) (*domain.SearchError, bool) {
	var searchErr *domain.SearchError
	if !errors.As(err, &searchErr) {
		return nil, false
	}
	return searchErr, true
}

func allowsFallback(searchErr *domain.SearchError) bool {
	if searchErr == nil || !searchErr.Retryable {
		return false
	}
	switch searchErr.Code {
	case domain.ErrCaptchaRequired,
		domain.ErrRateLimited,
		domain.ErrUpstreamChanged,
		domain.ErrProviderUnavailable,
		domain.ErrUpstreamTimeout:
		return true
	default:
		return false
	}
}
