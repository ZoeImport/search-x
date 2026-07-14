package app

import "testing"

func TestCandidateLimitUsesOversamplingFormula(t *testing.T) {
	tests := []struct {
		limit int
		want  int
	}{
		{limit: 1, want: 4},
		{limit: 3, want: 8},
		{limit: 5, want: 12},
		{limit: 10, want: 20},
	}
	for _, test := range tests {
		got, err := CandidateLimit(test.limit, 0)
		if err != nil {
			t.Fatalf("limit %d: %v", test.limit, err)
		}
		if got != test.want {
			t.Fatalf("limit %d: got %d want %d", test.limit, got, test.want)
		}
	}
}

func TestCandidateLimitValidatesExplicitValue(t *testing.T) {
	if got, err := CandidateLimit(5, 8); err != nil || got != 8 {
		t.Fatalf("got %d, err %v", got, err)
	}
	for _, explicit := range []int{4, 21} {
		if _, err := CandidateLimit(5, explicit); err == nil {
			t.Fatalf("expected explicit value %d to fail", explicit)
		}
	}
}

func TestCandidateLimitRejectsInvalidResultLimit(t *testing.T) {
	for _, limit := range []int{0, 11} {
		if _, err := CandidateLimit(limit, 0); err == nil {
			t.Fatalf("expected limit %d to fail", limit)
		}
	}
}
