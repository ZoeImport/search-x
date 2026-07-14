// Package searchquality evaluates query relevance and whole result-set quality.
package searchquality

import (
	"net"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/net/publicsuffix"

	"web-search-backend/internal/domain"
)

// Config contains immutable scoring weights and penalties.
type Config struct {
	// TitleWeight weights query coverage in the result title.
	TitleWeight float64
	// SnippetWeight weights query coverage in the result snippet.
	SnippetWeight float64
	// URLWeight weights query coverage in the result URL.
	URLWeight float64
	// PhraseWeight rewards an intact normalized query phrase.
	PhraseWeight float64
	// TopWeight weights the mean score of the first five results.
	TopWeight float64
	// DiversityWeight weights unique registrable-domain coverage.
	DiversityWeight float64
	// CompletenessWeight weights title, URL, and snippet completeness.
	CompletenessWeight float64
	// EmptyTitlePenalty penalizes a missing title.
	EmptyTitlePenalty float64
	// EmptyURLPenalty penalizes a missing or invalid URL.
	EmptyURLPenalty float64
	// EmptySnippetPenalty penalizes a missing snippet.
	EmptySnippetPenalty float64
	// DuplicateURLPenalty penalizes an already observed canonical URL.
	DuplicateURLPenalty float64
	// AdvertisementPenalty penalizes obvious advertisement markers.
	AdvertisementPenalty float64
}

// DefaultConfig returns the approved parity-plus scoring configuration.
func DefaultConfig() Config {
	return Config{
		TitleWeight: 0.55, SnippetWeight: 0.30, URLWeight: 0.05, PhraseWeight: 0.10,
		TopWeight: 0.70, DiversityWeight: 0.20, CompletenessWeight: 0.10,
		EmptyTitlePenalty: 0.25, EmptyURLPenalty: 0.50, EmptySnippetPenalty: 0.05,
		DuplicateURLPenalty: 0.20, AdvertisementPenalty: 0.15,
	}
}

// ItemScore contains the local relevance score for one original result rank.
type ItemScore struct {
	// OriginalRank is the Provider rank associated with Score.
	OriginalRank int
	// Score is the clamped item relevance score.
	Score float64
}

// Result contains per-item scores and whole result-set quality metrics.
type Result struct {
	// Score is the clamped whole result-set quality score.
	Score float64
	// Items preserves the input order and per-item scores.
	Items []ItemScore
	// UniqueDomainRatio is distinct registrable domains divided by result count.
	UniqueDomainRatio float64
	// UniqueDomainCount is the distinct registrable-domain count.
	UniqueDomainCount int
	// Completeness is populated title, URL, and snippet fields divided by all expected fields.
	Completeness float64
}

// QualityEvaluator scores one Provider result set without invoking transports.
type QualityEvaluator interface {
	// Name identifies the concrete scoring implementation.
	Name() domain.ImplementationName
	// Evaluate scores query relevance and result-set quality.
	Evaluate(query string, results []domain.SearchResult) Result
}

// Evaluator implements deterministic Unicode-aware result-set scoring.
type Evaluator struct {
	config Config
}

// NewEvaluator creates a Unicode-aware scorer using config.
func NewEvaluator(config Config) *Evaluator {
	return &Evaluator{config: config}
}

// Name identifies the Unicode-aware scoring implementation.
func (*Evaluator) Name() domain.ImplementationName {
	return domain.ImplementationNameUnicodeSearchQuality
}

