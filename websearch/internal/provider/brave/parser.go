package brave

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"web-search-backend/websearch/internal/domain"
)

var resultSelectors = []string{"[data-type=\"web\"]", ".result", ".fdb"}

var titleSelectors = []string{".title a", "h2 a", ".result-title a", ".snippet-title a", "a[href*=\"://\"]"}

var snippetSelectors = []string{".snippet-content", ".snippet", ".description", "p"}

// Parse extracts normalized results from a Brave HTML response.
func Parse(body []byte, limit int) ([]domain.SearchResult, error) {
	document, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parse Brave HTML: %w", err)
	}
	root := document.Find("#results, main").First()
	if root.Length() == 0 {
		return nil, fmt.Errorf("Brave result root not found")
	}
	items := firstResultSet(root)
	results := make([]domain.SearchResult, 0)
	items.EachWithBreak(func(_ int, item *goquery.Selection) bool {
		if limit > 0 && len(results) >= limit {
			return false
		}
		title, rawURL := braveTitle(item)
		if title == "" || !validBraveURL(rawURL) {
			return true
		}
		results = append(results, domain.SearchResult{
			Title: title, URL: strings.TrimSpace(rawURL), Snippet: braveSnippet(item), Rank: len(results) + 1,
		})
		return true
	})
	return results, nil
}

func firstResultSet(root *goquery.Selection) *goquery.Selection {
	for _, selector := range resultSelectors {
		if items := root.Find(selector); items.Length() > 0 {
			return items
		}
	}
	return root.Find("[data-brave-no-results]")
}

func braveTitle(item *goquery.Selection) (string, string) {
	for _, selector := range titleSelectors {
		anchor := item.Find(selector).First()
		if anchor.Length() == 0 {
			continue
		}
		title := cleanBraveText(anchor.Text())
		rawURL, exists := anchor.Attr("href")
		if title != "" && exists && validBraveURL(rawURL) {
			return title, rawURL
		}
	}
	return "", ""
}

func braveSnippet(item *goquery.Selection) string {
	for _, selector := range snippetSelectors {
		if snippet := cleanBraveText(item.Find(selector).First().Text()); snippet != "" {
			return snippet
		}
	}
	return ""
}

func validBraveURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func cleanBraveText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
