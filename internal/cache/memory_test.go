package cache

import (
	"context"
	"testing"
	"time"

	"web-search-backend/internal/domain"
)

func TestMemoryFreshAndStale(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	store, err := NewMemory(100, 15*time.Minute, 24*time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.Set(ctx, "baidu|golang|1|10", domain.SearchResponse{Query: "golang"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.GetFresh(ctx, "baidu|golang|1|10"); !ok {
		t.Fatal("expected fresh")
	}
	now = now.Add(16 * time.Minute)
	if _, ok := store.GetFresh(ctx, "baidu|golang|1|10"); ok {
		t.Fatal("expected not fresh")
	}
	if _, ok := store.GetStale(ctx, "baidu|golang|1|10"); !ok {
		t.Fatal("expected stale")
	}
	now = now.Add(24 * time.Hour)
	if _, ok := store.GetStale(ctx, "baidu|golang|1|10"); ok {
		t.Fatal("expected expired")
	}
}

func TestMemoryReturnsIndependentCopies(t *testing.T) {
	store, err := NewMemory(100, time.Hour, 24*time.Hour, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	original := domain.SearchResponse{Results: []domain.SearchResult{{Title: "original"}}}
	if err := store.Set(ctx, "k", original); err != nil {
		t.Fatal(err)
	}
	first, _ := store.GetFresh(ctx, "k")
	first.Results[0].Title = "mutated"
	second, _ := store.GetFresh(ctx, "k")
	if second.Results[0].Title != "original" {
		t.Fatalf("cached value mutated: %#v", second)
	}
}

func TestMemoryEvictsOldestEntry(t *testing.T) {
	now := time.Now()
	store, err := NewMemory(1, time.Hour, 24*time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = store.Set(ctx, "old", domain.SearchResponse{})
	now = now.Add(time.Second)
	_ = store.Set(ctx, "new", domain.SearchResponse{})
	if _, ok := store.GetStale(ctx, "old"); ok {
		t.Fatal("old entry should be evicted")
	}
}
