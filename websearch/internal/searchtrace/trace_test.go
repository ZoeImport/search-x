package searchtrace

import "testing"

func TestQueryMetadataRedactsDigits(t *testing.T) {
	manager := New(Config{PreviewChars: 32})
	hash, length, preview, stored := manager.QueryMetadata("customer 123")
	if hash == "" || length != 12 || preview != "customer ***" || stored != "" {
		t.Fatalf("metadata = %q %d %q %q", hash, length, preview, stored)
	}
}
