package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type PostgresSnapshotStore struct {
	db *sql.DB
}

func OpenPostgres(ctx context.Context, databaseURL string) (*PostgresSnapshotStore, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &PostgresSnapshotStore{db: db}, nil
}

func (s *PostgresSnapshotStore) Close() error { return s.db.Close() }

func (s *PostgresSnapshotStore) DB() *sql.DB { return s.db }

func (s *PostgresSnapshotStore) Load(ctx context.Context) ([]byte, error) {
	var payload []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM world_snapshots WHERE id = 1`).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load snapshot: %w", err)
	}
	return payload, nil
}

func (s *PostgresSnapshotStore) Save(ctx context.Context, payload []byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin snapshot transaction: %w", err)
	}
	defer tx.Rollback()
	var version int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO world_snapshots (id, payload, state_version)
		VALUES (1, $1::jsonb, 1)
		ON CONFLICT (id) DO UPDATE SET
			payload = EXCLUDED.payload,
			state_version = world_snapshots.state_version + 1,
			updated_at = clock_timestamp()
		RETURNING state_version
	`, payload).Scan(&version)
	if err != nil {
		return fmt.Errorf("save snapshot: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO outbox (aggregate_id, event_type, payload, state_version)
		VALUES ('world', 'world.snapshot_committed', jsonb_build_object('state_version', $1::bigint), $1::bigint)
	`, version); err != nil {
		return fmt.Errorf("write snapshot outbox: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit snapshot transaction: %w", err)
	}
	return nil
}
