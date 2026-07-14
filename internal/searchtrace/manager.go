package searchtrace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type FailureMode string

const (
	FailureModeStrict     FailureMode = "strict"
	FailureModeBestEffort FailureMode = "best_effort"
)

type ManagerConfig struct {
	FailureMode FailureMode
	StoreQuery  bool
	LogWriter   io.Writer
	Now         func() time.Time
}

// Manager persists every event to the durable spool before SQLite and JSON log sinks.
type Manager struct {
	spool     *Spool
	store     *Store
	config    ManagerConfig
	mu        sync.Mutex
	sequences map[string]uint64
	healthy   atomic.Bool
	notify    chan struct{}
	stop      chan struct{}
	done      chan struct{}
}

func NewManager(spool *Spool, store *Store, config ManagerConfig) (*Manager, error) {
	if spool == nil || store == nil {
		return nil, fmt.Errorf("trace manager dependencies are nil")
	}
	if config.FailureMode != FailureModeStrict && config.FailureMode != FailureModeBestEffort {
		return nil, fmt.Errorf("unsupported trace failure mode %q", config.FailureMode)
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.LogWriter == nil {
		config.LogWriter = io.Discard
	}
	manager := &Manager{spool: spool, store: store, config: config, sequences: make(map[string]uint64), notify: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	manager.healthy.Store(true)
	go manager.consumeLoop()
	return manager, nil
}

func (manager *Manager) Append(ctx context.Context, event Event) error {
	manager.mu.Lock()
	if event.OccurredAt.IsZero() {
		event.OccurredAt = manager.config.Now()
	}
	manager.sequences[event.TraceID]++
	if event.Sequence == 0 {
		event.Sequence = manager.sequences[event.TraceID]
	}
	manager.mu.Unlock()
	if err := manager.spool.Append(event); err != nil {
		manager.healthy.Store(false)
		return manager.handle(err)
	}
	select {
	case manager.notify <- struct{}{}:
	default:
	}
	return nil
}

func (manager *Manager) Replay(ctx context.Context) error {
	err := manager.consume(ctx)
	manager.healthy.Store(err == nil)
	return manager.handle(err)
}

func (manager *Manager) replayLocked(ctx context.Context) error {
	return manager.consume(ctx)
}

func (manager *Manager) consume(ctx context.Context) error {
	return manager.spool.Consume(func(event Event) error {
		if err := manager.store.Put(ctx, event); err != nil {
			return err
		}
		data, _ := json.Marshal(event)
		_, _ = manager.config.LogWriter.Write(append(data, '\n'))
		return nil
	})
}

func (manager *Manager) consumeLoop() {
	defer close(manager.done)
	for {
		select {
		case <-manager.stop:
			_ = manager.consume(context.Background())
			return
		case <-manager.notify:
			err := manager.consume(context.Background())
			manager.healthy.Store(err == nil)
		}
	}
}

func (manager *Manager) Flush(ctx context.Context) error { return manager.consume(ctx) }

func (manager *Manager) Healthy() bool { return manager.healthy.Load() }

func (manager *Manager) Cleanup(ctx context.Context, config RetentionConfig) error {
	if err := manager.store.Cleanup(ctx, config); err != nil {
		return err
	}
	if logger, ok := manager.config.LogWriter.(*RotatingLogger); ok {
		return logger.Cleanup()
	}
	return nil
}

func (manager *Manager) handle(err error) error {
	if err == nil || manager.config.FailureMode == FailureModeBestEffort {
		return nil
	}
	return fmt.Errorf("trace persistence unavailable: %w", err)
}

func (manager *Manager) Close() error {
	close(manager.stop)
	<-manager.done
	storeErr := manager.store.Close()
	spoolErr := manager.spool.Close()
	if storeErr != nil {
		return storeErr
	}
	return spoolErr
}

// QueryMetadata returns safe trace fields; full query storage is opt-in.
func (manager *Manager) QueryMetadata(query string) (hash string, length int, preview string, stored string) {
	digest := sha256.Sum256([]byte(query))
	hash = hex.EncodeToString(digest[:])
	length = len([]rune(query))
	preview = redactPreview(query, 32)
	if manager.config.StoreQuery {
		stored = query
	}
	return
}

func redactPreview(query string, maxRunes int) string {
	query = strings.TrimSpace(query)
	runes := []rune(query)
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	for index, value := range runes {
		if value >= '0' && value <= '9' {
			runes[index] = '*'
		}
	}
	return string(runes)
}
