package requesttimeout

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, value string
		want        time.Duration
		wantErr     bool
	}{
		{name: "default", want: 20 * time.Second},
		{name: "milliseconds", value: "1500ms", want: 1500 * time.Millisecond},
		{name: "seconds", value: "60s", want: 60 * time.Second},
		{name: "below minimum", value: "99ms", wantErr: true},
		{name: "decimal", value: "1.5s", wantErr: true},
		{name: "compound", value: "1s500ms", wantErr: true},
		{name: "zero", value: "0s", wantErr: true},
		{name: "over maximum", value: "61s", wantErr: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(test.value, 20*time.Second, 60*time.Second)
			if (err != nil) != test.wantErr {
				t.Fatalf("Parse() error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("Parse() = %s, want %s", got, test.want)
			}
		})
	}
}
