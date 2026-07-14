package extractor

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"web-search-backend/internal/domain"
)

// HTMLExtractor extracts article-like HTML and metadata into a canonical document.
type HTMLExtractor struct{}

// Name returns the stable HTML extractor implementation name.
func (HTMLExtractor) Name() domain.ImplementationName {
	return domain.ImplementationNameHTMLExtractor
}

// SourceTypes returns the source types handled by this extractor.
func (HTMLExtractor) SourceTypes() []domain.SourceType {
	return []domain.SourceType{domain.SourceTypeHTML}
}

// Extract delegates article selection to Readability, then applies the service sanitizer.
func (HTMLExtractor) Extract(ctx context.Context, resource domain.Resource) (domain.ReadDocument, error) {
	if err := ctx.Err(); err != nil {
		return domain.ReadDocument{}, err
	}
	if len(resource.Body) == 0 {
		return domain.ReadDocument{}, fmt.Errorf("extract HTML: empty body")
	}
	baseURL := resource.FinalURL
	if baseURL == "" {
		baseURL = resource.URL
	}
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil || parsedBaseURL.Scheme == "" || parsedBaseURL.Host == "" {
		parsedBaseURL, _ = url.Parse("https://invalid.local/")
	}
	article, err := readability.FromReader(bytes.NewReader(resource.Body), parsedBaseURL)
	if err != nil {
		return domain.ReadDocument{}, fmt.Errorf("extract HTML with readability: %w", err)
	}
	var htmlOutput bytes.Buffer
	if err := article.RenderHTML(&htmlOutput); err != nil {
		return domain.ReadDocument{}, fmt.Errorf("render readability HTML: %w", err)
	}
	var textOutput bytes.Buffer
	if err := article.RenderText(&textOutput); err != nil {
		return domain.ReadDocument{}, fmt.Errorf("render readability text: %w", err)
	}

	document, err := goquery.NewDocumentFromReader(strings.NewReader(htmlOutput.String()))
	if err != nil {
		return domain.ReadDocument{}, fmt.Errorf("parse readability HTML: %w", err)
	}
	root := document.Find("body").First()
	if root.Length() == 0 {
		root = document.Selection
	}
	for _, node := range root.Nodes {
		sanitizeNode(node)
	}

	contentHTML, err := root.Html()
	if err != nil {
		return domain.ReadDocument{}, fmt.Errorf("extract HTML: serialize content: %w", err)
	}
	contentText := strings.TrimSpace(textOutput.String())
	if contentHTML == "" && contentText == "" {
		return domain.ReadDocument{}, fmt.Errorf("extract HTML: empty content")
	}

	finalURL := resource.FinalURL
	if finalURL == "" {
		finalURL = resource.URL
	}
	return domain.ReadDocument{
		URL:         resource.URL,
		FinalURL:    finalURL,
		Title:       firstNonEmpty(cleanText(root.Find("h1,h2").First().Text()), strings.TrimSpace(article.Title())),
		Author:      strings.TrimSpace(article.Byline()),
		PublishedAt: readabilityPublishedAt(article),
		Language:    strings.TrimSpace(article.Language()),
		SourceType:  domain.SourceTypeHTML,
		ContentHTML: strings.TrimSpace(contentHTML),
		ContentText: contentText,
	}, nil
}

func readabilityPublishedAt(article readability.Article) string {
	publishedAt, err := article.PublishedTime()
	if err != nil {
		return ""
	}
	return publishedAt.Format(time.RFC3339)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func sanitizeNode(node *html.Node) {
	if node == nil {
		return
	}
	attributes := node.Attr[:0]
	for _, attribute := range node.Attr {
		key := strings.ToLower(attribute.Key)
		if strings.HasPrefix(key, "on") || key == "style" || key == "srcdoc" {
			continue
		}
		if key == "href" || key == "src" {
			if !safeContentURL(key, attribute.Val) {
				continue
			}
		}
		attributes = append(attributes, attribute)
	}
	node.Attr = attributes
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		sanitizeNode(child)
	}
}

func safeContentURL(attribute, rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return parsed.Host != ""
	case "mailto":
		return attribute == "href"
	default:
		return false
	}
}

func isDisplayControl(r rune) bool {
	return unicode.IsControl(r) && r != '\n' && r != '\t'
}
