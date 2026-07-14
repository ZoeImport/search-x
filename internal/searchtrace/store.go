package searchtrace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

func OpenStore(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("trace sqlite path is empty")
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (store *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS trace_events (
  trace_id TEXT NOT NULL, request_id TEXT NOT NULL, event_sequence INTEGER NOT NULL,
  event_type TEXT NOT NULL, occurred_at TEXT NOT NULL, provider TEXT, profile_id TEXT,
  lease_id TEXT, classification TEXT, payload_json TEXT NOT NULL,
  PRIMARY KEY(trace_id, event_sequence)
);
CREATE INDEX IF NOT EXISTS idx_trace_events_request ON trace_events(request_id, event_sequence);
CREATE INDEX IF NOT EXISTS idx_trace_events_profile ON trace_events(profile_id, occurred_at);
CREATE TABLE IF NOT EXISTS search_traces (trace_id TEXT PRIMARY KEY, request_id TEXT NOT NULL, requested_provider TEXT, query_hash TEXT, query_length INTEGER, started_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS route_decisions (trace_id TEXT NOT NULL, event_sequence INTEGER NOT NULL, provider TEXT, payload_json TEXT NOT NULL, PRIMARY KEY(trace_id,event_sequence));
CREATE TABLE IF NOT EXISTS provider_attempts (trace_id TEXT NOT NULL, event_sequence INTEGER NOT NULL, provider TEXT, classification TEXT, payload_json TEXT NOT NULL, PRIMARY KEY(trace_id,event_sequence));
CREATE TABLE IF NOT EXISTS profile_lease_events (trace_id TEXT NOT NULL, event_sequence INTEGER NOT NULL, provider TEXT, profile_id TEXT, lease_id TEXT, payload_json TEXT NOT NULL, PRIMARY KEY(trace_id,event_sequence));
CREATE TABLE IF NOT EXISTS profile_health_events (trace_id TEXT NOT NULL, event_sequence INTEGER NOT NULL, provider TEXT, profile_id TEXT, classification TEXT, occurred_at TEXT NOT NULL, payload_json TEXT NOT NULL, PRIMARY KEY(trace_id,event_sequence));`
	_, err := store.db.ExecContext(ctx, schema)
	return err
}

func (store *Store) Put(ctx context.Context, event Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := event.OccurredAt.UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO trace_events(trace_id,request_id,event_sequence,event_type,occurred_at,provider,profile_id,lease_id,classification,payload_json) VALUES(?,?,?,?,?,?,?,?,?,?)`, event.TraceID, event.RequestID, event.Sequence, event.Type, stamp, event.Provider, event.ProfileID, event.LeaseID, event.Classification, payload); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO search_traces(trace_id,request_id,requested_provider,query_hash,query_length,started_at,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(trace_id) DO UPDATE SET updated_at=excluded.updated_at`, event.TraceID, event.RequestID, event.RequestedProvider, event.QueryHash, event.QueryLength, stamp, stamp); err != nil {
		return err
	}
	table := eventTable(event.Type)
	if table != "" {
		query := fmt.Sprintf(`INSERT OR IGNORE INTO %s(trace_id,event_sequence,provider%s,payload_json%s) VALUES(?,?,?%s,?%s)`, table, extraColumns(table), healthTimeColumn(table), extraPlaceholders(table), healthTimePlaceholder(table))
		args := eventArgs(table, event, payload, stamp)
		if _, err = tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func eventTable(eventType string) string {
	switch eventType {
	case "route_decision":
		return "route_decisions"
	case "provider_attempt":
		return "provider_attempts"
	case "lease_acquired", "lease_released":
		return "profile_lease_events"
	case "profile_health", "profile_state":
		return "profile_health_events"
	default:
		return ""
	}
}

func extraColumns(table string) string {
	switch table {
	case "provider_attempts":
		return ",classification"
	case "profile_lease_events":
		return ",profile_id,lease_id"
	case "profile_health_events":
		return ",profile_id,classification"
	default:
		return ""
	}
}
func extraPlaceholders(table string) string {
	switch table {
	case "provider_attempts":
		return ",?"
	case "profile_lease_events", "profile_health_events":
		return ",?,?"
	default:
		return ""
	}
}
func healthTimeColumn(table string) string {
	if table == "profile_health_events" {
		return ",occurred_at"
	}
	return ""
}
func healthTimePlaceholder(table string) string {
	if table == "profile_health_events" {
		return ",?"
	}
	return ""
}
func eventArgs(table string, event Event, payload []byte, stamp string) []any {
	args := []any{event.TraceID, event.Sequence, event.Provider}
	switch table {
	case "provider_attempts":
		args = append(args, event.Classification)
	case "profile_lease_events":
		args = append(args, event.ProfileID, event.LeaseID)
	case "profile_health_events":
		args = append(args, event.ProfileID, event.Classification)
	}
	args = append(args, payload)
	if table == "profile_health_events" {
		args = append(args, stamp)
	}
	return args
}

func (store *Store) Close() error { return store.db.Close() }
