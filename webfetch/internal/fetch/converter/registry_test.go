package converter

import (
	"context"
	"testing"

	"web-search-backend/webfetch/internal/domain"
)

func TestNewRegistryRejectsDuplicateFormats(t *testing.T) {
	t.Parallel()

	_, err := NewRegistry(stubConverter{name: "one"}, stubConverter{name: "two"})
	if err == nil {
		t.Fatal("NewRegistry() error = nil, want duplicate registration error")
	}
}

func TestRegistryResolveRejectsMissingFormat(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry(stubConverter{name: "one"})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if _, err := registry.Resolve(domain.OutputFormatText); err == nil {
		t.Fatal("Resolve() error = nil, want missing registration error")
	}
}

func TestNewRegistryRejectsConverterWithoutSupportedFormat(t *testing.T) {
	t.Parallel()

	_, err := NewRegistry(unsupportedConverter{})
	if err == nil {
		t.Fatal("NewRegistry() error = nil, want unsupported converter error")
	}
}

type stubConverter struct {
	name domain.ImplementationName
}

func (converter stubConverter) Name() domain.ImplementationName { return converter.name }
func (stubConverter) Formats() []domain.OutputFormat {
	return []domain.OutputFormat{domain.OutputFormatMarkdown}
}
func (stubConverter) Convert(context.Context, domain.ReadDocument) (domain.FormattedContent, error) {
	return domain.FormattedContent{}, nil
}

type unsupportedConverter struct{}

func (unsupportedConverter) Name() domain.ImplementationName { return "unsupported" }
func (unsupportedConverter) Formats() []domain.OutputFormat  { return nil }
func (unsupportedConverter) Convert(context.Context, domain.ReadDocument) (domain.FormattedContent, error) {
	return domain.FormattedContent{}, nil
}
