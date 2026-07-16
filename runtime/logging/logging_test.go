package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggerWritesRollingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.log")
	logger, closer, err := New(Config{File: path, Level: "debug", MaxSizeMB: 1})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("ready", "request_id", "req_test")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"request_id":"req_test"`) {
		t.Fatalf("log does not contain request id: %s", body)
	}
}
