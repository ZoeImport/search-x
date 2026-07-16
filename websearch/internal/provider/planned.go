package provider

import (
	"context"
	"fmt"

	"web-search-backend/websearch/internal/domain"
	"web-search-backend/websearch/internal/searchplan"
)

// Planned compiles provider-specific query syntax and applies provider-neutral
// result guarantees around an existing provider.
type Planned struct {
	upstream Provider
}

func NewPlanned(upstream Provider) *Planned {
	return &Planned{upstream: upstream}
}

func (planned *Planned) Name() domain.ProviderName {
	if planned == nil || planned.upstream == nil {
		return ""
	}
	return planned.upstream.Name()
}

func (planned *Planned) Search(ctx context.Context, request domain.SearchRequest) (domain.SearchResponse, error) {
	if planned == nil || planned.upstream == nil {
		return domain.SearchResponse{}, &domain.SearchError{
			Code: domain.ErrProviderUnavailable, Message: "Provider 未配置", Retryable: false,
		}
	}
	providerQuery, err := searchplan.Compile(request, planned.Name())
	if err != nil {
		return domain.SearchResponse{}, &domain.SearchError{
			Code: domain.ErrInvalidRequest, Message: "Provider 不支持请求的高级搜索条件",
			Retryable: false, Original: err,
		}
	}
	upstreamRequest := request
	upstreamRequest.ProviderQuery = providerQuery
	response, err := planned.upstream.Search(ctx, upstreamRequest)
	if err != nil {
		return domain.SearchResponse{}, err
	}
	response.Results = searchplan.Finalize(request, response.Results)
	if response.Query == "" {
		response.Query = request.Query
	}
	if response.Query != request.Query {
		return domain.SearchResponse{}, fmt.Errorf("provider %s returned a mismatched query", planned.Name())
	}
	return response, nil
}
