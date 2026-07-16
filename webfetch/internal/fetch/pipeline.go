// Package read defines the replaceable boundaries of the content-read pipeline.
package read

import (
	"context"

	"web-search-backend/webfetch/internal/domain"
)

// URLPolicy validates a URL and resolves it to a safe, pinned target.
type URLPolicy interface {
	// ValidateAndResolve returns a canonical target with policy-approved pinned addresses.
	ValidateAndResolve(ctx context.Context, rawURL string) (domain.SafeTarget, error)
}

// SiteStrategyResolver selects a domain/path-specific strategy after URL policy
// validation. Implementations may use longest suffix/path matching.
type SiteStrategyResolver interface {
	Resolve(target domain.SafeTarget) SiteStrategy
}

// SiteStrategy is the second extension layer for site-specific read behavior.
// Phase one strategies leave the safe target unchanged.
type SiteStrategy interface {
	Name() string
	Prepare(ctx context.Context, target domain.SafeTarget) (domain.SafeTarget, error)
}

// ResourceReader retrieves an unextracted resource from a safe target.
type ResourceReader interface {
	// Name returns the concrete reader implementation name.
	Name() domain.ImplementationName
	// Read fetches one already validated target.
	Read(ctx context.Context, target domain.SafeTarget) (domain.Resource, error)
}

// SourceTypeDetector identifies a supported source type from bounded resource data.
type SourceTypeDetector interface {
	// Name returns the concrete detector implementation name.
	Name() domain.ImplementationName
	// Detect identifies the supported source type for a bounded resource.
	Detect(resource domain.Resource) (domain.SourceType, error)
}

// ContentExtractor converts one supported source type into a canonical document.
type ContentExtractor interface {
	// Name returns the concrete extractor implementation name.
	Name() domain.ImplementationName
	// SourceTypes returns every source type handled by this implementation.
	SourceTypes() []domain.SourceType
	// Extract converts one resource into a format-independent canonical document.
	Extract(ctx context.Context, resource domain.Resource) (domain.ReadDocument, error)
}

// QualityEvaluator decides whether a canonical document is accepted, rendered, or rejected.
type QualityEvaluator interface {
	// Name returns the concrete evaluator implementation name.
	Name() domain.ImplementationName
	// Evaluate returns an explicit accept, render, or reject result.
	Evaluate(document domain.ReadDocument, resource domain.Resource) domain.QualityResult
}

// ContentConverter converts a canonical document into an API output format.
type ContentConverter interface {
	// Name returns the concrete converter implementation name.
	Name() domain.ImplementationName
	// Formats returns every output format handled by this implementation.
	Formats() []domain.OutputFormat
	// Convert renders a canonical document in one API output format.
	Convert(ctx context.Context, document domain.ReadDocument) (domain.FormattedContent, error)
}

// ReadCache stores canonical documents independently of their eventual output format.
type ReadCache interface {
	// GetFresh returns a document while its fresh TTL is valid.
	GetFresh(ctx context.Context, key string) (domain.ReadDocument, bool)
	// GetStale returns a document while its stale TTL is valid.
	GetStale(ctx context.Context, key string) (domain.ReadDocument, bool)
	// Set stores one format-independent canonical document.
	Set(ctx context.Context, key string, document domain.ReadDocument) error
}

// ExtractorRegistry resolves exactly one extractor for a source type.
type ExtractorRegistry interface {
	// Resolve returns the unique extractor registered for sourceType.
	Resolve(sourceType domain.SourceType) (ContentExtractor, error)
}

// ConverterRegistry resolves exactly one converter for an output format.
type ConverterRegistry interface {
	// Resolve returns the unique converter registered for format.
	Resolve(format domain.OutputFormat) (ContentConverter, error)
}
