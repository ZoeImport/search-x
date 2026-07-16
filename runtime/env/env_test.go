package env

import (
	"testing"
	"time"
)

func TestStrictParsers(t *testing.T) {
	t.Setenv("BOOL_VALUE", "true")
	t.Setenv("INT_VALUE", "4")
	t.Setenv("DURATION_VALUE", "3s")
	if value, err := Bool("BOOL_VALUE", false); err != nil || !value {
		t.Fatalf("Bool() = %v, %v", value, err)
	}
	if value, err := PositiveInt("INT_VALUE", 1); err != nil || value != 4 {
		t.Fatalf("PositiveInt() = %v, %v", value, err)
	}
	if value, err := PositiveDuration("DURATION_VALUE", time.Second); err != nil || value != 3*time.Second {
		t.Fatalf("PositiveDuration() = %v, %v", value, err)
	}
}
