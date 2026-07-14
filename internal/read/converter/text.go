package converter

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"web-search-backend/internal/domain"
)

// TextConverter converts canonical documents to plain text.
type TextConverter struct{}

// Name returns the stable text converter implementation name.
func (TextConverter) Name() domain.ImplementationName {
	return domain.ImplementationNameTextConverter
}

// Formats returns the output formats handled by this converter.
func (TextConverter) Formats() []domain.OutputFormat {
	return []domain.OutputFormat{domain.OutputFormatText}
}

// Convert returns canonical plain text, deriving it from HTML only when necessary.
func (TextConverter) Convert(ctx context.Context, document domain.ReadDocument) (domain.FormattedContent, error) {
	if err := ctx.Err(); err != nil {
		return domain.FormattedContent{}, err
	}
	content := strings.TrimSpace(document.ContentText)
	if content == "" && strings.TrimSpace(document.ContentHTML) != "" {
		parsed, err := goquery.NewDocumentFromReader(strings.NewReader(document.ContentHTML))
		if err != nil {
			return domain.FormattedContent{}, fmt.Errorf("convert text: parse HTML: %w", err)
		}
		content = strings.Join(strings.Fields(parsed.Text()), " ")
	}
	if content == "" {
		return domain.FormattedContent{}, fmt.Errorf("convert text: document has no content")
	}
	return domain.FormattedContent{Content: content, Format: domain.OutputFormatText}, nil
}

// TruncateContent bounds content by Unicode characters, preferring a paragraph boundary.
func TruncateContent(content string, maxRunes int) (string, bool) {
	if maxRunes < 0 {
		maxRunes = 0
	}
	if utf8.RuneCountInString(content) <= maxRunes {
		return content, false
	}
	runes := []rune(content)
	prefix := string(runes[:maxRunes])
	if boundary := strings.LastIndex(prefix, "\n\n"); boundary > 0 {
		prefix = prefix[:boundary]
	}
	return strings.TrimSpace(prefix), true
}
