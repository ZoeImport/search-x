package profilepool

import (
	"context"
	"fmt"
	"sync"

	"web-search-backend/internal/domain"
)

// Searcher is the existing provider behavior wrapped by one profile resource.
type Searcher interface {
	Name() domain.ProviderName
	Search(context.Context, domain.SearchRequest) (domain.SearchResponse, error)
}

// ProviderProfile adapts one provider-specific searcher into a managed profile.
type ProviderProfile struct {
	id       string
	capacity int
	searcher Searcher
	close    func() error
	once     sync.Once
	closeErr error
}

// NewProviderProfile validates and creates a provider-backed profile.
func NewProviderProfile(id string, capacity int, searcher Searcher, closeFn func() error) (*ProviderProfile, error) {
	if id == "" || capacity <= 0 || searcher == nil || searcher.Name() == "" || searcher.Name() == domain.ProviderNameAuto {
		return nil, fmt.Errorf("invalid provider profile id=%q capacity=%d", id, capacity)
	}
	return &ProviderProfile{id: id, capacity: capacity, searcher: searcher, close: closeFn}, nil
}

func (profile *ProviderProfile) ID() string                    { return profile.id }
func (profile *ProviderProfile) Provider() domain.ProviderName { return profile.searcher.Name() }
func (profile *ProviderProfile) Capacity() int                 { return profile.capacity }
func (profile *ProviderProfile) Search(ctx context.Context, request domain.SearchRequest) (domain.SearchResponse, error) {
	return profile.searcher.Search(ctx, request)
}
func (profile *ProviderProfile) Close() error {
	profile.once.Do(func() {
		if profile.close != nil {
			profile.closeErr = profile.close()
		}
	})
	return profile.closeErr
}
