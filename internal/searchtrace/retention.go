package searchtrace

import (
	"context"
	"fmt"
	"time"
)

type RetentionConfig struct {
	RequestRetention time.Duration
	ProfileRetention time.Duration
	MaxBytes         int64
	Now              func() time.Time
}

func (store *Store) Cleanup(ctx context.Context, config RetentionConfig) error {
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.RequestRetention <= 0 {
		config.RequestRetention = 7 * 24 * time.Hour
	}
	if config.ProfileRetention <= 0 {
		config.ProfileRetention = 30 * 24 * time.Hour
	}
	requestCutoff := config.Now().Add(-config.RequestRetention).UTC().Format(time.RFC3339Nano)
	profileCutoff := config.Now().Add(-config.ProfileRetention).UTC().Format(time.RFC3339Nano)
	if _, err := store.db.ExecContext(ctx, `DELETE FROM profile_health_events WHERE occurred_at < ?`, profileCutoff); err != nil {
		return err
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM trace_events WHERE event_type IN ('profile_health','profile_state') AND occurred_at < ?`, profileCutoff); err != nil {
		return err
	}
	if err := store.deleteTracesBefore(ctx, requestCutoff); err != nil {
		return err
	}
	if config.MaxBytes > 0 {
		deleted := false
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			size, err := store.SizeBytes(ctx)
			if err != nil || size <= config.MaxBytes {
				if err != nil {
					return err
				}
				break
			}
			result, err := store.db.ExecContext(ctx, `DELETE FROM search_traces WHERE trace_id IN (SELECT trace_id FROM search_traces ORDER BY updated_at LIMIT 100)`)
			if err != nil {
				return err
			}
			rows, _ := result.RowsAffected()
			if rows == 0 {
				return fmt.Errorf("trace sqlite size %d exceeds limit %d with no removable traces", size, config.MaxBytes)
			}
			deleted = true
			if err := store.deleteOrphans(ctx); err != nil {
				return err
			}
		}
		if deleted {
			if _, err := store.db.ExecContext(ctx, `VACUUM`); err != nil {
				return err
			}
			size, err := store.SizeBytes(ctx)
			if err != nil {
				return err
			}
			if size > config.MaxBytes {
				return fmt.Errorf("trace sqlite size %d still exceeds limit %d", size, config.MaxBytes)
			}
		}
	}
	_, err := store.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

func (store *Store) deleteTracesBefore(ctx context.Context, cutoff string) error {
	if _, err := store.db.ExecContext(ctx, `DELETE FROM search_traces WHERE updated_at < ?`, cutoff); err != nil {
		return err
	}
	return store.deleteOrphans(ctx)
}

func (store *Store) deleteOrphans(ctx context.Context) error {
	for _, table := range []string{"route_decisions", "provider_attempts", "profile_lease_events"} {
		query := fmt.Sprintf(`DELETE FROM %s WHERE trace_id NOT IN (SELECT trace_id FROM search_traces)`, table)
		if _, err := store.db.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	_, err := store.db.ExecContext(ctx, `DELETE FROM trace_events WHERE event_type NOT IN ('profile_health','profile_state') AND trace_id NOT IN (SELECT trace_id FROM search_traces)`)
	return err
}

func (store *Store) SizeBytes(ctx context.Context) (int64, error) {
	var pages, freePages, pageSize int64
	if err := store.db.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pages); err != nil {
		return 0, err
	}
	if err := store.db.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&pageSize); err != nil {
		return 0, err
	}
	if err := store.db.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&freePages); err != nil {
		return 0, err
	}
	return (pages - freePages) * pageSize, nil
}
