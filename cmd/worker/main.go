package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if err := drainOne(context.Background(), db); err != nil {
			log.Printf("outbox: %v", err)
		}
	}
}

func drainOne(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM outbox
		WHERE completed_at IS NULL AND available_at <= clock_timestamp()
		ORDER BY id
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`).Scan(&id)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE outbox SET claimed_at = clock_timestamp(), completed_at = clock_timestamp(), attempts = attempts + 1 WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit()
}
