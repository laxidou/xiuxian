package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
)

const deadlineKey = "world:death_deadlines"

type outboxItem struct {
	ID           int64
	EventType    string
	Payload      []byte
	StateVersion int64
}

type deadlineCandidate struct {
	RoleID       string `json:"role_id"`
	StateVersion int64  `json:"state_version"`
}

func main() {
	ctx := context.Background()
	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	redisOptions, err := redis.ParseURL(os.Getenv("REDIS_URL"))
	if err != nil {
		log.Fatal(err)
	}
	cache := redis.NewClient(redisOptions)
	defer cache.Close()
	if err := cache.Ping(ctx).Err(); err != nil {
		log.Fatal(err)
	}
	if err := projectDeadlines(ctx, db, cache); err != nil {
		log.Fatal(err)
	}

	authorityURL := strings.TrimRight(os.Getenv("GAME_SERVER_URL"), "/")
	if authorityURL == "" {
		authorityURL = "http://game-server:8080"
	}
	workerToken := os.Getenv("WORKER_TOKEN")
	client := &http.Client{Timeout: 10 * time.Second}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	rebuildTicker := time.NewTicker(30 * time.Second)
	defer rebuildTicker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := processOutbox(ctx, db, cache); err != nil {
				log.Printf("outbox: %v", err)
			}
			if err := settleDue(ctx, cache, client, authorityURL, workerToken); err != nil {
				log.Printf("deadlines: %v", err)
			}
		case <-rebuildTicker.C:
			if err := projectDeadlines(ctx, db, cache); err != nil {
				log.Printf("rebuild deadlines: %v", err)
			}
		}
	}
}

func processOutbox(ctx context.Context, db *sql.DB, cache *redis.Client) error {
	item, err := claimOne(ctx, db)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := projectDeadlines(ctx, db, cache); err != nil {
		return err
	}
	if err := cache.Publish(ctx, "world:events", item.Payload).Err(); err != nil {
		return fmt.Errorf("publish world event: %w", err)
	}
	_, err = db.ExecContext(ctx, `UPDATE outbox SET completed_at = clock_timestamp() WHERE id = $1`, item.ID)
	return err
}

func claimOne(ctx context.Context, db *sql.DB) (outboxItem, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return outboxItem{}, err
	}
	defer tx.Rollback()
	var item outboxItem
	err = tx.QueryRowContext(ctx, `
		SELECT id, event_type, payload, state_version FROM outbox
		WHERE completed_at IS NULL AND available_at <= clock_timestamp()
		ORDER BY id
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`).Scan(&item.ID, &item.EventType, &item.Payload, &item.StateVersion)
	if err != nil {
		return outboxItem{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE outbox SET claimed_at = clock_timestamp(), attempts = attempts + 1 WHERE id = $1`, item.ID); err != nil {
		return outboxItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return outboxItem{}, err
	}
	return item, nil
}

func projectDeadlines(ctx context.Context, db *sql.DB, cache *redis.Client) error {
	rows, err := db.QueryContext(ctx, `
		SELECT r.id, r.state_version, l.next_death_at
		FROM roles r JOIN lives l ON l.role_id = r.id
		WHERE r.status = 'alive' AND l.next_death_at IS NOT NULL
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	deadlines := make([]redis.Z, 0)
	for rows.Next() {
		var candidate deadlineCandidate
		var deadline time.Time
		if err := rows.Scan(&candidate.RoleID, &candidate.StateVersion, &deadline); err != nil {
			return err
		}
		member, _ := json.Marshal(candidate)
		deadlines = append(deadlines, redis.Z{Score: float64(deadline.UnixMilli()), Member: string(member)})
	}
	pipe := cache.TxPipeline()
	pipe.Del(ctx, deadlineKey)
	if len(deadlines) > 0 {
		pipe.ZAdd(ctx, deadlineKey, deadlines...)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func settleDue(ctx context.Context, cache *redis.Client, client *http.Client, authorityURL, workerToken string) error {
	members, err := cache.ZRangeByScore(ctx, deadlineKey, &redis.ZRangeBy{Min: "-inf", Max: strconv.FormatInt(time.Now().UnixMilli(), 10), Offset: 0, Count: 100}).Result()
	if err != nil {
		return err
	}
	for _, member := range members {
		var candidate deadlineCandidate
		if err := json.Unmarshal([]byte(member), &candidate); err != nil {
			_ = cache.ZRem(ctx, deadlineKey, member).Err()
			continue
		}
		body, _ := json.Marshal(candidate)
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, authorityURL+"/internal/deadlines/settle", bytes.NewReader(body))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Worker-Token", workerToken)
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("settle deadline for %s: status %d", candidate.RoleID, response.StatusCode)
		}
		if err := cache.ZRem(ctx, deadlineKey, member).Err(); err != nil {
			return err
		}
	}
	return nil
}
