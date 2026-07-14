package domain

import "testing"

func TestReadRequestNormalize(t *testing.T) {
	t.Parallel()

	request, err := (ReadRequest{URL: "https://example.com/article", Debug: true}).Normalize()
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if request.Format != OutputFormatMarkdown {
		t.Fatalf("Format = %q, want %q", request.Format, OutputFormatMarkdown)
	}
	if request.MaxChars != DefaultReadMaxChars {
		t.Fatalf("MaxChars = %d, want %d", request.MaxChars, DefaultReadMaxChars)
	}
	if !request.Refresh {
		t.Fatal("Debug request must force Refresh")
	}
}

func TestReadRequestNormalizeRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request ReadRequest
	}{
		{name: "missing URL", request: ReadRequest{}},
		{name: "long URL", request: ReadRequest{URL: "https://example.com/" + string(make([]byte, MaxReadURLLength))}},
		{name: "unsupported format", request: ReadRequest{URL: "https://example.com", Format: OutputFormat("html")}},
		{name: "too short", request: ReadRequest{URL: "https://example.com", MaxChars: MinReadMaxChars - 1}},
		{name: "too long", request: ReadRequest{URL: "https://example.com", MaxChars: MaxReadMaxChars + 1}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := test.request.Normalize(); err == nil {
				t.Fatal("Normalize() error = nil, want non-nil")
			}
		})
	}
}
