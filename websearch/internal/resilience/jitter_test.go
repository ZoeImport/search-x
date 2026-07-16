package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestJitterUsesInjectedRandomDuration(t *testing.T) {
	started := time.Now()
	jitter, err := newJitter(2*time.Millisecond, 10*time.Millisecond, func(int64) (int64, error) { return 0, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := jitter.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 2*time.Millisecond || elapsed > 100*time.Millisecond {
		t.Fatalf("elapsed=%v", elapsed)
	}
}

func TestJitterHonorsCancellation(t *testing.T) {
	jitter, err := NewJitter(time.Second, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := jitter.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestJitterRejectsInvalidRange(t *testing.T) {
	if _, err := NewJitter(time.Second, time.Millisecond); err == nil {
		t.Fatal("expected invalid range error")
	}
}
