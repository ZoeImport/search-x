package extractor

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/htmlindex"

	"web-search-backend/webfetch/internal/domain"
)

// PlainTextExtractor normalizes plain-text resources into canonical documents.
type PlainTextExtractor struct{}

// Name returns the stable plain-text extractor implementation name.
func (PlainTextExtractor) Name() domain.ImplementationName {
	return domain.ImplementationNamePlainTextExtractor
}

// SourceTypes returns the source types handled by this extractor.
func (PlainTextExtractor) SourceTypes() []domain.SourceType {
	return []domain.SourceType{domain.SourceTypePlainText}
}

// Extract normalizes invalid UTF-8, line endings, control characters, and blank lines.
func (PlainTextExtractor) Extract(ctx context.Context, resource domain.Resource) (domain.ReadDocument, error) {
	if err := ctx.Err(); err != nil {
		return domain.ReadDocument{}, err
	}
	if len(resource.Body) == 0 {
		return domain.ReadDocument{}, fmt.Errorf("extract plain text: empty body")
	}
	content, warnings := decodeText(resource.Body, resource.Charset)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	content = strings.Map(func(r rune) rune {
		if isDisplayControl(r) {
			return -1
		}
		return r
	}, content)
	content = normalizeBlankLines(content)
	if content == "" {
		return domain.ReadDocument{}, fmt.Errorf("extract plain text: no displayable content")
	}
	finalURL := resource.FinalURL
	if finalURL == "" {
		finalURL = resource.URL
	}
	return domain.ReadDocument{
		URL:         resource.URL,
		FinalURL:    finalURL,
		SourceType:  domain.SourceTypePlainText,
		ContentText: content,
		Warnings:    warnings,
	}, nil
}

func decodeText(body []byte, charset string) (string, []domain.ReadWarning) {
	charset = strings.ToLower(strings.TrimSpace(charset))
	if charset != "" && charset != "utf-8" && charset != "utf8" && charset != "us-ascii" {
		encoding, err := htmlindex.Get(charset)
		if err == nil && encoding != nil {
			decoded, decodeErr := encoding.NewDecoder().Bytes(body)
			if decodeErr == nil {
				return string(decoded), nil
			}
		}
		content := strings.ToValidUTF8(string(body), "�")
		return content, []domain.ReadWarning{{
			Code: domain.ReadWarningUnsupportedCharset, Message: fmt.Sprintf("unsupported or invalid charset %q; replaced invalid UTF-8", charset),
		}}
	}
	if utf8.Valid(body) {
		return string(body), nil
	}
	return strings.ToValidUTF8(string(body), "�"), []domain.ReadWarning{{
		Code: domain.ReadWarningInvalidUTF8, Message: "invalid UTF-8 bytes were replaced",
	}}
}

func normalizeBlankLines(content string) string {
	lines := strings.Split(content, "\n")
	output := make([]string, 0, len(lines))
	blankLines := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			blankLines++
			if blankLines > 2 {
				continue
			}
		} else {
			blankLines = 0
		}
		output = append(output, line)
	}
	return strings.TrimSpace(strings.Join(output, "\n"))
}
