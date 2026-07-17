package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type PostgresSnapshotStore struct {
	db *sql.DB
}

type persistedAccount struct {
	PasswordHash []byte `json:"password_hash"`
	RoleID       string `json:"role_id"`
}

type persistedPosition struct {
	X int64
	Y int64
}

type persistedRole struct {
	ID              string
	Account         string
	Name            string
	LifeNumber      int64
	Status          string
	LifeStartedAt   time.Time
	CultivationBase int64
	CultivationAt   time.Time
	LastSettledAt   time.Time
	NextDeathAt     time.Time
	Position        persistedPosition
	StateVersion    int64
	MCPKeyHash      [32]byte
}

type persistedOpportunity struct {
	ID          string
	Position    persistedPosition
	Level       int
	Cultivation int64
	SenseRadius int64
	Status      string
	BoundRoleID string
	BoundAt     time.Time
	Credited    int64
}

type persistedMessage struct {
	ID        int64  `json:"id"`
	SenderID  string `json:"sender_id"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"`
}

type persistedConversation struct {
	ID          string             `json:"id"`
	RequesterID string             `json:"requester_id"`
	RecipientID string             `json:"recipient_id"`
	Status      string             `json:"status"`
	Messages    []persistedMessage `json:"messages"`
	CreatedAt   int64              `json:"created_at"`
	UpdatedAt   int64              `json:"updated_at"`
}

type persistedEvent struct {
	ID         int64          `json:"id"`
	Type       string         `json:"type"`
	CreatedAt  int64          `json:"created_at"`
	LifeNumber int64          `json:"life_number"`
	Data       map[string]any `json:"data"`
}

