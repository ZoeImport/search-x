package searchtrace

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RotatingLogger writes one JSONL file per UTC day and truncates expired files.
type RotatingLogger struct {
	mu        sync.Mutex
	root      string
	retention time.Duration
	now       func() time.Time
}

func NewRotatingLogger(root string, retention time.Duration, now func() time.Time) (*RotatingLogger, error) {
	if root == "" {
		return nil, fmt.Errorf("trace log root is empty")
	}
	if retention <= 0 {
		retention = 7 * 24 * time.Hour
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	return &RotatingLogger{root: root, retention: retention, now: now}, nil
}

func (logger *RotatingLogger) Write(data []byte) (int, error) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	path := filepath.Join(logger.root, logger.now().UTC().Format("2006-01-02")+".jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return 0, err
	}
	written, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return written, writeErr
	}
	return written, closeErr
}

func (logger *RotatingLogger) Cleanup() error {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	entries, err := os.ReadDir(logger.root)
	if err != nil {
		return err
	}
	cutoff := logger.now().Add(-logger.retention)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		stamp, parseErr := time.Parse("2006-01-02", entry.Name()[:len("2006-01-02")])
		if parseErr == nil && stamp.Before(cutoff) {
			if err := os.Truncate(filepath.Join(logger.root, entry.Name()), 0); err != nil {
				return err
			}
		}
	}
	return nil
}
