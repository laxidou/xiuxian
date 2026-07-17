package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type PostgresSnapshotStore struct {
	db    *gorm.DB
	sqlDB *sql.DB
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
	RuleVersion     int32
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
	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &PostgresSnapshotStore{db: db, sqlDB: sqlDB}, nil
}

func (s *PostgresSnapshotStore) Close() error { return s.sqlDB.Close() }

func (s *PostgresSnapshotStore) DB() *sql.DB { return s.sqlDB }

func (s *PostgresSnapshotStore) AuthorityNow(ctx context.Context) (time.Time, error) {
	var now time.Time
	if err := s.db.WithContext(ctx).Raw(`SELECT transaction_timestamp()`).Row().Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("read authoritative database time: %w", err)
	}
	return now.UTC(), nil
}

func (s *PostgresSnapshotStore) Load(ctx context.Context) ([]byte, error) {
	var payload []byte
	err := s.db.WithContext(ctx).Raw(`SELECT payload FROM world_snapshots WHERE id = 1`).Row().Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load snapshot: %w", err)
	}
	return payload, nil
}

func (s *PostgresSnapshotStore) Save(ctx context.Context, payload []byte) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var currentVersion int64
		var currentPayload []byte
		err := tx.Raw(`SELECT payload, state_version FROM world_snapshots WHERE id = 1 FOR UPDATE`).Row().Scan(&currentPayload, &currentVersion)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("lock world authority: %w", err)
		}
		roleIDs, err := changedRoleIDs(currentPayload, payload)
		if err != nil {
			return err
		}
		if err := lockRoleRows(tx, roleIDs); err != nil {
			return err
		}
		var version int64
		err = tx.Raw(`
		INSERT INTO world_snapshots (id, payload, state_version)
		VALUES (1, $1::jsonb, 1)
		ON CONFLICT (id) DO UPDATE SET
			payload = EXCLUDED.payload,
			state_version = world_snapshots.state_version + 1,
			updated_at = clock_timestamp()
		RETURNING state_version
	`, payload).Row().Scan(&version)
		if err != nil {
			return fmt.Errorf("save snapshot: %w", err)
		}
		if err := mirrorNormalizedState(ctx, tx, payload); err != nil {
			return err
		}
		if err := tx.Exec(`
		INSERT INTO outbox (aggregate_id, event_type, payload, state_version)
		VALUES ('world', 'world.snapshot_committed', jsonb_build_object('state_version', $1::bigint), $1::bigint)
	`, version).Error; err != nil {
			return fmt.Errorf("write snapshot outbox: %w", err)
		}
		return nil
	})
}

func changedRoleIDs(currentPayload, nextPayload []byte) ([]string, error) {
	current := persistedWorld{Roles: map[string]persistedRole{}}
	if len(currentPayload) > 0 {
		if err := json.Unmarshal(currentPayload, &current); err != nil {
			return nil, fmt.Errorf("decode current world state for role locks: %w", err)
		}
	}
	var next persistedWorld
	if err := json.Unmarshal(nextPayload, &next); err != nil {
		return nil, fmt.Errorf("decode next world state for role locks: %w", err)
	}
	changed := make([]string, 0)
	for id, role := range next.Roles {
		previous, exists := current.Roles[id]
		if !exists || previous.StateVersion != role.StateVersion {
			changed = append(changed, id)
		}
	}
	for id := range current.Roles {
		if _, exists := next.Roles[id]; !exists {
			changed = append(changed, id)
		}
	}
	sort.Strings(changed)
	return changed, nil
}

