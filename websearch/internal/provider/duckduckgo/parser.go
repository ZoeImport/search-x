package duckduckgo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"web-search-backend/websearch/internal/domain"
)

// Parse extracts normalized search results from a DuckDuckGo HTML response.
func Parse(body []byte, limit int) ([]domain.SearchResult, error) {
	results, _, err := ParsePage(body, limit)
	return results, err
}

// ParsePage extracts results and the opaque values from DuckDuckGo's real
// Next form. The token is encrypted by the public API cursor before leaving
// the service.
func ParsePage(body []byte, limit int) ([]domain.SearchResult, string, error) {
	document, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, "", fmt.Errorf("parse DuckDuckGo HTML: %w", err)
	}
	root := document.Find("#links, .results").First()
	if root.Length() == 0 {
		return nil, "", fmt.Errorf("DuckDuckGo result root not found")
	}
	results := make([]domain.SearchResult, 0)
	root.Find(".result").EachWithBreak(func(_ int, item *goquery.Selection) bool {
		if limit > 0 && len(results) >= limit {
			return false
		}
		anchor := item.Find(".result__a").First()
		title := cleanText(anchor.Text())
		href, exists := anchor.Attr("href")
		if title == "" || !exists {
			return true
		}
		targetURL, resolveErr := resolveResultURL(href)
		if resolveErr != nil {
			return true
		}
		results = append(results, domain.SearchResult{
			Title:   title,
			URL:     targetURL,
			Snippet: cleanText(item.Find(".result__snippet").First().Text()),
			Rank:    len(results) + 1,
		})
		return true
	})
	nextToken, err := encodeNextForm(root)
	if err != nil {
		return nil, "", err
	}
	return results, nextToken, nil
}

func encodeNextForm(root *goquery.Selection) (string, error) {
	form := root.Find(".nav-link form").First()
	if form.Length() == 0 {
		return "", nil
	}
	values := make(url.Values)
	allowed := map[string]struct{}{
		"q": {}, "s": {}, "nextParams": {}, "v": {}, "o": {},
		"dc": {}, "api": {}, "vqd": {}, "kl": {}, "df": {},
	}
	form.Find("input[name]").Each(func(_ int, input *goquery.Selection) {
		name, _ := input.Attr("name")
		if _, ok := allowed[name]; !ok {
			return
		}
		value, _ := input.Attr("value")
		values.Add(name, value)
	})
	if values.Get("q") == "" || values.Get("s") == "" {
		return "", fmt.Errorf("DuckDuckGo next form is incomplete")
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("encode DuckDuckGo next form: %w", err)
	}
	return string(encoded), nil
}

func resolveResultURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if strings.HasPrefix(rawURL, "//") {
		rawURL = "https:" + rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse result URL: %w", err)
	}
	if strings.EqualFold(parsed.Hostname(), "duckduckgo.com") || strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".duckduckgo.com") {
		if redirectTarget := strings.TrimSpace(parsed.Query().Get("uddg")); redirectTarget != "" {
			parsed, err = url.Parse(redirectTarget)
			if err != nil {
				return "", fmt.Errorf("parse redirect target: %w", err)
			}
		}
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("unsupported result URL %q", rawURL)
	}
	return parsed.String(), nil
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
