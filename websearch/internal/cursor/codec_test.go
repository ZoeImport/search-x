package cursor

import (
	"errors"
	"testing"
	"time"
)

func TestRoundTripAndExpiry(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	codec, err := New("test-secret", time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	token, err := codec.Encode(State{QueryHash: QueryHash("go"), Provider: "bing", Providers: []string{"baidu", "bing"}, ProviderPage: 2, ProviderPageToken: "opaque-provider-token", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	state, err := codec.Decode(token)
	if err != nil || state.Provider != "bing" || state.ProviderPage != 2 || state.ProviderPageToken != "opaque-provider-token" {
		t.Fatalf("Decode() = %#v, %v", state, err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := codec.Decode(token); !errors.Is(err, ErrExpired) {
		t.Fatalf("Decode() error = %v", err)
	}
}
