package bing

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"web-search-backend/internal/domain"
)

// Parse extracts normalized results from a Bing HTML response.
func Parse(body []byte, limit int) ([]domain.SearchResult, error) {
	document, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parse Bing HTML: %w", err)
	}
	root := document.Find("#b_results, .b_results").First()
	if root.Length() == 0 {
		return nil, fmt.Errorf("Bing result root not found")
	}
	results := make([]domain.SearchResult, 0)
	root.Find(".b_algo").EachWithBreak(func(_ int, item *goquery.Selection) bool {
		if limit > 0 && len(results) >= limit {
			return false
		}
		anchor := item.Find("h2 a").First()
		title := cleanText(anchor.Text())
		href, exists := anchor.Attr("href")
		if title == "" || !exists || !validHTTPURL(href) {
			return true
		}
		snippet := cleanText(item.Find(".b_caption p").First().Text())
		if snippet == "" {
			snippet = cleanText(item.Find("p").First().Text())
		}
		results = append(results, domain.SearchResult{
			Title: title, URL: href, Snippet: snippet, Rank: len(results) + 1,
		})
		return true
	})
	return results, nil
}

func validHTTPURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
