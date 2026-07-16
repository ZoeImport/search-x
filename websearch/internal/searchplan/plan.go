package searchplan

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"web-search-backend/websearch/internal/domain"
)

const maxFilterValues = 20

var (
	domainLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	fileTypePattern    = regexp.MustCompile(`^[a-z0-9]{1,10}$`)
	trackingParameters = map[string]struct{}{
		"fbclid": {}, "gclid": {}, "dclid": {}, "msclkid": {}, "mc_cid": {}, "mc_eid": {},
	}
)

// Normalize validates and canonicalizes provider-neutral advanced search fields.
func Normalize(request domain.SearchRequest) (domain.SearchRequest, error) {
	var err error
	request.Filters.IncludeDomains, err = normalizeDomains(request.Filters.IncludeDomains)
	if err != nil {
		return domain.SearchRequest{}, fmt.Errorf("include_domains: %w", err)
	}
	request.Filters.ExcludeDomains, err = normalizeDomains(request.Filters.ExcludeDomains)
	if err != nil {
		return domain.SearchRequest{}, fmt.Errorf("exclude_domains: %w", err)
	}
	excluded := make(map[string]struct{}, len(request.Filters.ExcludeDomains))
	for _, value := range request.Filters.ExcludeDomains {
		excluded[value] = struct{}{}
	}
	for _, value := range request.Filters.IncludeDomains {
		if _, exists := excluded[value]; exists {
			return domain.SearchRequest{}, fmt.Errorf("domain %q appears in both include_domains and exclude_domains", value)
		}
	}
	request.QueryOptions.ExactPhrases, err = normalizeTextValues(request.QueryOptions.ExactPhrases, "exact_phrases")
	if err != nil {
		return domain.SearchRequest{}, err
	}
	request.QueryOptions.AnyTerms, err = normalizeTextValues(request.QueryOptions.AnyTerms, "any_terms")
	if err != nil {
		return domain.SearchRequest{}, err
	}
	request.QueryOptions.ExcludeTerms, err = normalizeTextValues(request.QueryOptions.ExcludeTerms, "exclude_terms")
	if err != nil {
		return domain.SearchRequest{}, err
	}
	request.QueryOptions.TitleTerms, err = normalizeTextValues(request.QueryOptions.TitleTerms, "title_terms")
	if err != nil {
		return domain.SearchRequest{}, err
	}
	request.QueryOptions.FileTypes, err = normalizeFileTypes(request.QueryOptions.FileTypes)
	if err != nil {
		return domain.SearchRequest{}, err
	}
	return request, nil
}

// CompatibleProviders preserves caller order while removing providers that
// cannot honor the requested structured query options under strict semantics.
func CompatibleProviders(request domain.SearchRequest, providers []domain.ProviderName) []domain.ProviderName {
	if !hasQueryOptions(request.QueryOptions) {
		return append([]domain.ProviderName(nil), providers...)
	}
	result := make([]domain.ProviderName, 0, len(providers))
	for _, provider := range providers {
		if compilerFor(provider).Supports(request.QueryOptions) {
			result = append(result, provider)
		}
	}
	return result
}

func HasQueryOptions(request domain.SearchRequest) bool {
	return hasQueryOptions(request.QueryOptions)
}

// Compile creates a provider-specific query string without changing the
// provider-neutral query returned to clients or used in request fingerprints.
func Compile(request domain.SearchRequest, provider domain.ProviderName) (string, error) {
	compiler := compilerFor(provider)
	if !compiler.Supports(request.QueryOptions) {
		return "", fmt.Errorf("provider %s does not support the requested query options", provider)
	}
	return compiler.Compile(request), nil
}

// Finalize enforces domain filters, canonicalizes URLs, removes duplicates,
// and assigns ranks after filtering.
func Finalize(request domain.SearchRequest, results []domain.SearchResult) []domain.SearchResult {
	finalized := make([]domain.SearchResult, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		canonicalURL, resultDomain, err := CanonicalizeURL(result.URL)
		if err != nil || !domainAllowed(resultDomain, request.Filters) {
			continue
		}
		if _, exists := seen[canonicalURL]; exists {
			continue
		}
		seen[canonicalURL] = struct{}{}
		result.CanonicalURL = canonicalURL
		result.Domain = resultDomain
		result.Rank = len(finalized) + 1
		finalized = append(finalized, result)
		if len(finalized) >= request.Limit {
			break
		}
	}
	return finalized
}

