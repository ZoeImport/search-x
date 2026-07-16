// Package detector identifies supported resource source types.
package detector

import (
	"fmt"
	"mime"
	"strings"

	"web-search-backend/webfetch/internal/domain"
)

// MIMETypeDetector maps normalized MIME types to typed source formats.
type MIMETypeDetector struct{}

// NewMIMETypeDetector creates a stateless MIME source-type detector.
func NewMIMETypeDetector() *MIMETypeDetector {
	return &MIMETypeDetector{}
}

// Name returns the typed implementation name.
func (*MIMETypeDetector) Name() domain.ImplementationName {
	return domain.ImplementationNameMIMETypeDetector
}

// Detect returns the supported source type for a fetched resource.
func (d *MIMETypeDetector) Detect(resource domain.Resource) (domain.SourceType, error) {
	contentType := strings.TrimSpace(strings.ToLower(resource.ContentType))
	if parsed, _, err := mime.ParseMediaType(contentType); err == nil {
		contentType = parsed
	}
	switch contentType {
	case "text/html", "application/xhtml+xml":
		return domain.SourceTypeHTML, nil
	case "text/plain":
		return domain.SourceTypePlainText, nil
	default:
		return "", fmt.Errorf("unsupported content type %q", resource.ContentType)
	}
}
