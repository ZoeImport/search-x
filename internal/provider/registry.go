package provider

import (
	"fmt"
	"strings"
	"sync"

	"web-search-backend/internal/domain"
)

type Registry struct {
	mu        sync.RWMutex
	providers map[domain.ProviderName]Provider
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[domain.ProviderName]Provider)}
}

func (r *Registry) Register(p Provider) error {
	if p == nil {
		return fmt.Errorf("provider is nil")
	}
	name := normalizeName(p.Name())
	if name == "" {
		return fmt.Errorf("provider name is empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("provider %q already registered", name)
	}
	r.providers[name] = p
	return nil
}

func (r *Registry) Get(name domain.ProviderName) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[normalizeName(name)]
	return p, ok
}

func normalizeName(name domain.ProviderName) domain.ProviderName {
	return domain.ProviderName(strings.ToLower(strings.TrimSpace(string(name))))
}
