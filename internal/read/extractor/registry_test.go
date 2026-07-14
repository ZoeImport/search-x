package extractor

import (
	"context"
	"testing"

	"web-search-backend/internal/domain"
	readpipeline "web-search-backend/internal/read"
)

func TestNewRegistryRejectsDuplicateSourceTypes(t *testing.T) {
	t.Parallel()

	_, err := NewRegistry(stubExtractor{name: "first"}, stubExtractor{name: "second"})
	if err == nil {
		t.Fatal("NewRegistry() error = nil, want duplicate registration error")
	}
}

func TestRegistryResolveRejectsMissingSourceType(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry(stubExtractor{name: "only"})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if _, err := registry.Resolve(domain.SourceTypePlainText); err == nil {
		t.Fatal("Resolve() error = nil, want missing registration error")
	}
}

func TestNewRegistryRejectsExtractorWithoutSupportedSourceType(t *testing.T) {
	t.Parallel()

	_, err := NewRegistry(unsupportedExtractor{})
	if err == nil {
		t.Fatal("NewRegistry() error = nil, want unsupported extractor error")
	}
}

type stubExtractor struct {
	name domain.ImplementationName
}

func (extractor stubExtractor) Name() domain.ImplementationName { return extractor.name }
func (stubExtractor) SourceTypes() []domain.SourceType {
	return []domain.SourceType{domain.SourceTypeHTML}
}
func (stubExtractor) Extract(context.Context, domain.Resource) (domain.ReadDocument, error) {
	return domain.ReadDocument{}, nil
}

var _ readpipeline.ContentExtractor = stubExtractor{}

type unsupportedExtractor struct{}

func (unsupportedExtractor) Name() domain.ImplementationName  { return "unsupported" }
func (unsupportedExtractor) SourceTypes() []domain.SourceType { return nil }
func (unsupportedExtractor) Extract(context.Context, domain.Resource) (domain.ReadDocument, error) {
	return domain.ReadDocument{}, nil
}