func lockRoleRows(tx *gorm.DB, roleIDs []string) error {
	if len(roleIDs) == 0 {
		return nil
	}
	placeholders := make([]string, len(roleIDs))
	arguments := make([]any, len(roleIDs))
	for index, roleID := range roleIDs {
		placeholders[index] = fmt.Sprintf("$%d", index+1)
		arguments[index] = roleID
	}
	rows, err := tx.Raw(
		`SELECT id FROM roles WHERE id IN (`+strings.Join(placeholders, ",")+`) ORDER BY id FOR UPDATE`,
		arguments...,
	).Rows()
	if err != nil {
		return fmt.Errorf("lock changed roles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var roleID string
		if err := rows.Scan(&roleID); err != nil {
			return fmt.Errorf("read locked role: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate locked roles: %w", err)
	}
	return nil
}

func mirrorNormalizedState(ctx context.Context, tx *gorm.DB, payload []byte) error {
	var world persistedWorld
	if err := json.Unmarshal(payload, &world); err != nil {
		return fmt.Errorf("decode normalized world state: %w", err)
	}
	for accountID, account := range world.Accounts {
		if err := tx.WithContext(ctx).Exec(`
			INSERT INTO accounts (id, account_identifier, password_hash)
			VALUES ($1, $1, $2)
			ON CONFLICT (id) DO UPDATE SET password_hash = EXCLUDED.password_hash
		`, accountID, account.PasswordHash).Error; err != nil {
			return fmt.Errorf("mirror account: %w", err)
		}
		role := world.Roles[account.RoleID]
		mcpHash := role.MCPKeyHash[:]
		if role.MCPKeyHash == ([32]byte{}) {
			mcpHash = nil
		}
		if err := tx.WithContext(ctx).Exec(`
			INSERT INTO roles (id, account_id, name, life_number, status, mcp_key_hash, state_version, rule_version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (id) DO UPDATE SET
				life_number = EXCLUDED.life_number,
				status = EXCLUDED.status,
				mcp_key_hash = EXCLUDED.mcp_key_hash,
				state_version = EXCLUDED.state_version,
				rule_version = EXCLUDED.rule_version
		`, role.ID, accountID, role.Name, role.LifeNumber, role.Status, mcpHash, role.StateVersion, role.RuleVersion).Error; err != nil {
			return fmt.Errorf("mirror role: %w", err)
		}
		var nextDeath any
		if !role.NextDeathAt.IsZero() {
			nextDeath = role.NextDeathAt
		}
		if err := tx.WithContext(ctx).Exec(`
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
		`, role.ID, role.LifeStartedAt, role.CultivationBase, role.CultivationAt, role.LastSettledAt, nextDeath, role.Position.X, role.Position.Y, role.StateVersion).Error; err != nil {
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
		if err := tx.WithContext(ctx).Exec(`
			INSERT INTO opportunities (id,total_cultivation_millis,converted_cultivation_millis,level,sense_radius,position_x,position_y,status,bound_role_id,bound_at,state_version)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,1)
			ON CONFLICT (id) DO UPDATE SET converted_cultivation_millis=EXCLUDED.converted_cultivation_millis,status=EXCLUDED.status,bound_role_id=EXCLUDED.bound_role_id,bound_at=EXCLUDED.bound_at,state_version=opportunities.state_version+1
		`, opportunity.ID, opportunity.Cultivation, opportunity.Credited, opportunity.Level, opportunity.SenseRadius, opportunity.Position.X, opportunity.Position.Y, opportunity.Status, boundRole, boundAt).Error; err != nil {
			return fmt.Errorf("mirror opportunity: %w", err)
		}
	}
	for _, conversation := range world.Conversations {
		if err := tx.WithContext(ctx).Exec(`
			INSERT INTO conversations (id,requester_role_id,recipient_role_id,status,created_at,updated_at)
			VALUES ($1,$2,$3,$4,to_timestamp($5::double precision/1000),to_timestamp($6::double precision/1000))
			ON CONFLICT (id) DO UPDATE SET status=EXCLUDED.status,updated_at=EXCLUDED.updated_at
		`, conversation.ID, conversation.RequesterID, conversation.RecipientID, conversation.Status, conversation.CreatedAt, conversation.UpdatedAt).Error; err != nil {
			return fmt.Errorf("mirror conversation: %w", err)
		}
		for _, message := range conversation.Messages {
			if err := tx.WithContext(ctx).Exec(`INSERT INTO conversation_messages (id,conversation_id,sender_role_id,content,created_at) VALUES ($1,$2,$3,$4,to_timestamp($5::double precision/1000)) ON CONFLICT (id) DO NOTHING`, message.ID, conversation.ID, message.SenderID, message.Content, message.CreatedAt).Error; err != nil {
				return fmt.Errorf("mirror conversation message: %w", err)
			}
		}
	}
	for roleID, events := range world.Events {
		for _, event := range events {
			data, _ := json.Marshal(event.Data)
			if err := tx.WithContext(ctx).Exec(`INSERT INTO role_events (id,role_id,life_number,event_type,payload,created_at) VALUES ($1,$2,$3,$4,$5::jsonb,to_timestamp($6::double precision/1000)) ON CONFLICT (id) DO NOTHING`, event.ID, roleID, event.LifeNumber, event.Type, data, event.CreatedAt).Error; err != nil {
				return fmt.Errorf("mirror role event: %w", err)
			}
		}
	}
	for roleID, records := range world.Idempotency {
		for key, response := range records {
			if err := tx.WithContext(ctx).Exec(`INSERT INTO idempotency_records (role_id,idempotency_key,command_name,response) VALUES ($1,$2,'world_command',$3::jsonb) ON CONFLICT (role_id,idempotency_key) DO NOTHING`, roleID, key, response).Error; err != nil {
				return fmt.Errorf("mirror idempotency record: %w", err)
			}
		}
	}
	return nil
}
