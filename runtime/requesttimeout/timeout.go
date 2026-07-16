package requesttimeout

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

var pattern = regexp.MustCompile(`^([1-9][0-9]*)(ms|s)$`)

// Parse validates the public API timeout syntax and applies the configured
// request ceiling. Only integer millisecond and second values are accepted.
func Parse(value string, defaultValue, maximum time.Duration) (time.Duration, error) {
	if maximum <= 0 {
		maximum = 60 * time.Second
	}
	if value == "" {
		return defaultValue, nil
	}
	matches := pattern.FindStringSubmatch(value)
	if matches == nil {
		return 0, fmt.Errorf("timeout must be an integer followed by ms or s")
	}
	number, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("timeout is too large")
	}
	unit := time.Millisecond
	if matches[2] == "s" {
		unit = time.Second
	}
	if number > int64(maximum/unit)+1 {
		return 0, fmt.Errorf("timeout exceeds maximum %s", maximum)
	}
	duration := time.Duration(number) * unit
	if duration < 100*time.Millisecond {
		return 0, fmt.Errorf("timeout must be at least 100ms")
	}
	if duration > maximum {
		return 0, fmt.Errorf("timeout must not exceed %s", maximum)
	}
	return duration, nil
}
