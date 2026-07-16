-- +goose Up
CREATE TABLE world_snapshots (
    id smallint PRIMARY KEY CHECK (id = 1),
    payload jsonb NOT NULL,
    state_version bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE accounts (
    id uuid PRIMARY KEY,
    account_identifier text NOT NULL UNIQUE,
    password_hash bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE roles (
    id uuid PRIMARY KEY,
    account_id uuid NOT NULL UNIQUE REFERENCES accounts(id),
    name text NOT NULL UNIQUE,
    life_number bigint NOT NULL CHECK (life_number >= 1),
    status text NOT NULL CHECK (status IN ('alive', 'pending_reincarnation')),
    mcp_key_hash bytea,
    state_version bigint NOT NULL,
    rule_version integer NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE lives (
    role_id uuid PRIMARY KEY REFERENCES roles(id),
    life_started_at timestamptz NOT NULL,
    cultivation_millis bigint NOT NULL DEFAULT 0,
    cultivation_at timestamptz NOT NULL,
    last_settled_at timestamptz NOT NULL,
    next_death_at timestamptz,
    position_x bigint NOT NULL,
    position_y bigint NOT NULL,
    state_version bigint NOT NULL
);

CREATE TABLE trajectories (
    role_id uuid PRIMARY KEY REFERENCES roles(id),
    start_x bigint NOT NULL,
    start_y bigint NOT NULL,
    target_x bigint NOT NULL,
    target_y bigint NOT NULL,
    started_at timestamptz NOT NULL,
    speed_basis bigint NOT NULL,
    state_version bigint NOT NULL
);

CREATE TABLE opportunities (
    id uuid PRIMARY KEY,
    total_cultivation_millis bigint NOT NULL,
    converted_cultivation_millis bigint NOT NULL DEFAULT 0,
    level integer NOT NULL,
    sense_radius bigint NOT NULL,
    position_x bigint NOT NULL,
    position_y bigint NOT NULL,
    status text NOT NULL,
    bound_role_id uuid REFERENCES roles(id),
    bound_at timestamptz,
    state_version bigint NOT NULL
);

CREATE TABLE conversations (
    id uuid PRIMARY KEY,
    requester_role_id uuid NOT NULL REFERENCES roles(id),
    recipient_role_id uuid NOT NULL REFERENCES roles(id),
    status text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE conversation_messages (
    id bigserial PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES conversations(id),
    sender_role_id uuid NOT NULL REFERENCES roles(id),
    content text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE cultivation_ledger (
    id bigserial PRIMARY KEY,
    role_id uuid NOT NULL REFERENCES roles(id),
    life_number bigint NOT NULL,
    kind text NOT NULL,
    delta_millis bigint NOT NULL,
    command_id text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE role_events (
    id bigserial PRIMARY KEY,
    role_id uuid NOT NULL REFERENCES roles(id),
    life_number bigint NOT NULL,
    event_type text NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX role_events_recent_idx ON role_events (role_id, created_at DESC, id DESC);

CREATE TABLE idempotency_records (
    role_id uuid NOT NULL REFERENCES roles(id),
    idempotency_key text NOT NULL,
    command_name text NOT NULL,
    response jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (role_id, idempotency_key)
);

CREATE TABLE outbox (
    id bigserial PRIMARY KEY,
    aggregate_id text NOT NULL,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    state_version bigint NOT NULL,
    available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    claimed_at timestamptz,
    completed_at timestamptz,
    attempts integer NOT NULL DEFAULT 0
);
CREATE INDEX outbox_pending_idx ON outbox (available_at, id) WHERE completed_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS outbox;
DROP TABLE IF EXISTS idempotency_records;
DROP TABLE IF EXISTS role_events;
DROP TABLE IF EXISTS cultivation_ledger;
DROP TABLE IF EXISTS conversation_messages;
DROP TABLE IF EXISTS conversations;
DROP TABLE IF EXISTS opportunities;
DROP TABLE IF EXISTS trajectories;
DROP TABLE IF EXISTS lives;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS accounts;
DROP TABLE IF EXISTS world_snapshots;