type persistedWorld struct {
	Accounts      map[string]persistedAccount           `json:"accounts"`
	Roles         map[string]persistedRole              `json:"roles"`
	Opportunities map[string]persistedOpportunity       `json:"opportunities"`
	Conversations map[string]persistedConversation      `json:"conversations"`
	Events        map[string][]persistedEvent           `json:"events"`
	Idempotency   map[string]map[string]json.RawMessage `json:"idempotency"`
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

func (s *PostgresSnapshotStore) AuthorityNow(ctx context.Context) (time.Time, error) {
	var now time.Time
	if err := s.db.QueryRowContext(ctx, `SELECT transaction_timestamp()`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("read authoritative database time: %w", err)
	}
	return now.UTC(), nil
}

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
	var currentVersion int64
	err = tx.QueryRowContext(ctx, `SELECT state_version FROM world_snapshots WHERE id = 1 FOR UPDATE`).Scan(&currentVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("lock world authority: %w", err)
	}
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
	if err := mirrorNormalizedState(ctx, tx, payload); err != nil {
		return err
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

func mirrorNormalizedState(ctx context.Context, tx *sql.Tx, payload []byte) error {
	var world persistedWorld
	if err := json.Unmarshal(payload, &world); err != nil {
		return fmt.Errorf("decode normalized world state: %w", err)
	}
	for accountID, account := range world.Accounts {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO accounts (id, account_identifier, password_hash)
			VALUES ($1, $1, $2)
			ON CONFLICT (id) DO UPDATE SET password_hash = EXCLUDED.password_hash
		`, accountID, account.PasswordHash); err != nil {
			return fmt.Errorf("mirror account: %w", err)
		}
		role := world.Roles[account.RoleID]
		mcpHash := role.MCPKeyHash[:]
		if role.MCPKeyHash == ([32]byte{}) {
			mcpHash = nil
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO roles (id, account_id, name, life_number, status, mcp_key_hash, state_version)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (id) DO UPDATE SET
				life_number = EXCLUDED.life_number,
				status = EXCLUDED.status,
				mcp_key_hash = EXCLUDED.mcp_key_hash,
				state_version = EXCLUDED.state_version
		`, role.ID, accountID, role.Name, role.LifeNumber, role.Status, mcpHash, role.StateVersion); err != nil {
			return fmt.Errorf("mirror role: %w", err)
		}
		var nextDeath any
		if !role.NextDeathAt.IsZero() {
			nextDeath = role.NextDeathAt
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO lives (role_id, life_started_at, cultivation_millis, cultivation_at, last_settled_at, next_death_at, position_x, position_y, state_version)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (role_id) DO UPDATE SET
				life_started_at=EXCLUDED.life_started_at,
				cultivation_millis=EXCLUDED.cultivation_millis,
				cultivation_at=EXCLUDED.cultivation_at,
				last_settled_at=EXCLUDED.last_settled_at,
				next_death_at=EXCLUDED.next_death_at,
				position_x=EXCLUDED.position_x,
				position_y=EXCLUDED.position_y,
				state_version=EXCLUDED.state_version
		`, role.ID, role.LifeStartedAt, role.CultivationBase, role.CultivationAt, role.LastSettledAt, nextDeath, role.Position.X, role.Position.Y, role.StateVersion); err != nil {
			return fmt.Errorf("mirror life: %w", err)
		}
	}
	for _, opportunity := range world.Opportunities {
		var boundRole any
		var boundAt any
		if opportunity.BoundRoleID != "" {
			boundRole = opportunity.BoundRoleID
			boundAt = opportunity.BoundAt
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO opportunities (id,total_cultivation_millis,converted_cultivation_millis,level,sense_radius,position_x,position_y,status,bound_role_id,bound_at,state_version)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,1)
			ON CONFLICT (id) DO UPDATE SET converted_cultivation_millis=EXCLUDED.converted_cultivation_millis,status=EXCLUDED.status,bound_role_id=EXCLUDED.bound_role_id,bound_at=EXCLUDED.bound_at,state_version=opportunities.state_version+1
		`, opportunity.ID, opportunity.Cultivation, opportunity.Credited, opportunity.Level, opportunity.SenseRadius, opportunity.Position.X, opportunity.Position.Y, opportunity.Status, boundRole, boundAt); err != nil {
			return fmt.Errorf("mirror opportunity: %w", err)
		}
	}
	for _, conversation := range world.Conversations {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO conversations (id,requester_role_id,recipient_role_id,status,created_at,updated_at)
			VALUES ($1,$2,$3,$4,to_timestamp($5::double precision/1000),to_timestamp($6::double precision/1000))
			ON CONFLICT (id) DO UPDATE SET status=EXCLUDED.status,updated_at=EXCLUDED.updated_at
		`, conversation.ID, conversation.RequesterID, conversation.RecipientID, conversation.Status, conversation.CreatedAt, conversation.UpdatedAt); err != nil {
			return fmt.Errorf("mirror conversation: %w", err)
		}
		for _, message := range conversation.Messages {
			if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_messages (id,conversation_id,sender_role_id,content,created_at) VALUES ($1,$2,$3,$4,to_timestamp($5::double precision/1000)) ON CONFLICT (id) DO NOTHING`, message.ID, conversation.ID, message.SenderID, message.Content, message.CreatedAt); err != nil {
				return fmt.Errorf("mirror conversation message: %w", err)
			}
		}
	}
	for roleID, events := range world.Events {
		for _, event := range events {
			data, _ := json.Marshal(event.Data)
			if _, err := tx.ExecContext(ctx, `INSERT INTO role_events (id,role_id,life_number,event_type,payload,created_at) VALUES ($1,$2,$3,$4,$5::jsonb,to_timestamp($6::double precision/1000)) ON CONFLICT (id) DO NOTHING`, event.ID, roleID, event.LifeNumber, event.Type, data, event.CreatedAt); err != nil {
				return fmt.Errorf("mirror role event: %w", err)
			}
		}
	}
	for roleID, records := range world.Idempotency {
		for key, response := range records {
			if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records (role_id,idempotency_key,command_name,response) VALUES ($1,$2,'world_command',$3::jsonb) ON CONFLICT (role_id,idempotency_key) DO NOTHING`, roleID, key, response); err != nil {
				return fmt.Errorf("mirror idempotency record: %w", err)
			}
		}
	}
	return nil
}
