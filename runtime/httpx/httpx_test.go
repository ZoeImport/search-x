package httpx

import (
	"strings"
	"testing"
)

func TestNewRequestID(t *testing.T) {
	first, second := NewRequestID(), NewRequestID()
	if first == second || !strings.HasPrefix(first, "req_") {
		t.Fatalf("unexpected request ids %q %q", first, second)
	}
}
