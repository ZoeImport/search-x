// Package converter provides output-format conversion for canonical read documents.
package converter

import (
	"fmt"

	"web-search-backend/internal/domain"
	readpipeline "web-search-backend/internal/read"
)

// Registry is an immutable mapping from output format to converter.
type Registry struct {
	byFormat map[domain.OutputFormat]readpipeline.ContentConverter
}

// NewRegistry builds an immutable converter registry and rejects duplicate registrations.
func NewRegistry(converters ...readpipeline.ContentConverter) (*Registry, error) {
	byFormat := make(map[domain.OutputFormat]readpipeline.ContentConverter, len(converters))
	for _, contentConverter := range converters {
		if contentConverter == nil {
			return nil, fmt.Errorf("register converter: nil implementation")
		}
		formats := contentConverter.Formats()
		if len(formats) == 0 {
			return nil, fmt.Errorf("register converter %q: no supported output format", contentConverter.Name())
		}
		for _, format := range formats {
			if format == "" {
				return nil, fmt.Errorf("register converter %q: empty output format", contentConverter.Name())
			}
			if existing, ok := byFormat[format]; ok {
				return nil, fmt.Errorf("register converter for %q: duplicate %q and %q", format, existing.Name(), contentConverter.Name())
			}
			byFormat[format] = contentConverter
		}
	}
	return &Registry{byFormat: byFormat}, nil
}

// Resolve returns the converter registered for format.
func (registry *Registry) Resolve(format domain.OutputFormat) (readpipeline.ContentConverter, error) {
	if registry == nil {
		return nil, fmt.Errorf("resolve converter for %q: nil registry", format)
	}
	contentConverter, ok := registry.byFormat[format]
	if !ok {
		return nil, fmt.Errorf("resolve converter for %q: not registered", format)
	}
	return contentConverter, nil
}
