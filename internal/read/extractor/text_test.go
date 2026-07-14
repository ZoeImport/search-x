package extractor

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"web-search-backend/internal/domain"
)

func TestPlainTextExtractorNormalizesMalformedText(t *testing.T) {
	t.Parallel()

	body := []byte("first\r\n\r\n\r\n\r\nsecond\x00\x01\xff中文")
	document, err := (PlainTextExtractor{}).Extract(context.Background(), domain.Resource{
		URL:      "https://example.com/a.txt",
		FinalURL: "https://example.com/a.txt",
		Body:     body,
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if !utf8.ValidString(document.ContentText) {
		t.Fatal("ContentText is not valid UTF-8")
	}
	if strings.ContainsRune(document.ContentText, '\x00') || strings.ContainsRune(document.ContentText, '\x01') {
		t.Fatalf("ContentText contains control bytes: %q", document.ContentText)
	}
	if strings.Contains(document.ContentText, "\r") || strings.Contains(document.ContentText, "\n\n\n\n") {
		t.Fatalf("ContentText is not normalized: %q", document.ContentText)
	}
	if len(document.Warnings) != 1 || document.Warnings[0].Code != domain.ReadWarningInvalidUTF8 {
		t.Fatalf("Warnings = %#v, want invalid UTF-8 warning", document.Warnings)
	}
}

func TestPlainTextExtractorDecodesDeclaredCharset(t *testing.T) {
	t.Parallel()

	document, err := (PlainTextExtractor{}).Extract(context.Background(), domain.Resource{
		Charset: "iso-8859-1",
		Body:    []byte{'c', 'a', 'f', 0xe9},
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if document.ContentText != "café" {
		t.Fatalf("ContentText = %q, want café", document.ContentText)
	}
}
