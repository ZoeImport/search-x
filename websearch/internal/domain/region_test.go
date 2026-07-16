package domain

import "testing"

func TestNormalizeRegionAcceptsEmpty(t *testing.T) {
	got, err := NormalizeRegion("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("region = %q", got)
	}
}

func TestNormalizeRegionTrimsAndLowercases(t *testing.T) {
	got, err := NormalizeRegion("  JP  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "jp" {
		t.Fatalf("region = %q", got)
	}
}

func TestNormalizeRegionRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"usa", "u", "12", "j p", "日本"} {
		if _, err := NormalizeRegion(value); err == nil {
			t.Fatalf("expected validation error for %q", value)
		}
	}
}
