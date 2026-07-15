package bing

import (
	"fmt"
	"net/url"
	"strconv"

	"web-search-backend/internal/domain"
)

// regionMarketCodes maps a lower-case ISO 3166-1 alpha-2 country code to the
// Bing "mkt" market code for that region's dominant language. Only regions
// verified live to change actual Bing result content are listed; unlisted
// regions are treated as unsupported rather than guessed at.
var regionMarketCodes = map[string]string{
	"cn": "zh-CN",
	"us": "en-US",
	"jp": "ja-JP",
	"hk": "zh-HK",
	"tw": "zh-TW",
	"gb": "en-GB",
	"de": "de-DE",
	"fr": "fr-FR",
	"kr": "ko-KR",
}

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
	if mkt, ok := regionMarketCodes[request.Region]; ok {
		values.Set("mkt", mkt)
	}
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}
