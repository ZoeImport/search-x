package searchtrace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Spool synchronously appends durable NDJSON records before downstream storage.
type Spool struct {
	mu   sync.Mutex
	path string
	file *os.File
}

func OpenSpool(root string) (*Spool, error) {
	if root == "" {
		return nil, fmt.Errorf("trace spool root is empty")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create trace spool root: %w", err)
	}
	path := filepath.Join(root, "events.ndjson")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o640)
	if err != nil {
		return nil, fmt.Errorf("open trace spool: %w", err)
	}
	return &Spool{path: path, file: file}, nil
}

func (spool *Spool) Append(event Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode trace event: %w", err)
	}
	spool.mu.Lock()
	defer spool.mu.Unlock()
	if spool.file == nil {
		return fmt.Errorf("trace spool is closed")
	}
	if _, err := spool.file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append trace spool: %w", err)
	}
	if err := spool.file.Sync(); err != nil {
		return fmt.Errorf("sync trace spool: %w", err)
	}
	return nil
}

func (spool *Spool) Replay(consume func(Event) error) error {
	spool.mu.Lock()
	defer spool.mu.Unlock()
	if _, err := spool.file.Seek(0, 0); err != nil {
		return err
	}
	scanner := bufio.NewScanner(spool.file)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 4*1024*1024)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode trace spool: %w", err)
		}
		if err := consume(event); err != nil {
			return err
		}
	}
	_, _ = spool.file.Seek(0, 2)
	return scanner.Err()
}

// Consume replays and acknowledges the active segment while holding one lock,
// preventing appends from racing between replay and truncation.
func (spool *Spool) Consume(consume func(Event) error) error {
	spool.mu.Lock()
	defer spool.mu.Unlock()
	if spool.file == nil {
		return fmt.Errorf("trace spool is closed")
	}
	if _, err := spool.file.Seek(0, 0); err != nil {
		return err
	}
	scanner := bufio.NewScanner(spool.file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return err
		}
		if err := consume(event); err != nil {
			_, _ = spool.file.Seek(0, 2)
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := spool.file.Truncate(0); err != nil {
		return err
	}
	_, err := spool.file.Seek(0, 0)
	return err
}

// Reset acknowledges that every current record has committed to SQLite.
func (spool *Spool) Reset() error {
	spool.mu.Lock()
	defer spool.mu.Unlock()
	if spool.file == nil {
		return fmt.Errorf("trace spool is closed")
	}
	if err := spool.file.Truncate(0); err != nil {
		return err
	}
	if _, err := spool.file.Seek(0, 0); err != nil {
		return err
	}
	return spool.file.Sync()
}

func (spool *Spool) Close() error {
	spool.mu.Lock()
	defer spool.mu.Unlock()
	if spool.file == nil {
		return nil
	}
	err := spool.file.Close()
	spool.file = nil
	return err
}
