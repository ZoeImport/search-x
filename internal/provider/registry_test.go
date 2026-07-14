package provider_test

import (
	"context"
	"testing"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/provider"
)

type stubProvider struct {
	name domain.ProviderName
}

func (s stubProvider) Name() domain.ProviderName { return s.name }

func (s stubProvider) Search(context.Context, domain.SearchRequest) (domain.SearchResponse, error) {
	return domain.SearchResponse{Provider: s.name}, nil
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := provider.NewRegistry()
	if err := r.Register(stubProvider{name: domain.ProviderNameBaidu}); err != nil {
		t.Fatal(err)
	}
	p, ok := r.Get(domain.ProviderNameBaidu)
	if !ok || p.Name() != domain.ProviderNameBaidu {
		t.Fatalf("unexpected provider: %#v %v", p, ok)
	}
}

func TestRegistryRejectsDuplicate(t *testing.T) {
	r := provider.NewRegistry()
	if err := r.Register(stubProvider{name: domain.ProviderNameBaidu}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(stubProvider{name: domain.ProviderNameBaidu}); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestRegistryRejectsNilAndEmptyName(t *testing.T) {
	r := provider.NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Fatal("expected nil provider error")
	}
	if err := r.Register(stubProvider{}); err == nil {
		t.Fatal("expected empty provider name error")
	}
}
