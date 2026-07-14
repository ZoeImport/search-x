package read

import (
	"context"
	"testing"

	"web-search-backend/internal/domain"
)

func TestPipelineInterfacesRemainNarrow(t *testing.T) {
	t.Parallel()

	var _ URLPolicy = fakePolicy{}
	var _ ResourceReader = fakeReader{}
	var _ SourceTypeDetector = fakeDetector{}
	var _ ContentExtractor = fakeExtractor{}
	var _ QualityEvaluator = fakeEvaluator{}
	var _ ContentConverter = fakeConverter{}
	var _ ReadCache = fakeCache{}
}

type fakePolicy struct{}

func (fakePolicy) ValidateAndResolve(context.Context, string) (domain.SafeTarget, error) {
	return domain.SafeTarget{}, nil
}

type fakeReader struct{}

func (fakeReader) Name() domain.ImplementationName { return domain.ImplementationNameHTTPReader }
func (fakeReader) Read(context.Context, domain.SafeTarget) (domain.Resource, error) {
	return domain.Resource{}, nil
}

type fakeDetector struct{}

func (fakeDetector) Name() domain.ImplementationName {
	return domain.ImplementationNameMIMETypeDetector
}

func (fakeDetector) Detect(domain.Resource) (domain.SourceType, error) {
	return domain.SourceTypeHTML, nil
}

type fakeExtractor struct{}

func (fakeExtractor) Name() domain.ImplementationName { return domain.ImplementationNameHTMLExtractor }
func (fakeExtractor) SourceTypes() []domain.SourceType {
	return []domain.SourceType{domain.SourceTypeHTML}
}
func (fakeExtractor) Extract(context.Context, domain.Resource) (domain.ReadDocument, error) {
	return domain.ReadDocument{}, nil
}

type fakeEvaluator struct{}

func (fakeEvaluator) Name() domain.ImplementationName {
	return domain.ImplementationNameArticleQualityEvaluator
}

func (fakeEvaluator) Evaluate(domain.ReadDocument, domain.Resource) domain.QualityResult {
	return domain.QualityResult{}
}

type fakeConverter struct{}

func (fakeConverter) Name() domain.ImplementationName { return domain.ImplementationNameTextConverter }
func (fakeConverter) Formats() []domain.OutputFormat {
	return []domain.OutputFormat{domain.OutputFormatText}
}
func (fakeConverter) Convert(context.Context, domain.ReadDocument) (domain.FormattedContent, error) {
	return domain.FormattedContent{}, nil
}

type fakeCache struct{}

func (fakeCache) GetFresh(context.Context, string) (domain.ReadDocument, bool) {
	return domain.ReadDocument{}, false
}
func (fakeCache) GetStale(context.Context, string) (domain.ReadDocument, bool) {
	return domain.ReadDocument{}, false
}
func (fakeCache) Set(context.Context, string, domain.ReadDocument) error { return nil }
