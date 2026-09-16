package chromebrowser

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewRequiresProfileDirectory(t *testing.T) {
	t.Parallel()

	if _, err := New(Config{Timeout: time.Second}); err == nil {
		t.Fatal("New() error = nil, want profile directory validation error")
	}
}

func TestClientCachesBrowserInitializationFailure(t *testing.T) {
	wantErr := errors.New("browser unavailable")
	var calls atomic.Int32
	client := &Client{
		browserCtx: context.Background(),
		browserInit: func(context.Context) error {
			calls.Add(1)
			return wantErr
		},
	}

	for index := 0; index < 3; index++ {
		if err := client.ensureBrowser(); !errors.Is(err, wantErr) {
			t.Fatalf("ensureBrowser() error = %v, want %v", err, wantErr)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("browser initialization calls = %d, want 1", calls.Load())
	}
}
