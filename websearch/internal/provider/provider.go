package provider

import (
	"context"

	"web-search-backend/websearch/internal/domain"
)

type Provider interface {
	Name() domain.ProviderName
	Search(context.Context, domain.SearchRequest) (domain.SearchResponse, error)
}
