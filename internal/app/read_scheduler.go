package app

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"

	"web-search-backend/internal/domain"
)

// ContentReader reads one public URL through the existing safe read service.
type ContentReader interface {
	// Read retrieves and converts one public resource.
	Read(context.Context, domain.ReadRequest) (domain.ReadResponse, error)
}

// ReadScheduleRequest contains candidates and body-read options for one combined search.
type ReadScheduleRequest struct {
	// Candidates contains Provider results in original-rank order.
	Candidates []domain.SearchResult
	// TargetCount is the number of usable bodies requested.
	TargetCount int
	// Content configures body format and size.
	Content domain.ContentOptions
	// Refresh skips fresh read cache entries.
	Refresh bool
	// Debug enables authorized read diagnostics.
	Debug bool
	// RequestID correlates candidate reads with the combined request.
	RequestID string
}

// CandidateReadResult contains one completed candidate read in original-rank order.
type CandidateReadResult struct {
	// Candidate is the deduplicated Provider result.
	Candidate domain.SearchResult
	// OriginalRank preserves the Provider result-set rank.
	OriginalRank int
	// Response contains a successful readable body.
	Response domain.ReadResponse
	// Err contains the typed read failure.
	Err            error
	candidateIndex int
}

// CandidateScheduler reads deduplicated candidates with bounded concurrency.
type CandidateScheduler interface {
	// Schedule reads candidates and preserves original-rank order.
	Schedule(context.Context, ReadScheduleRequest) []CandidateReadResult
}

// ConcurrentReadScheduler implements bounded, rank-preserving candidate reads.
type ConcurrentReadScheduler struct {
	reader        ContentReader
	maxConcurrent int
}

// NewReadScheduler validates dependencies and creates a concurrent scheduler.
func NewReadScheduler(reader ContentReader, maxConcurrent int) (*ConcurrentReadScheduler, error) {
	if reader == nil {
		return nil, fmt.Errorf("read scheduler reader is nil")
	}
	if maxConcurrent <= 0 {
		return nil, fmt.Errorf("read scheduler concurrency must be positive")
	}
	return &ConcurrentReadScheduler{reader: reader, maxConcurrent: maxConcurrent}, nil
}

// Schedule reads unique candidates until all complete or rank-safe early success is possible.
func (scheduler *ConcurrentReadScheduler) Schedule(ctx context.Context, request ReadScheduleRequest) []CandidateReadResult {
	candidates := prepareReadCandidates(request.Candidates)
	if len(candidates) == 0 || request.TargetCount <= 0 {
		return make([]CandidateReadResult, 0)
	}
	operationCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	state := &readWorkState{candidates: candidates}
	resultChannel := make(chan CandidateReadResult, len(candidates))
	workerCount := scheduler.maxConcurrent
	if workerCount > len(candidates) {
		workerCount = len(candidates)
	}
	for worker := 0; worker < workerCount; worker++ {
		go scheduler.runReadWorker(operationCtx, request, state, resultChannel)
	}

	completed := make(map[int]bool, len(candidates))
	collected := make([]CandidateReadResult, 0, len(candidates))
	for len(completed) < len(candidates) {
		select {
		case result := <-resultChannel:
			completed[result.candidateIndex] = true
			collected = append(collected, result)
			if rankSafeTargetReached(candidates, completed, collected, request.TargetCount) {
				state.stop()
				cancel()
				return sortCandidateReads(collected)
			}
		case <-ctx.Done():
			state.stop()
			return sortCandidateReads(collected)
		}
	}
	return sortCandidateReads(collected)
}

// readWorkState assigns candidates in original-rank order and supports early stop.
type readWorkState struct {
	mu         sync.Mutex
	candidates []CandidateReadResult
	next       int
	stopped    bool
}

func (state *readWorkState) take() (CandidateReadResult, bool) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.stopped || state.next >= len(state.candidates) {
		return CandidateReadResult{}, false
	}
	candidate := state.candidates[state.next]
	state.next++
	return candidate, true
}

func (state *readWorkState) stop() {
	state.mu.Lock()
	state.stopped = true
	state.mu.Unlock()
}

func (scheduler *ConcurrentReadScheduler) runReadWorker(ctx context.Context, request ReadScheduleRequest, state *readWorkState, results chan<- CandidateReadResult) {
	for {
		candidate, ok := state.take()
		if !ok {
			return
		}
		readResponse, err := scheduler.reader.Read(ctx, domain.ReadRequest{
			URL: candidate.Candidate.URL, Format: request.Content.Format, MaxChars: request.Content.MaxChars,
			Refresh: request.Refresh, Debug: request.Debug, RequestID: request.RequestID,
		})
		candidate.Response = readResponse
		candidate.Err = err
		results <- candidate
	}
}

func prepareReadCandidates(results []domain.SearchResult) []CandidateReadResult {
	candidates := make([]CandidateReadResult, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for index, searchResult := range results {
		rank := searchResult.Rank
		if rank <= 0 {
			rank = index + 1
			searchResult.Rank = rank
		}
		key := canonicalCandidateKey(searchResult.URL)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		candidates = append(candidates, CandidateReadResult{
			Candidate: searchResult, OriginalRank: rank, candidateIndex: len(candidates),
		})
	}
	sort.SliceStable(candidates, func(first, second int) bool {
		return candidates[first].OriginalRank < candidates[second].OriginalRank
	})
	for index := range candidates {
		candidates[index].candidateIndex = index
	}
	return candidates
}

func canonicalCandidateKey(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return "invalid:" + strings.TrimSpace(rawURL)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (parsed.Scheme == "https" && port == "443") || (parsed.Scheme == "http" && port == "80") {
		port = ""
	}
	parsed.Host = hostname
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	}
	parsed.Fragment = ""
	return parsed.String()
}

func rankSafeTargetReached(candidates []CandidateReadResult, completed map[int]bool, results []CandidateReadResult, target int) bool {
	successRanks := make([]int, 0, target)
	for _, result := range results {
		if result.Err == nil {
			successRanks = append(successRanks, result.OriginalRank)
		}
	}
	if len(successRanks) < target {
		return false
	}
	sort.Ints(successRanks)
	thresholdRank := successRanks[target-1]
	for _, candidate := range candidates {
		if candidate.OriginalRank < thresholdRank && !completed[candidate.candidateIndex] {
			return false
		}
	}
	return true
}

func sortCandidateReads(results []CandidateReadResult) []CandidateReadResult {
	sorted := append([]CandidateReadResult(nil), results...)
	sort.SliceStable(sorted, func(first, second int) bool {
		return sorted[first].OriginalRank < sorted[second].OriginalRank
	})
	return sorted
}

func uniqueCandidateCount(results []domain.SearchResult) int {
	return len(prepareReadCandidates(results))
}
