package baidu

import (
	"fmt"
	"net/url"
	"strconv"

	"web-search-backend/internal/domain"
)

// BuildSearchURL builds a Baidu desktop search URL for a normalized request.
func BuildSearchURL(baseURL string, request domain.SearchRequest) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("invalid Baidu browser base URL %q", baseURL)
	}
	values := parsed.Query()
	values.Set("wd", request.Query)
	values.Set("rn", strconv.Itoa(request.Limit))
	values.Set("pn", strconv.Itoa((request.Page-1)*request.Limit))
	values.Set("ie", "utf-8")
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}
