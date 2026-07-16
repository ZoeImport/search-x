package baidu

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"web-search-backend/websearch/internal/domain"
)

func ParseDesktop(body []byte, limit int) ([]domain.SearchResult, []domain.Warning, error) {
	return parse(body, limit, "#content_left", ".result, .result-op, .c-container", true, []string{
		".c-abstract",
		".content-right_8Zs40",
		".c-span-last p",
		".op_exactqa_s_answer",
		"[class*='summary-text_']",
		"[class*='summary-gap_']",
		".cos-line-clamp-3",
	})
}

func ParseMobile(body []byte, limit int) ([]domain.SearchResult, []domain.Warning, error) {
	return parse(body, limit, "#results, #page", ".result, .c-result, [data-log]", false, []string{
		".c-line-clamp3",
		".c-line-clamp2",
		".c-abstract",
		".summary",
	})
}

func parse(body []byte, limit int, rootSelector, itemSelector string, directChildren bool, snippetSelectors []string) ([]domain.SearchResult, []domain.Warning, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, nil, fmt.Errorf("parse baidu html: %w", err)
	}
	root := doc.Find(rootSelector).First()
	if root.Length() == 0 {
		return nil, nil, fmt.Errorf("normal result root not found: %s", rootSelector)
	}

	results := make([]domain.SearchResult, 0)
	warnings := make([]domain.Warning, 0)
	items := root.Find(itemSelector)
	if directChildren {
		items = root.ChildrenFiltered(itemSelector)
	}
	items.EachWithBreak(func(_ int, item *goquery.Selection) bool {
		if limit > 0 && len(results) >= limit {
			return false
		}
		anchor := item.Find("h3 a").First()
		if anchor.Length() == 0 {
			anchor = item.Find("a").First()
		}
		title := cleanText(anchor.Text())
		if title == "" {
			return true
		}
		resultURL, unresolved := extractURL(item, anchor)
		if resultURL == "" || isNonPageResult(resultURL) {
			return true
		}
		snippet := ""
		for _, selector := range snippetSelectors {
			snippet = cleanText(item.Find(selector).First().Text())
			if snippet != "" {
				break
			}
		}
		results = append(results, domain.SearchResult{
			Title:   title,
			URL:     resultURL,
			Snippet: snippet,
			Rank:    len(results) + 1,
		})
		if unresolved {
			warnings = append(warnings, domain.Warning{
				Code:    domain.WarningCodeRedirectURLUnresolved,
				Message: fmt.Sprintf("result rank %d uses a Baidu redirect URL", len(results)),
			})
		}
		return true
	})

	return results, warnings, nil
}

func extractURL(item, anchor *goquery.Selection) (string, bool) {
	for _, attr := range []string{"mu", "data-landurl", "data-url"} {
		if value, ok := item.Attr(attr); ok && validHTTPURL(value) {
			return value, false
		}
	}
	if raw, ok := item.Attr("data-log"); ok {
		var data map[string]any
		if json.Unmarshal([]byte(raw), &data) == nil {
			for _, key := range []string{"mu", "url", "target_url"} {
				if value, ok := data[key].(string); ok && validHTTPURL(value) {
					return value, false
				}
			}
		}
	}
	href, _ := anchor.Attr("href")
	if !validHTTPURL(href) {
		return "", false
	}
	return href, isBaiduRedirect(href)
}

func validHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func isBaiduRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "baidu.com" || strings.HasSuffix(host, ".baidu.com")
}

func isNonPageResult(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "nourl.ubs.baidu.com" ||
		host == "recommend_list.baidu.com" ||
		strings.HasSuffix(host, ".recommend_list.baidu.com")
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
