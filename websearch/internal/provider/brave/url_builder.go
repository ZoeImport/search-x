package brave

import (
	"fmt"
	"net/url"
	"strings"

	"web-search-backend/websearch/internal/domain"
)

// BuildSearchURL builds a Brave Web Search result-page URL.
func BuildSearchURL(baseURL string, request domain.SearchRequest) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid Brave base URL %q", baseURL)
	}
	query := parsed.Query()
	query.Set("q", request.UpstreamQuery())
	query.Set("source", "web")
	if request.Page > 1 {
		query.Set("offset", fmt.Sprintf("%d", request.Page-1))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
