package benchmark

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"web-search-backend/webfetch/internal/domain"
	"web-search-backend/webfetch/internal/fetch/extractor"
)

func BenchmarkHTMLExtract(b *testing.B) {
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "read", "article.html"))
	if err != nil {
		b.Fatal(err)
	}
	value, err := extractor.NewHTMLExtractor()
	if err != nil {
		b.Fatal(err)
	}
	resource := domain.Resource{URL: "https://example.com/article", FinalURL: "https://example.com/article", ContentType: "text/html", Body: body}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := value.Extract(context.Background(), resource); err != nil {
			b.Fatal(err)
		}
	}
}
