package extractor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"web-search-backend/internal/domain"
)

// HTMLExtractionStrategy extracts one canonical HTML document using a replaceable algorithm.
type HTMLExtractionStrategy interface {
	// Name returns the typed strategy implementation name.
	Name() domain.ImplementationName
	// Extract converts one HTML resource into a canonical document.
	Extract(ctx context.Context, resource domain.Resource) (domain.ReadDocument, error)
}

// HTMLExtractor tries ordered HTML extraction strategies.
type HTMLExtractor struct {
	strategies []HTMLExtractionStrategy
}

type challengeError struct {
	signature string
}

func (err *challengeError) Error() string {
	return fmt.Sprintf("HTML verification challenge detected: %s", err.signature)
}

func (*challengeError) ReadErrorCode() domain.ErrorCode {
	return domain.ErrCaptchaRequired
}

// NewHTMLExtractor creates an ordered HTML strategy chain.
func NewHTMLExtractor(strategies ...HTMLExtractionStrategy) (*HTMLExtractor, error) {
	if len(strategies) == 0 {
		strategies = defaultHTMLStrategies()
	}
	for index, strategy := range strategies {
		if strategy == nil {
			return nil, fmt.Errorf("HTML extraction strategy %d is nil", index)
		}
		if strategy.Name() == "" {
			return nil, fmt.Errorf("HTML extraction strategy %d has no name", index)
		}
	}
	return &HTMLExtractor{strategies: append([]HTMLExtractionStrategy(nil), strategies...)}, nil
}

// Name returns the stable HTML extractor implementation name.
func (HTMLExtractor) Name() domain.ImplementationName {
	return domain.ImplementationNameHTMLExtractor
}

// SourceTypes returns the source types handled by this extractor.
func (HTMLExtractor) SourceTypes() []domain.SourceType {
	return []domain.SourceType{domain.SourceTypeHTML}
}

// Extract returns the first successful strategy result.
func (extractor HTMLExtractor) Extract(ctx context.Context, resource domain.Resource) (domain.ReadDocument, error) {
	if signature := challengeSignature(resource.Body); signature != "" {
		return domain.ReadDocument{}, &challengeError{signature: signature}
	}
	strategies := extractor.strategies
	if len(strategies) == 0 {
		strategies = defaultHTMLStrategies()
	}
	strategyErrors := make([]error, 0, len(strategies))
	for _, strategy := range strategies {
		document, err := strategy.Extract(ctx, resource)
		if err == nil {
			document.Extractor = strategy.Name()
			return document, nil
		}
		strategyErrors = append(strategyErrors, fmt.Errorf("%s: %w", strategy.Name(), err))
	}
	return domain.ReadDocument{}, errors.Join(strategyErrors...)
}

func challengeSignature(body []byte) string {
	lowerBody := bytes.ToLower(body)
	for _, signature := range []string{"eo-bot-js-token", "window.solvechallenge", "geetest_challenge", "cf-chl-"} {
		if bytes.Contains(lowerBody, []byte(signature)) {
			return signature
		}
	}
	return ""
}

func defaultHTMLStrategies() []HTMLExtractionStrategy {
	return []HTMLExtractionStrategy{ReadabilityStrategy{}, DOMArticleStrategy{}}
}

// ReadabilityStrategy extracts primary article content using Readability v2.
type ReadabilityStrategy struct{}

// Name returns the Readability implementation name.
func (ReadabilityStrategy) Name() domain.ImplementationName {
	return domain.ImplementationNameReadabilityExtractor
}

