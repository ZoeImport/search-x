package provider

import (
	"context"

	"web-search-backend/internal/domain"
)

type Provider interface {
	Name() string
	Search(context.Context, domain.SearchRequest) (domain.SearchResponse, error)
}
