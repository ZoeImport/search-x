// Package extractor provides source-specific canonical content extractors.
package extractor

import (
	"fmt"

	"web-search-backend/webfetch/internal/domain"
	readpipeline "web-search-backend/webfetch/internal/fetch"
)

// Registry is an immutable mapping from source type to extractor.
type Registry struct {
	bySourceType map[domain.SourceType]readpipeline.ContentExtractor
}

// NewRegistry builds an immutable extractor registry and rejects duplicate registrations.
func NewRegistry(extractors ...readpipeline.ContentExtractor) (*Registry, error) {
	bySourceType := make(map[domain.SourceType]readpipeline.ContentExtractor, len(extractors))
	for _, contentExtractor := range extractors {
		if contentExtractor == nil {
			return nil, fmt.Errorf("register extractor: nil implementation")
		}
		sourceTypes := contentExtractor.SourceTypes()
		if len(sourceTypes) == 0 {
			return nil, fmt.Errorf("register extractor %q: no supported source type", contentExtractor.Name())
		}
		for _, sourceType := range sourceTypes {
			if sourceType == "" {
				return nil, fmt.Errorf("register extractor %q: empty source type", contentExtractor.Name())
			}
			if existing, ok := bySourceType[sourceType]; ok {
				return nil, fmt.Errorf("register extractor for %q: duplicate %q and %q", sourceType, existing.Name(), contentExtractor.Name())
			}
			bySourceType[sourceType] = contentExtractor
		}
	}
	return &Registry{bySourceType: bySourceType}, nil
}

// Resolve returns the extractor registered for sourceType.
func (registry *Registry) Resolve(sourceType domain.SourceType) (readpipeline.ContentExtractor, error) {
	if registry == nil {
		return nil, fmt.Errorf("resolve extractor for %q: nil registry", sourceType)
	}
	contentExtractor, ok := registry.bySourceType[sourceType]
	if !ok {
		return nil, fmt.Errorf("resolve extractor for %q: not registered", sourceType)
	}
	return contentExtractor, nil
}