// Extract delegates article selection to Readability, then applies the service sanitizer.
func (ReadabilityStrategy) Extract(ctx context.Context, resource domain.Resource) (domain.ReadDocument, error) {
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

// DOMArticleStrategy extracts the largest known article container from server-rendered HTML.
type DOMArticleStrategy struct{}

// Name returns the article-DOM implementation name.
func (DOMArticleStrategy) Name() domain.ImplementationName {
	return domain.ImplementationNameDOMArticleExtractor
}

// Extract selects an article-like DOM container without executing JavaScript.
func (DOMArticleStrategy) Extract(ctx context.Context, resource domain.Resource) (domain.ReadDocument, error) {
	if err := ctx.Err(); err != nil {
		return domain.ReadDocument{}, err
	}
	if len(resource.Body) == 0 {
		return domain.ReadDocument{}, fmt.Errorf("extract article DOM: empty body")
	}
	document, err := goquery.NewDocumentFromReader(bytes.NewReader(resource.Body))
	if err != nil {
		return domain.ReadDocument{}, fmt.Errorf("extract article DOM: parse document: %w", err)
	}
	document.Find("script,style,noscript,nav,footer,header,aside,form,iframe,svg,canvas,template,[hidden],[aria-hidden='true']").Remove()
	root, candidateCount, bestScore := selectArticleRoot(document)
	if root.Length() == 0 || bestScore < 80 {
		return domain.ReadDocument{}, fmt.Errorf(
			"extract article DOM: no substantial article container: body_bytes=%d candidates=%d best_score=%d article_marker=%t",
			len(resource.Body), candidateCount, bestScore, bytes.Contains(resource.Body, []byte("mod-content__markdown")),
		)
	}
	baseURL := parsedResourceURL(resource)
	normalizeContentLinks(root, baseURL)
	for _, node := range root.Nodes {
		sanitizeNode(node)
	}
	contentHTML, err := root.Html()
	if err != nil {
		return domain.ReadDocument{}, fmt.Errorf("extract article DOM: serialize content: %w", err)
	}
	contentText := extractBlockText(root)
	if contentHTML == "" || contentText == "" {
		return domain.ReadDocument{}, fmt.Errorf("extract article DOM: empty content")
	}
	finalURL := resource.FinalURL
	if finalURL == "" {
		finalURL = resource.URL
	}
	return domain.ReadDocument{
		URL: resource.URL, FinalURL: finalURL,
		Title:       firstNonEmpty(cleanText(root.Find("h1,h2").First().Text()), metaContent(document, "meta[property='og:title']"), cleanText(document.Find("title").First().Text())),
		Author:      firstNonEmpty(metaContent(document, "meta[name='author']"), metaContent(document, "meta[property='article:author']")),
		PublishedAt: firstNonEmpty(metaContent(document, "meta[property='article:published_time']"), document.Find("time[datetime]").First().AttrOr("datetime", "")),
		Language:    strings.TrimSpace(document.Find("html").First().AttrOr("lang", "")),
		SourceType:  domain.SourceTypeHTML, ContentHTML: strings.TrimSpace(contentHTML), ContentText: contentText,
	}, nil
}

func selectArticleRoot(document *goquery.Document) (*goquery.Selection, int, int) {
	candidates := document.Find("article,main,[role='main'],.article-content,.post-content,.entry-content,.mod-content__markdown,.mod-content")
	var selected *goquery.Selection
	bestScore := 0
	candidates.Each(func(_ int, candidate *goquery.Selection) {
		score := utf8.RuneCountInString(cleanText(candidate.Text()))
		if score > bestScore {
			bestScore = score
			selected = candidate
		}
	})
	if selected == nil {
		return &goquery.Selection{}, candidates.Length(), 0
	}
	return selected, candidates.Length(), bestScore
}

func parsedResourceURL(resource domain.Resource) *url.URL {
	baseURL := resource.FinalURL
	if baseURL == "" {
		baseURL = resource.URL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil
	}
	return parsed
}

func normalizeContentLinks(root *goquery.Selection, baseURL *url.URL) {
	if baseURL == nil {
		return
	}
	root.Find("a[href],img[src]").Each(func(_ int, selection *goquery.Selection) {
		attribute := "href"
		if goquery.NodeName(selection) == "img" {
			attribute = "src"
		}
		rawValue, ok := selection.Attr(attribute)
		if !ok {
			return
		}
		reference, err := url.Parse(strings.TrimSpace(rawValue))
		if err == nil {
			selection.SetAttr(attribute, baseURL.ResolveReference(reference).String())
		}
	})
}

func metaContent(document *goquery.Document, selector string) string {
	return strings.TrimSpace(document.Find(selector).First().AttrOr("content", ""))
}

func extractBlockText(root *goquery.Selection) string {
	paragraphs := make([]string, 0)
	root.Find("h1,h2,h3,h4,h5,h6,p,blockquote,pre,li").Each(func(_ int, selection *goquery.Selection) {
		value := cleanText(selection.Text())
		if value != "" && (len(paragraphs) == 0 || paragraphs[len(paragraphs)-1] != value) {
			paragraphs = append(paragraphs, value)
		}
	})
	if len(paragraphs) == 0 {
		return cleanText(root.Text())
	}
	return strings.Join(paragraphs, "\n\n")
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