// Evaluate scores query relevance, diversity, and field completeness.
func (evaluator *Evaluator) Evaluate(query string, results []domain.SearchResult) Result {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 || len(results) == 0 {
		return Result{Items: make([]ItemScore, 0)}
	}

	itemScores := make([]ItemScore, 0, len(results))
	seenURLs := make(map[string]struct{}, len(results))
	uniqueDomains := make(map[string]struct{}, len(results))
	completeFields := 0
	queryPhrase := compactLettersAndDigits(query)
	for index, searchResult := range results {
		rank := searchResult.Rank
		if rank <= 0 {
			rank = index + 1
		}
		score := evaluator.config.TitleWeight*coverage(queryTokens, searchResult.Title) +
			evaluator.config.SnippetWeight*coverage(queryTokens, searchResult.Snippet) +
			evaluator.config.URLWeight*coverage(queryTokens, searchResult.URL)
		if queryPhrase != "" && strings.Contains(compactLettersAndDigits(searchResult.Title+" "+searchResult.Snippet), queryPhrase) {
			score += evaluator.config.PhraseWeight
		}
		if strings.TrimSpace(searchResult.Title) == "" {
			score -= evaluator.config.EmptyTitlePenalty
		} else {
			completeFields++
		}

		canonicalURL, hostname := normalizeURL(searchResult.URL)
		if canonicalURL == "" {
			score -= evaluator.config.EmptyURLPenalty
		} else {
			completeFields++
			if _, exists := seenURLs[canonicalURL]; exists {
				score -= evaluator.config.DuplicateURLPenalty
			} else {
				seenURLs[canonicalURL] = struct{}{}
			}
			if domainName := registrableDomain(hostname); domainName != "" {
				uniqueDomains[domainName] = struct{}{}
			}
		}
		if strings.TrimSpace(searchResult.Snippet) == "" {
			score -= evaluator.config.EmptySnippetPenalty
		} else {
			completeFields++
		}
		if containsAdvertisementMarker(searchResult.Title, searchResult.Snippet) {
			score -= evaluator.config.AdvertisementPenalty
		}
		itemScores = append(itemScores, ItemScore{OriginalRank: rank, Score: clamp(score)})
	}

	topCount := len(itemScores)
	if topCount > 5 {
		topCount = 5
	}
	topTotal := 0.0
	for index := 0; index < topCount; index++ {
		topTotal += itemScores[index].Score
	}
	uniqueDomainRatio := float64(len(uniqueDomains)) / float64(len(results))
	completeness := float64(completeFields) / float64(len(results)*3)
	providerScore := evaluator.config.TopWeight*(topTotal/float64(topCount)) +
		evaluator.config.DiversityWeight*uniqueDomainRatio +
		evaluator.config.CompletenessWeight*completeness
	return Result{
		Score: clamp(providerScore), Items: itemScores,
		UniqueDomainRatio: uniqueDomainRatio, UniqueDomainCount: len(uniqueDomains), Completeness: completeness,
	}
}

func coverage(queryTokens []string, value string) float64 {
	if len(queryTokens) == 0 || strings.TrimSpace(value) == "" {
		return 0
	}
	compactValue := compactLettersAndDigits(value)
	matched := 0
	for _, token := range queryTokens {
		if strings.Contains(compactValue, compactLettersAndDigits(token)) {
			matched++
		}
	}
	return float64(matched) / float64(len(queryTokens))
}

func tokenize(value string) []string {
	value = strings.ToLower(value)
	tokenSet := make(map[string]struct{})
	runes := []rune(value)
	for start := 0; start < len(runes); {
		if isCJK(runes[start]) {
			end := start + 1
			for end < len(runes) && isCJK(runes[end]) {
				end++
			}
			addCJKTokens(tokenSet, runes[start:end])
			start = end
			continue
		}
		if unicode.IsLetter(runes[start]) || unicode.IsDigit(runes[start]) {
			end := start + 1
			for end < len(runes) && !isCJK(runes[end]) && (unicode.IsLetter(runes[end]) || unicode.IsDigit(runes[end])) {
				end++
			}
			tokenSet[string(runes[start:end])] = struct{}{}
			start = end
			continue
		}
		start++
	}
	tokens := make([]string, 0, len(tokenSet))
	for token := range tokenSet {
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	sort.Strings(tokens)
	return tokens
}

func addCJKTokens(tokens map[string]struct{}, runes []rune) {
	if len(runes) == 0 {
		return
	}
	tokens[string(runes)] = struct{}{}
	for index, current := range runes {
		tokens[string(current)] = struct{}{}
		if index+1 < len(runes) {
			tokens[string(runes[index:index+2])] = struct{}{}
		}
	}
}

func isCJK(value rune) bool {
	return unicode.In(value, unicode.Han)
}

func compactLettersAndDigits(value string) string {
	var builder strings.Builder
	for _, current := range strings.ToLower(value) {
		if unicode.IsLetter(current) || unicode.IsDigit(current) {
			builder.WriteRune(current)
		}
	}
	return builder.String()
}

func normalizeURL(rawURL string) (string, string) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return "", ""
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
	return parsed.String(), hostname
}

func registrableDomain(hostname string) string {
	if hostname == "" {
		return ""
	}
	domainName, err := publicsuffix.EffectiveTLDPlusOne(hostname)
	if err == nil {
		return domainName
	}
	return hostname
}

func containsAdvertisementMarker(values ...string) bool {
	combined := strings.ToLower(strings.Join(values, " "))
	for _, marker := range []string{"广告", "推广", "sponsored", "advertisement"} {
		if strings.Contains(combined, marker) {
			return true
		}
	}
	return false
}

func clamp(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
