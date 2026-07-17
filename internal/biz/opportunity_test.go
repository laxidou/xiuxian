package biz

import (
	"context"
	"testing"
	"time"
)

type futureAuthorityStore struct {
	now time.Time
}

func (store futureAuthorityStore) Load(context.Context) ([]byte, error) { return nil, nil }
func (store futureAuthorityStore) Save(context.Context, []byte) error   { return nil }
func (store futureAuthorityStore) AuthorityNow(context.Context) (time.Time, error) {
	return store.now, nil
}

func TestManualClockHidesDatabaseTimeForAcceptanceRuns(t *testing.T) {
	start := time.UnixMilli(1_700_000_000_000)
	service, err := NewWorldAuthority(futureAuthorityStore{now: start.Add(24 * time.Hour)}, NewManualClock(start))
	if err != nil {
		t.Fatal(err)
	}
	_, state, err := service.Register(context.Background(), "manual", "a sufficiently long password", "试时真人")
	if err != nil {
		t.Fatal(err)
	}
	if settled := service.roles[state.ID].LastSettledAt; !settled.Equal(start) {
		t.Fatalf("last settled at = %s, want manual clock %s", settled, start)
	}
}

func TestInitialWorldCanPlaceDeathOpportunityAwayFromDeath(t *testing.T) {
	clock := NewManualClock(time.UnixMilli(1_700_000_000_000))
	service := NewService(clock)
	_, state, err := service.Register(context.Background(), "origin", "a sufficiently long password", "原点真人")
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(8 * time.Hour)
	if _, err := service.State(context.Background(), state.ID); err != nil {
		t.Fatal(err)
	}
	if len(service.opportunities) != 1 {
		t.Fatalf("opportunities = %d, want 1", len(service.opportunities))
	}
	for _, opportunity := range service.opportunities {
		if opportunity.Status != OpportunityUnclaimed {
			t.Fatalf("status = %s, want %s", opportunity.Status, OpportunityUnclaimed)
		}
		if opportunity.Position == opportunity.DeathPosition {
			t.Fatalf("opportunity remained at death position %#v", opportunity.Position)
		}
	}
}
