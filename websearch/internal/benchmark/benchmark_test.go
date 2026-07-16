package benchmark

import (
	"os"
	"path/filepath"
	"testing"

	"web-search-backend/websearch/internal/provider/duckduckgo"
)

func BenchmarkDuckDuckGoParse(b *testing.B) {
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "duckduckgo", "normal.html"))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := duckduckgo.Parse(body, 10); err != nil {
			b.Fatal(err)
		}
	}
}
