package chromebrowser

import (
	"testing"
	"time"
)

func TestNewRequiresProfileDirectory(t *testing.T) {
	t.Parallel()

	if _, err := New(Config{Timeout: time.Second}); err == nil {
		t.Fatal("New() error = nil, want profile directory validation error")
	}
}
