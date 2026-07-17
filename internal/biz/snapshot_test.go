package biz

import (
	"bytes"
	"context"
	"testing"
	"time"
)

type captureStore struct {
	payload []byte
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
