package detector

import (
	"testing"

	"web-search-backend/webfetch/internal/domain"
)

func TestMIMETypeDetectorRecognizesSupportedTypes(t *testing.T) {
	detector := NewMIMETypeDetector()
	for contentType, want := range map[string]domain.SourceType{
		"text/html":             domain.SourceTypeHTML,
		"application/xhtml+xml": domain.SourceTypeHTML,
		"text/plain":            domain.SourceTypePlainText,
	} {
		got, err := detector.Detect(domain.Resource{ContentType: contentType})
		if err != nil || got != want {
			t.Fatalf("content_type=%q got=%q err=%v", contentType, got, err)
		}
	}
}

func TestMIMETypeDetectorRejectsUnsupportedType(t *testing.T) {
	detector := NewMIMETypeDetector()
	if _, err := detector.Detect(domain.Resource{ContentType: "application/pdf"}); err == nil {
		t.Fatal("Detect() error=nil, want unsupported type error")
	}
}
