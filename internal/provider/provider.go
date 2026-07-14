package provider

import (
	"context"

	"web-search-backend/internal/domain"
)

type Provider interface {
	Name() domain.ProviderName
	Search(context.Context, domain.SearchRequest) (domain.SearchResponse, error)
}