// Fingerprint hashes every public request field that can affect result content.
// Provider is intentionally excluded because cursor pagination pins it separately.
func Fingerprint(request domain.SearchRequest) string {
	if normalized, err := Normalize(request); err == nil {
		request = normalized
	}
	providers := make([]string, len(request.Providers))
	for index, provider := range request.Providers {
		providers[index] = string(provider)
	}
	value := struct {
		Query        string                    `json:"query"`
		Providers    []string                  `json:"providers"`
		Limit        int                       `json:"limit"`
		Region       string                    `json:"region"`
		Filters      domain.SearchFilters      `json:"filters"`
		QueryOptions domain.SearchQueryOptions `json:"query_options"`
	}{
		Query: request.Query, Providers: providers, Limit: request.Limit,
		Region: request.Region, Filters: request.Filters, QueryOptions: request.QueryOptions,
	}
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest[:])
}

// CanonicalizeURL creates the identity used for deduplication while retaining
// the original result URL as the navigation target.
func CanonicalizeURL(raw string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", "", fmt.Errorf("invalid result URL %q", raw)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if hostname == "" {
		return "", "", fmt.Errorf("result URL host is empty")
	}
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	parsed.Host = hostname
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	}
	parsed.Fragment = ""
	if parsed.Path == "" {
		parsed.Path = "/"
	} else {
		cleaned := path.Clean(parsed.Path)
		if strings.HasSuffix(parsed.Path, "/") && cleaned != "/" {
			cleaned += "/"
		}
		parsed.Path = cleaned
	}
	query := parsed.Query()
	for key := range query {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") {
			query.Del(key)
			continue
		}
		if _, drop := trackingParameters[lower]; drop {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), hostname, nil
}

func normalizeDomains(values []string) ([]string, error) {
	if len(values) > maxFilterValues {
		return nil, fmt.Errorf("must contain at most %d values", maxFilterValues)
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		value = strings.TrimSuffix(value, ".")
		if value == "" || strings.ContainsAny(value, "/:@?#") || net.ParseIP(value) != nil {
			return nil, fmt.Errorf("%q is not a valid domain", raw)
		}
		labels := strings.Split(value, ".")
		if len(labels) < 2 {
			return nil, fmt.Errorf("%q must contain a public suffix", raw)
		}
		for _, label := range labels {
			if !domainLabelPattern.MatchString(label) {
				return nil, fmt.Errorf("%q is not a valid domain", raw)
			}
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func normalizeTextValues(values []string, field string) ([]string, error) {
	if len(values) > maxFilterValues {
		return nil, fmt.Errorf("%s must contain at most %d values", field, maxFilterValues)
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.Join(strings.Fields(raw), " ")
		if value == "" || len([]rune(value)) > 100 {
			return nil, fmt.Errorf("%s values must contain between 1 and 100 characters", field)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func normalizeFileTypes(values []string) ([]string, error) {
	if len(values) > 10 {
		return nil, fmt.Errorf("file_types must contain at most 10 values")
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "."))
		if !fileTypePattern.MatchString(value) {
			return nil, fmt.Errorf("invalid file type %q", raw)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func hasQueryOptions(options domain.SearchQueryOptions) bool {
	return len(options.ExactPhrases)+len(options.AnyTerms)+len(options.ExcludeTerms)+len(options.TitleTerms)+len(options.FileTypes) > 0
}

func domainAllowed(host string, filters domain.SearchFilters) bool {
	for _, excluded := range filters.ExcludeDomains {
		if hostMatches(host, excluded) {
			return false
		}
	}
	if len(filters.IncludeDomains) == 0 {
		return true
	}
	for _, included := range filters.IncludeDomains {
		if hostMatches(host, included) {
			return true
		}
	}
	return false
}

func hostMatches(host, filter string) bool {
	return host == filter || strings.HasSuffix(host, "."+filter)
}

func quote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func quoteWhenNeeded(value string) string {
	if strings.ContainsAny(value, " \t\"") {
		return quote(value)
	}
	return value
}
