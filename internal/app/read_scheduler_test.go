package app

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"web-search-backend/internal/domain"
)

type scheduledReaderStub struct {
	mu        sync.Mutex
	delays    map[string]time.Duration
	failures  map[string]error
	calls     map[string]int
	active    int
	maxActive int
}

func (stub *scheduledReaderStub) Read(ctx context.Context, request domain.ReadRequest) (domain.ReadResponse, error) {
	stub.mu.Lock()
	stub.active++
	if stub.active > stub.maxActive {
		stub.maxActive = stub.active
	}
	if stub.calls == nil {
		stub.calls = make(map[string]int)
	}
	stub.calls[request.URL]++
	stub.mu.Unlock()
	defer func() {
		stub.mu.Lock()
		stub.active--
		stub.mu.Unlock()
	}()
	if delay := stub.delays[request.URL]; delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return domain.ReadResponse{}, ctx.Err()
		}
	}
	if err := stub.failures[request.URL]; err != nil {
		return domain.ReadResponse{}, err
	}
	return domain.ReadResponse{
		URL: request.URL, FinalURL: request.URL, Title: "article", Content: "body " + request.URL,
		ContentFormat: request.Format, ContentLength: 20,
		Meta: domain.ReadMeta{Transport: domain.ReadTransportHTTP}, Warnings: make([]domain.ReadWarning, 0),
	}, nil
}

func TestReadSchedulerPreservesOriginalOrderAcrossReverseCompletion(t *testing.T) {
	reader := &scheduledReaderStub{delays: map[string]time.Duration{
		"https://example.com/1": 30 * time.Millisecond,
		"https://example.com/2": 20 * time.Millisecond,
		"https://example.com/3": 10 * time.Millisecond,
	}}
	scheduler, err := NewReadScheduler(reader, 3)
	if err != nil {
		t.Fatal(err)
	}
	results := scheduler.Schedule(context.Background(), ReadScheduleRequest{
		Candidates: rankedCandidates(3), TargetCount: 3,
		Content: domain.ContentOptions{Enabled: true, Format: domain.OutputFormatMarkdown, MaxChars: domain.DefaultReadMaxChars},
	})
	if len(results) != 3 {
		t.Fatalf("results=%+v", results)
	}
	for index, result := range results {
		if result.OriginalRank != index+1 {
			t.Fatalf("results=%+v", results)
		}
	}
}

func TestReadSchedulerDeduplicatesCanonicalURLs(t *testing.T) {
	reader := &scheduledReaderStub{}
	scheduler, err := NewReadScheduler(reader, 2)
	if err != nil {
		t.Fatal(err)
	}
	results := scheduler.Schedule(context.Background(), ReadScheduleRequest{
		Candidates: []domain.SearchResult{
			{Rank: 1, URL: "https://EXAMPLE.com:443/article#top"},
			{Rank: 2, URL: "https://example.com/article"},
		},
		TargetCount: 2, Content: domain.ContentOptions{Enabled: true, Format: domain.OutputFormatMarkdown, MaxChars: domain.DefaultReadMaxChars},
	})
	if len(results) != 1 || results[0].OriginalRank != 1 {
		t.Fatalf("results=%+v", results)
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if len(reader.calls) != 1 {
		t.Fatalf("calls=%v", reader.calls)
	}
}

func TestReadSchedulerBoundsConcurrency(t *testing.T) {
	reader := &scheduledReaderStub{delays: map[string]time.Duration{}}
	for rank := 1; rank <= 6; rank++ {
		reader.delays[candidateURL(rank)] = 10 * time.Millisecond
	}
	scheduler, err := NewReadScheduler(reader, 2)
	if err != nil {
		t.Fatal(err)
	}
	_ = scheduler.Schedule(context.Background(), ReadScheduleRequest{
		Candidates: rankedCandidates(6), TargetCount: 6,
		Content: domain.ContentOptions{Enabled: true, Format: domain.OutputFormatMarkdown, MaxChars: domain.DefaultReadMaxChars},
	})
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.maxActive > 2 {
		t.Fatalf("max active reads = %d", reader.maxActive)
	}
}

func rankedCandidates(count int) []domain.SearchResult {
	results := make([]domain.SearchResult, count)
	for index := range results {
		rank := index + 1
		results[index] = domain.SearchResult{
			Rank: rank, Title: "result", URL: candidateURL(rank), Provider: domain.ProviderNameDuckDuckGo,
		}
	}
	return results
}

func candidateURL(rank int) string {
	return "https://example.com/" + strconv.Itoa(rank)
}

func readFailure(message string) error {
	return &domain.ReadError{Code: domain.ErrFetchFailed, Message: "read failed", Retryable: true, Original: errors.New(message)}
}
