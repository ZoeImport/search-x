package bing

import (
	"fmt"
	"net/url"
	"strconv"

	"web-search-backend/internal/domain"
)

// BuildSearchURL builds a direct Bing search URL for a normalized request.
func BuildSearchURL(baseURL string, request domain.SearchRequest) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("invalid Bing browser base URL %q", baseURL)
	}
	values := parsed.Query()
	values.Set("q", request.Query)
	values.Set("count", strconv.Itoa(request.Limit))
	values.Set("first", strconv.Itoa((request.Page-1)*request.Limit+1))
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}
