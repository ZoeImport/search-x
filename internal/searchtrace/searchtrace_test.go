package searchtrace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSpoolStoreReplayAndQueryRedaction(t *testing.T) {
	root := t.TempDir()
	spool, err := OpenSpool(filepath.Join(root, "spool"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(root, "trace.db"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(spool, store, ManagerConfig{FailureMode: FailureModeStrict, Now: func() time.Time { return time.Unix(10, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	hash, length, preview, stored := manager.QueryMetadata("phone 13800138000")
	if hash == "" || length != 17 || preview != "phone ***********" || stored != "" {
		t.Fatalf("metadata=%q %d %q %q", hash, length, preview, stored)
	}
	event := Event{TraceID: "trace-1", RequestID: "request-1", Type: "route_decision", QueryHash: hash, QueryLength: length, QueryPreview: preview}
	if err := manager.Append(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := manager.Replay(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := manager.store.db.QueryRow(`SELECT COUNT(*) FROM trace_events WHERE trace_id='trace-1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count=%d", count)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "spool", "events.ndjson"))
	if err != nil || len(data) != 0 {
		t.Fatalf("spool=%q err=%v", data, err)
	}
}

func TestFailureModes(t *testing.T) {
	for _, test := range []struct {
		name    string
		mode    FailureMode
		wantErr bool
	}{{"strict", FailureModeStrict, true}, {"best_effort", FailureModeBestEffort, false}} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			spool, _ := OpenSpool(filepath.Join(root, "spool"))
			store, _ := OpenStore(filepath.Join(root, "trace.db"))
			manager, _ := NewManager(spool, store, ManagerConfig{FailureMode: test.mode})
			_ = spool.Close()
			err := manager.Append(context.Background(), Event{TraceID: "t", RequestID: "r", Type: "request_started"})
			if (err != nil) != test.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, test.wantErr)
			}
			if manager.Healthy() {
				t.Fatal("manager should be unhealthy")
			}
			_ = store.Close()
		})
	}
}

func TestNewManagerRejectsMode(t *testing.T) {
	_, err := NewManager(nil, nil, FailureModeConfigForTest())
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRetentionRemovesExpiredRequestTrace(t *testing.T) {
	root := t.TempDir()
	store, err := OpenStore(filepath.Join(root, "trace.db"))
	if err != nil {
		t.Fatal(err)
	}
	old := time.Unix(10, 0)
	if err := store.Put(context.Background(), Event{TraceID: "old", RequestID: "old", Sequence: 1, Type: "route_decision", OccurredAt: old}); err != nil {
		t.Fatal(err)
	}
	if err := store.Cleanup(context.Background(), RetentionConfig{RequestRetention: time.Hour, ProfileRetention: 24 * time.Hour, MaxBytes: 1 << 30, Now: func() time.Time { return old.Add(2 * time.Hour) }}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM trace_events WHERE trace_id='old'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("count=%d", count)
	}
	_ = store.Close()
}

func FailureModeConfigForTest() ManagerConfig { return ManagerConfig{FailureMode: "invalid"} }
