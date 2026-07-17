package biz

import (
	"bytes"
	"context"
	"testing"
	"time"

	"xiuxian/internal/rules"
)

type captureStore struct {
	payload []byte
}

func TestDirectionalMovementRestoresFromAuthoritativeSnapshot(t *testing.T) {
	store := &captureStore{}
	startedAt := time.UnixMilli(1_700_000_000_000)
	service, err := NewPersistentService(context.Background(), NewManualClock(startedAt), store)
	if err != nil {
		t.Fatal(err)
	}
	_, state, err := service.Register(context.Background(), "snapshot-direction", "a sufficiently long password", "长风")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.MoveDirection(context.Background(), state.ID, "direction-snapshot", rules.DirectionUp, 1, CommandExpectation{LifeNumber: state.LifeNumber, StateVersion: state.StateVersion})
	if err != nil {
		t.Fatal(err)
	}

	restored, err := NewPersistentService(context.Background(), NewManualClock(startedAt.Add(2*time.Second)), store)
	if err != nil {
		t.Fatal(err)
	}
	current, err := restored.State(context.Background(), state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Position.X != 0 || current.Position.Y != 2 || current.MovementMode != "direction" || current.MovementDirection != "up" {
		t.Fatalf("restored direction state = %#v", current)
	}
}

func (store *captureStore) Load(context.Context) ([]byte, error) {
	return append([]byte(nil), store.payload...), nil
}

func (store *captureStore) Save(_ context.Context, payload []byte) error {
	store.payload = append([]byte(nil), payload...)
	return nil
}

func TestConversationIdempotencySnapshotIsJSONBCompatible(t *testing.T) {
	store := &captureStore{}
	service, err := NewPersistentService(context.Background(), NewManualClock(time.UnixMilli(1_700_000_000_000)), store)
	if err != nil {
		t.Fatal(err)
	}
	_, requester, err := service.Register(context.Background(), "snapshot-requester", "a sufficiently long password", "快照甲")
	if err != nil {
		t.Fatal(err)
	}
	_, recipient, err := service.Register(context.Background(), "snapshot-recipient", "a sufficiently long password", "快照乙")
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := service.RequestConversation(context.Background(), requester.ID, recipient.ID, "conversation-key", CommandExpectation{LifeNumber: 1, StateVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(store.payload, []byte(`\u0000`)) {
		t.Fatalf("snapshot contains PostgreSQL-incompatible NUL escape: %s", store.payload)
	}
	restored, err := NewPersistentService(context.Background(), NewManualClock(time.UnixMilli(1_700_000_000_000)), store)
	if err != nil {
		t.Fatal(err)
	}
	if got := restored.conversationResults[conversationCommandKey(requester.ID, "conversation-key")]; got != conversation.ID {
		t.Fatalf("restored conversation result = %q, want %q", got, conversation.ID)
	}
}
