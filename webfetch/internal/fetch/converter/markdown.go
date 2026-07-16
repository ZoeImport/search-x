package converter

import (
	"context"
	"fmt"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"

	"web-search-backend/webfetch/internal/domain"
)

// MarkdownConverter converts canonical article HTML to Markdown.
type MarkdownConverter struct{}

// Name returns the stable Markdown converter implementation name.
func (MarkdownConverter) Name() domain.ImplementationName {
	return domain.ImplementationNameMarkdownConverter
}

// Formats returns the output formats handled by this converter.
func (MarkdownConverter) Formats() []domain.OutputFormat {
	return []domain.OutputFormat{domain.OutputFormatMarkdown}
}

// Convert converts canonical HTML structure to bounded, readable Markdown syntax.
func (MarkdownConverter) Convert(ctx context.Context, document domain.ReadDocument) (domain.FormattedContent, error) {
	if err := ctx.Err(); err != nil {
		return domain.FormattedContent{}, err
	}
	if strings.TrimSpace(document.ContentHTML) == "" {
		if strings.TrimSpace(document.ContentText) == "" {
			return domain.FormattedContent{}, fmt.Errorf("convert Markdown: document has no content")
		}
		return domain.FormattedContent{Content: strings.TrimSpace(document.ContentText), Format: domain.OutputFormatMarkdown}, nil
	}
	content, err := htmltomarkdown.ConvertString(document.ContentHTML)
	if err != nil {
		return domain.FormattedContent{}, fmt.Errorf("convert Markdown: %w", err)
	}
	content = normalizeMarkdown(content)
	if content == "" {
		return domain.FormattedContent{}, fmt.Errorf("convert Markdown: empty result")
	}
	return domain.FormattedContent{Content: content, Format: domain.OutputFormatMarkdown}, nil
}

func normalizeMarkdown(content string) string {
	lines := strings.Split(content, "\n")
	output := make([]string, 0, len(lines))
	empty := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			empty++
			if empty > 1 {
				continue
			}
		} else {
			empty = 0
		}
		output = append(output, line)
	}
	return strings.TrimSpace(strings.Join(output, "\n"))
}
