package cache

import (
	"context"
	"testing"
	"time"

	"web-search-backend/webfetch/internal/domain"
)

func TestMemorySeparatesFreshAndStaleWindows(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	store, err := NewMemory(2, time.Minute, time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	document := domain.ReadDocument{URL: "https://example.com", ContentText: "body"}
	if err := store.Set(context.Background(), "example", document); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.GetFresh(context.Background(), "example"); !ok {
		t.Fatal("fresh entry missing")
	}
	now = now.Add(2 * time.Minute)
	if _, ok := store.GetFresh(context.Background(), "example"); ok {
		t.Fatal("expired fresh entry returned")
	}
	stale, ok := store.GetStale(context.Background(), "example")
	if !ok || stale.StoredAt.IsZero() {
		t.Fatalf("stale=%#v ok=%v", stale, ok)
	}
}

func TestMemoryEvictsOldestEntry(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	store, err := NewMemory(1, time.Minute, time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), "first", domain.ReadDocument{URL: "https://first.example"}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if err := store.Set(context.Background(), "second", domain.ReadDocument{URL: "https://second.example"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.GetStale(context.Background(), "first"); ok {
		t.Fatal("oldest entry was not evicted")
	}
}
